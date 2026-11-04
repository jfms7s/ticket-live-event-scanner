package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

var lisbon = func() *time.Location {
	l, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		panic(err)
	}
	return l
}()

func utc(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func TestHourlyFiresAtTheTopOfTheNextHour(t *testing.T) {
	next := Hourly()
	for in, want := range map[time.Time]time.Time{
		utc(2026, 11, 4, 10, 0):  utc(2026, 11, 4, 11, 0),
		utc(2026, 11, 4, 10, 59): utc(2026, 11, 4, 11, 0),
		utc(2026, 11, 4, 23, 30): utc(2026, 11, 5, 0, 0),
	} {
		if got := next(in); !got.Equal(want) {
			t.Errorf("Hourly()(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestWeeklyAtIsSundayEveningInLisbon(t *testing.T) {
	next := WeeklyAt(lisbon, time.Sunday, 19, 0)
	// 2026-11-04 is a Wednesday; Lisbon is on UTC in November.
	if got, want := next(utc(2026, 11, 4, 12, 0)), utc(2026, 11, 8, 19, 0); !got.Equal(want) {
		t.Errorf("from Wednesday: %v, want %v", got, want)
	}
	// Exactly on the tick: the following week.
	if got, want := next(utc(2026, 11, 8, 19, 0)), utc(2026, 11, 15, 19, 0); !got.Equal(want) {
		t.Errorf("on the tick: %v, want %v", got, want)
	}
	// Sunday before 19:00: that evening.
	if got, want := next(utc(2026, 11, 8, 18, 59)), utc(2026, 11, 8, 19, 0); !got.Equal(want) {
		t.Errorf("Sunday morning: %v, want %v", got, want)
	}
	// Lisbon local time is read, not UTC: 23:30 UTC Sunday in summer is
	// already Monday 00:30 in Lisbon, so the next tick is a week away.
	if got, want := next(utc(2026, 7, 5, 23, 30)), time.Date(2026, 7, 12, 19, 0, 0, 0, lisbon); !got.Equal(want) {
		t.Errorf("late Sunday in summer: %v, want %v", got, want)
	}
}

// The wall-clock time holds across the clock change: Lisbon goes from UTC+1
// to UTC on Sunday 2026-10-25 at 01:00 UTC, so the Sunday 19:00 ticks on the
// 18th and the 25th are 169 hours apart.
func TestWeeklyAtKeepsTheLocalTimeAcrossDST(t *testing.T) {
	next := WeeklyAt(lisbon, time.Sunday, 19, 0)
	before := next(utc(2026, 10, 14, 12, 0)) // Sunday 18th, summer time
	after := next(before)                    // Sunday 25th, winter time
	if !before.Equal(time.Date(2026, 10, 18, 19, 0, 0, 0, lisbon)) || !after.Equal(time.Date(2026, 10, 25, 19, 0, 0, 0, lisbon)) {
		t.Errorf("ticks %v, %v; want 19:00 Lisbon on the 18th and the 25th", before, after)
	}
	if d := after.Sub(before); d != 7*24*time.Hour+time.Hour {
		t.Errorf("gap %v, want a week and the hour the clocks went back", d)
	}
}

// fakeClock advances only when told to; After fires when the clock reaches
// the deadline.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	at := c.now.Add(d)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, waiter{at, ch})
	return ch
}

// advance moves the clock to t and fires the timers that are due.
func (c *fakeClock) advance(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
	rest := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(t) {
			w.ch <- t
		} else {
			rest = append(rest, w)
		}
	}
	c.waiters = rest
}

func (c *fakeClock) waiting() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

type runs struct {
	mu sync.Mutex
	at []time.Time
}

func (r *runs) add(t time.Time) {
	r.mu.Lock()
	r.at = append(r.at, t)
	r.mu.Unlock()
}

func (r *runs) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.at)
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func start(t *testing.T, clock *fakeClock, job Job, next Next, atStart bool) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Run(ctx, clock, job, next, atStart); close(done) }()
	return func() { cancel(); <-done }
}

func TestRunsAtStartThenAtEachTick(t *testing.T) {
	clock := &fakeClock{now: utc(2026, 11, 4, 10, 20)}
	r := &runs{}
	stop := start(t, clock, Job{Name: "scrape", Run: func(context.Context) error { r.add(clock.Now()); return nil }}, Hourly(), true)
	defer stop()

	eventually(t, func() bool { return r.count() == 1 && clock.waiting() == 1 })
	clock.advance(utc(2026, 11, 4, 11, 0))
	eventually(t, func() bool { return r.count() == 2 && clock.waiting() == 1 })
	clock.advance(utc(2026, 11, 4, 12, 0))
	eventually(t, func() bool { return r.count() == 3 })
}

