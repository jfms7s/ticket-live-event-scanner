// Package main runs the web-ui-api service, a long-lived Kubernetes Deployment.
//
// Responsibilities:
//  1. Send the notifications the outbox holds (package deliver): the
//     Telegram message for each new session and the calendar-invite email,
//     with retries. A channel whose configuration is incomplete is disabled
//     (see ticket_scanner_notifier_enabled).
//  2. Run the scraper every hour and the weekly digest on Sunday evening
//     (package schedule); `--scrape-once` and `--digest-once` run one by
//     hand and exit.
//  3. Serve an HTTP API to query events and notifications and to trigger
//     retries, plus the scraper's run log (GET /api/runs), and the dashboard
//     (the embedded web/frontend build) on the same origin.
//
// Everything is in one process on one SQLite file; there is no message
// broker. `--nats-drain-check` is for the move from the release that had one.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	// The image has no zone database; the digest runs on Lisbon time.
	_ "time/tzdata"

	"github.com/jfms7s/ticket-live-event-scanner/internal/deliver"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/channels"
	"github.com/jfms7s/ticket-live-event-scanner/internal/outbox"
	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
	"github.com/jfms7s/ticket-live-event-scanner/internal/schedule"
	scraperconfig "github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/job"
	"github.com/jfms7s/ticket-live-event-scanner/internal/shutdown"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/api"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/static"
	"github.com/jfms7s/ticket-live-event-scanner/web/frontend"
)

// backgroundStopTimeout bounds how long shutdown waits for the delivery
// poller and the scheduled jobs to finish what they are doing, inside
// Kubernetes' 30 s grace period (the HTTP server gets 10 s before it).
const backgroundStopTimeout = 15 * time.Second

func main() {
	scrapeOnce := flag.Bool("scrape-once", false, "run one discovery now and exit (a manual scrape: kubectl exec ... --scrape-once)")
	digestOnce := flag.Bool("digest-once", false, "post this week's digest now and exit (it takes the week's slot: Sunday's scheduled digest is then not sent again)")
	drainCheck := flag.Bool("nats-drain-check", false, "report whether the NATS consumers of the earlier releases have nothing in flight, and exit (0 = drained)")
	flag.Parse()

	if *drainCheck {
		os.Exit(runDrainCheck())
	}

	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Open the SQLite database and migrate the schema. An empty one is
	// refused unless DATABASE_ALLOW_EMPTY: a wrong or unmounted volume must
	// not look like a fresh install (every session would be announced again).
	db, err := store.Open(ctx, store.Options{
		Path:       cfg.DatabasePath,
		AllowEmpty: cfg.DatabaseAllowEmpty,
	})
	if err != nil {
		log.Fatalf("Failed to open the database: %v", err)
	}
	defer db.Close()

	ch := channels.FromEnv()
	runLog := runs.SQLStore{DB: db}

	// A manual run: do the job on this process's database and exit, without
	// serving or sending. What a scrape queues is sent by the running
	// server's poller within seconds.
	if *scrapeOnce || *digestOnce {
		scfg, err := scraperconfig.Load()
		if err != nil {
			log.Fatalf("Scraper configuration: %v", err)
		}
		deps := job.Deps{Config: scfg, DB: db, Queue: &outbox.Queue{DB: db}, SendDigest: ch.Digest}
		if *scrapeOnce {
			if err := job.Scrape(ctx, deps); err != nil {
				log.Fatalf("Scrape failed: %v", err)
			}
		}
		if *digestOnce {
			if err := job.Digest(ctx, deps); err != nil {
				log.Fatalf("Digest failed: %v", err)
			}
		}
		return
	}

	// The delivery poller sends what the outbox holds.
	poller := deliver.New(db, ch.Senders, ch.TelegramGap)
	var background sync.WaitGroup
	background.Add(1)
	go func() {
		defer background.Done()
		poller.Run(ctx)
	}()

	// The scraper and the weekly digest, formerly two CronJobs.
	queue := &outbox.Queue{DB: db, Nudge: poller.Nudge}
	schedulersDone := startSchedulers(ctx, db, queue, ch.Digest, runs.NewReader(runLog))

	// Setup HTTP server
	app := api.NewApp(db, poller.Nudge)
	app.SetNotifierStatus(ch.Enabled)
	app.SetRuns(runs.NewReader(runLog))
	if cfg.CORSOrigin == "*" {
		log.Println("WARNING: CORS_ORIGIN=* lets any website call mutating endpoints; set it to the dashboard origin")
	}
	app.SetStatic(static.Handler(frontend.Dist()))
	handler := api.RequireCustomHeader(app.NewMux())
	if cfg.CORSOrigin != "" {
		// Only for a dev setup serving the dashboard from another origin;
		// in production it shares this one. Outermost, so preflights are
		// answered before the header check.
		handler = api.CORSMiddleware(handler, cfg.CORSOrigin)
	}

	// Timeouts keep a slow or idle client from holding connections open
	// on a small pod.
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting HTTP server on %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Order matters: stop HTTP, stop the poller and the jobs and wait for
	// the work in hand, and only then let the deferred db.Close run. Both
	// waits stay inside Kubernetes' 30 s grace period.
	log.Println("Shutting down: stopping the HTTP server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Shutting down: stopping the poller and the scheduled jobs...")
	cancel()
	pollerDone := make(chan struct{})
	go func() {
		background.Wait()
		close(pollerDone)
	}()
	if shutdown.WaitOrTimeout(allDone(pollerDone, schedulersDone), backgroundStopTimeout) {
		log.Println("Stopped; closing the database")
	} else {
		log.Printf("Still busy after %v; closing the database anyway", backgroundStopTimeout)
	}
}

