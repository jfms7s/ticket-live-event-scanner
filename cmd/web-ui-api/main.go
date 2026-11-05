// Package main runs the web-ui-api service, a long-lived Kubernetes Deployment.
//
// Responsibilities:
//  1. Consume events.discovered messages from NATS JetStream and materialize
//     them into the SQLite database (upsert events, insert pending notifications).
//  2. Consume notifications.sent and notifications.failed messages and update
//     notification statuses in the database.
//  3. Deliver the notifications: the Telegram channel (each new session,
//     the weekly digest) and the email channel (calendar invites), which
//     used to be separate pods. A channel whose configuration is
//     incomplete is disabled (see ticket_scanner_notifier_enabled).
//  4. Run the scraper every hour and the weekly digest on Sunday evening
//     (package schedule), formerly two CronJobs; `--scrape-once` and
//     `--digest-once` run one by hand and exit.
//  5. Serve an HTTP API to query events and notifications and to trigger
//     retries, plus the scraper's run log (GET /api/runs), and the dashboard
//     (the embedded web/frontend build) on the same origin.
//
// See the project design doc (kept outside this repo), §6.4 and §8.1, for the full spec.
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

	"github.com/jfms7s/ticket-live-event-scanner/internal/natsconn"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/channels"
	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
	"github.com/jfms7s/ticket-live-event-scanner/internal/schedule"
	scraperconfig "github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/job"
	"github.com/jfms7s/ticket-live-event-scanner/internal/shutdown"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/api"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/consumer"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/static"
	"github.com/jfms7s/ticket-live-event-scanner/web/frontend"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// consumerRestartDelay is the pause before a consumer that exited is
	// started again.
	consumerRestartDelay = 5 * time.Second
	// consumerStopTimeout bounds how long shutdown waits for the consumers
	// to finish the message in hand.
	consumerStopTimeout = 15 * time.Second
)

func main() {
	scrapeOnce := flag.Bool("scrape-once", false, "run one discovery now and exit (a manual scrape: kubectl exec ... --scrape-once)")
	digestOnce := flag.Bool("digest-once", false, "publish this week's digest now and exit (it takes the week's slot: Sunday's scheduled digest is then not sent again)")
	flag.Parse()

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

	// Connect to NATS
	nc, err := natsconn.Connect(cfg.NatsURL, "web-ui-api")
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Get JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatalf("Failed to create JetStream context: %v", err)
	}

	// Ensure streams exist
	if err := streams.EnsureStreams(ctx, js); err != nil {
		log.Fatalf("Failed to ensure streams: %v", err)
	}

	// A manual run: do the job on this process's connections and exit,
	// without serving or consuming.
	if *scrapeOnce || *digestOnce {
		scfg, err := scraperconfig.Load()
		if err != nil {
			log.Fatalf("Scraper configuration: %v", err)
		}
		deps := job.Deps{Config: scfg, DB: db, JS: js}
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

	// Create durable consumers for web-ui-api
	// Consumer for events.discovered
	eventConsumer, err := streams.EnsureConsumer(ctx, js, streams.WebUIEventsConsumer())
	if err != nil {
		log.Fatalf("Failed to create events consumer: %v", err)
	}

	// Consumer for Telegram and email delivery results
	notifConsumer, err := streams.EnsureConsumer(ctx, js, streams.WebUINotificationsConsumer())
	if err != nil {
		log.Fatalf("Failed to create notifications consumer: %v", err)
	}

	// The scraper's run log, served by GET /api/runs. Created here too so
	// the endpoint works before the scraper's first run.
	runsKV, err := streams.EnsureRunsBucket(ctx, js)
	if err != nil {
		log.Fatalf("Failed to ensure the runs bucket: %v", err)
	}

	// Run the NATS consumers in the background, restarting each one if it
	// exits, until shutdown cancels ctx.
	var consumers sync.WaitGroup
	consumers.Add(2)
	go func() {
		defer consumers.Done()
		shutdown.RunWithRestart(ctx, "Events consumer", func(ctx context.Context) {
			consumer.ConsumeEventsDiscovered(ctx, eventConsumer, db)
		}, consumerRestartDelay)
	}()
	go func() {
		defer consumers.Done()
		shutdown.RunWithRestart(ctx, "Notifications consumer", func(ctx context.Context) {
			consumer.ConsumeNotifications(ctx, notifConsumer, db)
		}, consumerRestartDelay)
	}()
	consumersDone := make(chan struct{})
	go func() {
		consumers.Wait()
		close(consumersDone)
	}()

	// The delivery channels (Telegram, email) that used to be separate
	// pods. One whose configuration is incomplete is disabled, not fatal.
	channelList, channelStatus := channels.FromEnv()
	notifiersDone, err := notifier.Start(ctx, js, channelList)
	if err != nil {
		log.Fatalf("Failed to start the notifiers: %v", err)
	}

	// The scraper and the weekly digest, formerly two CronJobs.
	runsReader := runs.NewReader(runsKV)
	schedulersDone := startSchedulers(ctx, db, js, runsReader)

	// Setup HTTP server
	app := api.NewApp(db, js)
	app.SetNotifierStatus(channelStatus)
	app.SetRuns(runsReader)
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

	// Order matters: stop HTTP, stop the consumers and wait for the
	// message in hand, and only then let the deferred nc.Close and
	// db.Close run. Both waits stay inside Kubernetes' 30 s grace period.
	log.Println("Shutting down: stopping the HTTP server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Shutting down: stopping the consumers...")
	cancel()
	if shutdown.WaitOrTimeout(allDone(consumersDone, notifiersDone, schedulersDone), consumerStopTimeout) {
		log.Println("Consumers stopped; closing NATS and the database")
	} else {
		log.Printf("Consumers still busy after %v; closing NATS and the database anyway", consumerStopTimeout)
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
// turns both off (a deployment that still runs the CronJobs, or a dev setup
// that only wants the dashboard); a malformed scraper setting disables them
// with a logged error rather than stopping the dashboard.
func startSchedulers(ctx context.Context, db *sql.DB, js jetstream.JetStream, reader *runs.Reader) <-chan struct{} {
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
	deps := job.Deps{Config: scfg, DB: db, JS: js}
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