func TestDoesNotRunAtStartWhenNotAsked(t *testing.T) {
	clock := &fakeClock{now: utc(2026, 11, 4, 10, 20)}
	r := &runs{}
	stop := start(t, clock, Job{Name: "digest", Run: func(context.Context) error { r.add(clock.Now()); return nil }}, Hourly(), false)
	defer stop()
	eventually(t, func() bool { return clock.waiting() == 1 })
	if r.count() != 0 {
		t.Errorf("ran %d times before the first tick", r.count())
	}
}

// A run that outlasts the interval makes the tick it spanned disappear: the
// next run is at the next hour after it ended, never back to back.
func TestALongRunSkipsTheTicksItSpanned(t *testing.T) {
	clock := &fakeClock{now: utc(2026, 11, 4, 10, 50)}
	r := &runs{}
	release := make(chan struct{})
	job := Job{Name: "scrape", Run: func(context.Context) error {
		r.add(clock.Now())
		if r.count() == 2 {
			<-release
		}
		return nil
	}}
	stop := start(t, clock, job, Hourly(), true)
	defer stop()

	eventually(t, func() bool { return r.count() == 1 && clock.waiting() == 1 })
	clock.advance(utc(2026, 11, 4, 11, 0)) // run 2 starts and blocks
	eventually(t, func() bool { return r.count() == 2 })
	clock.advance(utc(2026, 11, 4, 12, 5)) // the 12:00 tick passes while run 2 is busy
	close(release)
	eventually(t, func() bool { return clock.waiting() == 1 })
	if r.count() != 2 {
		t.Fatalf("a run started for the tick that fell inside the busy run: %d runs", r.count())
	}
	clock.advance(utc(2026, 11, 4, 13, 0))
	eventually(t, func() bool { return r.count() == 3 })
}

// A timer that fires slightly before the tick must not run the job twice.
func TestAnEarlyTimerDoesNotFireATickTwice(t *testing.T) {
	clock := &fakeClock{now: utc(2026, 11, 4, 10, 20)}
	r := &runs{}
	stop := start(t, clock, Job{Name: "scrape", Run: func(context.Context) error { r.add(clock.Now()); return nil }}, Hourly(), false)
	defer stop()
	eventually(t, func() bool { return clock.waiting() == 1 })
	// Fire the timer while the clock still reads 10:59:59.9.
	early := utc(2026, 11, 4, 10, 59).Add(59*time.Second + 900*time.Millisecond)
	clock.mu.Lock()
	clock.now = early
	w := clock.waiters[0]
	clock.waiters = nil
	clock.mu.Unlock()
	w.ch <- early
	eventually(t, func() bool { return r.count() == 1 && clock.waiting() == 1 })
	clock.mu.Lock()
	next := clock.waiters[0].at
	clock.mu.Unlock()
	if want := utc(2026, 11, 4, 12, 0); !next.Equal(want) {
		t.Errorf("next wait ends at %v, want %v (not the same 11:00 tick again)", next, want)
	}
}

func TestAFailingOrPanickingRunDoesNotStopTheLoop(t *testing.T) {
	clock := &fakeClock{now: utc(2026, 11, 4, 10, 20)}
	r := &runs{}
	job := Job{Name: "scrape", Run: func(context.Context) error {
		r.add(clock.Now())
		switch r.count() {
		case 1:
			return errors.New("boom")
		case 2:
			panic("kaboom")
		}
		return nil
	}}
	stop := start(t, clock, job, Hourly(), true)
	defer stop()
	eventually(t, func() bool { return r.count() == 1 && clock.waiting() == 1 })
	clock.advance(utc(2026, 11, 4, 11, 0))
	eventually(t, func() bool { return r.count() == 2 && clock.waiting() == 1 })
	clock.advance(utc(2026, 11, 4, 12, 0))
	eventually(t, func() bool { return r.count() == 3 })
}

func TestTheRunGetsItsTimeout(t *testing.T) {
	clock := &fakeClock{now: utc(2026, 11, 4, 10, 20)}
	got := make(chan time.Duration, 1)
	job := Job{Name: "scrape", Timeout: 15 * time.Minute, Run: func(ctx context.Context) error {
		dl, ok := ctx.Deadline()
		if !ok {
			got <- 0
		} else {
			got <- time.Until(dl)
		}
		return nil
	}}
	stop := start(t, clock, job, Hourly(), true)
	defer stop()
	if d := <-got; d <= 14*time.Minute || d > 15*time.Minute {
		t.Errorf("run deadline in %v, want about 15m", d)
	}
}

func TestStopsWhileWaitingAndWhileRunning(t *testing.T) {
	clock := &fakeClock{now: utc(2026, 11, 4, 10, 20)}
	stop := start(t, clock, Job{Name: "x", Run: func(context.Context) error { return nil }}, Hourly(), false)
	eventually(t, func() bool { return clock.waiting() == 1 })
	stop() // returns: Run noticed the cancelled context while waiting

	started := make(chan struct{})
	job := Job{Name: "y", Run: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	stop2 := start(t, clock, job, Hourly(), true)
	<-started
	stop2() // returns: the run saw the cancel and Run did not start another
}
