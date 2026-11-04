package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/natsconn"
	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/digest"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/discovery"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/watch"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	// A node drain or eviction sends SIGTERM: cancel the run so it returns
	// and its deferred run record is written before the process exits.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	digestMode := flag.Bool("digest", false, "send the weekly digest to telegram-notifier instead of scraping")
	flag.Parse()

	cfg := config.Load()

	runMode := run
	if *digestMode {
		runMode = runDigest
	}
	if err := runMode(ctx, cfg); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}

// recordTimeout bounds writing the run records. It has its own context:
// the run's may already have expired.
const recordTimeout = 10 * time.Second

func run(ctx context.Context, cfg config.Config) (err error) {
	start := time.Now()

	// NATS first: the run log lives there, and a run that fails later
	// (the database, discovery) is still recorded.
	nc, err := natsconn.Connect(cfg.NatsURL, "scraper")
	if err != nil {
		return fmt.Errorf("connect to nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("create jetstream context: %w", err)
	}

	// The run log (see streams.RunsBucketName). Without it the run goes
	// on: the log and the exit code still show the outcome.
	runsKV, rerr := streams.EnsureRunsBucket(ctx, js)
	if rerr != nil {
		log.Printf("WARN %v; this run will not be recorded", rerr)
	}
	var hubs []discovery.HubStats
	defer func() {
		if runsKV == nil {
			return
		}
		records := discovery.RunRecords(cfg.HubPages, hubs, start, time.Now(), err, redactor(cfg))
		wctx, cancel := context.WithTimeout(context.Background(), recordTimeout)
		defer cancel()
		if werr := runs.Write(wctx, runsKV, records); werr != nil {
			log.Printf("WARN recording the run: %v", werr)
		}
	}()

	// Initialize Turso connection
	db, err := store.Connect(cfg.TursoDatabaseURL, cfg.TursoAuthToken)
	if err != nil {
		// Wrap error but avoid exposing auth token in error messages
		return fmt.Errorf("connect to turso (check credentials): %w", err)
	}
	defer db.Close()

	// Ensure streams exist
	if err := streams.EnsureStreams(ctx, js); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}

	// The hubs to scrape: the enabled watches (TICKETLINE_HUB_PAGES only
	// seeds an empty table). Recorded runs name these hubs too.
	// cfg.HubPages stays the env list until this succeeds, so a failure
	// here is still recorded against the configured hubs.
	hubPages, err := watch.Resolve(ctx, db, cfg.HubPages, time.Now())
	if err != nil {
		return err
	}
	if len(hubPages) == 0 {
		return errors.New("no enabled watch: add a hub page on the dashboard's Watches page, or enable one")
	}
	cfg.HubPages = hubPages

	// The scraper's own record of what it has published (see
	// streams.SeenBucketName).
	seen, err := streams.EnsureSeenBucket(ctx, js)
	if err != nil {
		return fmt.Errorf("ensure seen bucket: %w", err)
	}

	// Sessions the parser rejected recently (see
	// streams.SkippedBucketName).
	skipped, err := streams.EnsureSkippedBucket(ctx, js)
	if err != nil {
		return fmt.Errorf("ensure skipped bucket: %w", err)
	}

	// Create scraper instance
	scraper := discovery.New(cfg, db, js, seen, skipped)

	// Load seen event IDs (seen bucket plus the events table)
	if err := scraper.LoadKnownIDs(ctx); err != nil {
		return fmt.Errorf("load known event IDs: %w", err)
	}

	// Discover events
	derr := scraper.Discover(ctx)
	hubs = scraper.Stats.Hubs
	if derr != nil {
		log.Printf("Discovery encountered errors: %v", derr)
		return fmt.Errorf("discovery failed: %w", derr)
	}

	// Log summary
	log.Printf("Scraper finished: found=%d published=%d skipped=%d errors=%d",
		scraper.Stats.EventsFound,
		scraper.Stats.EventsPublished,
		scraper.Stats.Skipped,
		scraper.Stats.Errors)

	return nil
}

// runDigest is `scraper --digest`: build this week's digest from the
// events table and the run log and publish it once for telegram-notifier
// (see package digest). It scrapes nothing and records no run.
func runDigest(ctx context.Context, cfg config.Config) error {
	nc, err := natsconn.Connect(cfg.NatsURL, "scraper-digest")
	if err != nil {
		return fmt.Errorf("connect to nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("create jetstream context: %w", err)
	}
	if err := streams.EnsureStreams(ctx, js); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}
	runsKV, err := streams.EnsureRunsBucket(ctx, js)
	if err != nil {
		return fmt.Errorf("ensure runs bucket: %w", err)
	}

	db, err := store.Connect(cfg.TursoDatabaseURL, cfg.TursoAuthToken)
	if err != nil {
		return fmt.Errorf("connect to turso (check credentials): %w", err)
	}
	defer db.Close()

	return digest.Send(ctx, db, runs.NewReader(runsKV), js, time.Now())
}

// redactor returns a function that removes the database auth token from
// error text before it goes into the run log, which the dashboard serves
// unauthenticated. The driver's errors are not known to echo the DSN, but
// the record must not depend on that.
func redactor(cfg config.Config) func(string) string {
	return func(s string) string {
		if cfg.TursoAuthToken == "" {
			return s
		}
		return strings.ReplaceAll(s, cfg.TursoAuthToken, "REDACTED")
	}
}
