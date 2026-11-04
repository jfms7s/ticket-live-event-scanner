// Package schedule runs a job on a calendar schedule inside the process,
// the way the scraper's Kubernetes CronJobs did: at the top of every hour,
// or at a weekly time in a time zone.
package schedule

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"time"
)

// Clock is the time source; tests substitute a fake.
type Clock interface {
	Now() time.Time
	// After returns a channel that receives once d has passed.
	After(d time.Duration) <-chan time.Time
}

// System is the real clock.
type System struct{}

func (System) Now() time.Time                         { return time.Now() }
func (System) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Next returns the next time a schedule fires strictly after t.
type Next func(t time.Time) time.Time

// Hourly fires at minute 0 of every hour (cron "0 * * * *").
func Hourly() Next {
	return func(t time.Time) time.Time { return t.Truncate(time.Hour).Add(time.Hour) }
}

// WeeklyAt fires once a week, on day at hour:minute in loc (Sunday 19:00
// Europe/Lisbon is WeeklyAt(lisbon, time.Sunday, 19, 0)). The wall-clock
// time is kept across daylight saving changes.
func WeeklyAt(loc *time.Location, day time.Weekday, hour, minute int) Next {
	return func(t time.Time) time.Time {
		lt := t.In(loc)
		days := (int(day) - int(lt.Weekday()) + 7) % 7
		at := time.Date(lt.Year(), lt.Month(), lt.Day()+days, hour, minute, 0, 0, loc)
		if !at.After(t) {
			at = time.Date(lt.Year(), lt.Month(), lt.Day()+days+7, hour, minute, 0, 0, loc)
		}
		return at
	}
}

// Job is something to run on a schedule.
type Job struct {
	Name string
	// Timeout bounds one run (0: unbounded).
	Timeout time.Duration
	Run     func(ctx context.Context) error
}

// Run runs job at each time next gives, until ctx is cancelled, and, when
// runAtStart is set, once at once before the first wait. Runs never
// overlap: a run that takes longer than the interval makes the late tick
// disappear rather than queue up. A failed run, even a panicking one, is
// logged and does not stop the loop. It returns when ctx is cancelled and
// the run in progress, if any, has returned.
func Run(ctx context.Context, clock Clock, job Job, next Next, runAtStart bool) {
	if runAtStart {
		runOnce(ctx, clock, job)
	}
	var last time.Time // the tick just run
	for ctx.Err() == nil {
		// A timer that fires a little early must not make the same tick
		// fire twice: the next tick is computed from the later of now and
		// the tick just run.
		from := clock.Now()
		if from.Before(last) {
			from = last
		}
		last = next(from)
		log.Printf("%s: next run at %s", job.Name, last.Format(time.RFC3339))
		select {
		case <-ctx.Done():
			return
		case <-clock.After(last.Sub(clock.Now())):
		}
		runOnce(ctx, clock, job)
	}
}

// runOnce runs job once with its timeout, turning a panic into an error.
func runOnce(ctx context.Context, clock Clock, job Job) {
	runCtx := ctx
	if job.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, job.Timeout)
		defer cancel()
	}
	start := clock.Now()
	log.Printf("%s: running", job.Name)
	err := protect(runCtx, job.Run)
	if err != nil {
		log.Printf("ERROR %s failed after %v: %v", job.Name, clock.Now().Sub(start).Round(time.Millisecond), err)
		return
	}
	log.Printf("%s: done in %v", job.Name, clock.Now().Sub(start).Round(time.Millisecond))
}

func protect(ctx context.Context, run func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return run(ctx)
}