// allDone returns a channel that is closed once every channel in chans is.
func allDone(chans ...<-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for _, c := range chans {
			<-c
		}
		close(done)
	}()
	return done
}

// startSchedulers runs discovery every hour on the hour and the digest on
// Sunday at 19:00 Lisbon time, in the background until ctx is cancelled. The
// returned channel is closed once both have returned. SCRAPER_ENABLED=false
// turns both off (a dev setup that only wants the dashboard); a malformed
// scraper setting disables them with a logged error rather than stopping
// the dashboard.
func startSchedulers(ctx context.Context, db *sql.DB, queue *outbox.Queue, sendDigest func(context.Context, string) (string, error), reader *runs.Reader) <-chan struct{} {
	done := make(chan struct{})
	if v := strings.ToLower(os.Getenv("SCRAPER_ENABLED")); v == "false" || v == "0" {
		log.Printf("Scraper and digest schedulers are OFF (SCRAPER_ENABLED=%s)", v)
		close(done)
		return done
	}
	scfg, err := scraperconfig.Load()
	if err != nil {
		log.Printf("ERROR the scraper and digest are DISABLED: %v", err)
		close(done)
		return done
	}
	deps := job.Deps{Config: scfg, DB: db, Queue: queue, SendDigest: sendDigest}
	lisbon, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		log.Printf("ERROR the scraper and digest are DISABLED: %v", err)
		close(done)
		return done
	}

	// Scrape at start-up, as a CronJob would at its next hour at the latest,
	// unless a run finished a moment ago: a pod stuck restarting must not
	// hit ticketline.pt on every start.
	atStart := !ranRecently(ctx, reader, startupGap)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		schedule.Run(ctx, schedule.System{}, schedule.Job{
			Name: "scrape", Timeout: job.ScrapeTimeout,
			Run: func(ctx context.Context) error { return job.Scrape(ctx, deps) },
		}, schedule.Hourly(), atStart)
	}()
	go func() {
		defer wg.Done()
		schedule.Run(ctx, schedule.System{}, schedule.Job{
			Name: "digest", Timeout: 5 * time.Minute,
			// Sunday's tick is the week's only chance: retry a failure a few
			// times before giving up until next week.
			Run: func(ctx context.Context) error {
				return schedule.Retry(ctx, schedule.System{}, 3, 30*time.Second, func(ctx context.Context) error { return job.Digest(ctx, deps) })
			},
		}, schedule.WeeklyAt(lisbon, time.Sunday, 19, 0), false)
	}()
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

// startupGap is how recently a run must have finished for start-up to skip
// its own scrape.
const startupGap = 10 * time.Minute

// ranRecently reports whether the run log has a run that finished within d.
// An unreadable log counts as "no".
func ranRecently(ctx context.Context, reader *runs.Reader, d time.Duration) bool {
	sum, err := reader.Summary(ctx, 0)
	if err != nil {
		return false
	}
	for _, h := range sum.Latest {
		if time.Since(h.FinishedAt) < d {
			return true
		}
	}
	return false
}
