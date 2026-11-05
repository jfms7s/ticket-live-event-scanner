// Package job holds the scraper's two jobs, a discovery run and the weekly
// digest, as functions over connections the caller owns. web-ui-api runs
// them on a schedule (package schedule); `web-ui-api --scrape-once` and
// `--digest-once` run one by hand.
package job

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/digest"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/discovery"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/watch"
	"github.com/nats-io/nats.go/jetstream"
)

// ScrapeTimeout bounds one discovery run.
const ScrapeTimeout = 15 * time.Minute

// recordTimeout bounds writing the run records. It has its own context:
// the run's may already have expired.
const recordTimeout = 10 * time.Second

// Deps are what the jobs run against. The caller has ensured the streams
// (streams.EnsureStreams).
type Deps struct {
	Config config.Config
	DB     *sql.DB
	JS     jetstream.JetStream
}

// Scrape runs discovery once: the enabled watches' hub pages are fetched
// and each new session is published. The outcome per hub is written to the
// run log, a failed run included.
func Scrape(ctx context.Context, d Deps) (err error) {
	cfg := d.Config
	start := time.Now()

	// The run log (see streams.RunsBucketName). Without it the run goes
	// on: the log and the error still show the outcome.
	runsKV, rerr := streams.EnsureRunsBucket(ctx, d.JS)
	if rerr != nil {
		log.Printf("WARN %v; this run will not be recorded", rerr)
	}
	var hubs []discovery.HubStats
	defer func() {
		if runsKV == nil || !shouldRecord(err) {
			return
		}
		records := discovery.RunRecords(cfg.HubPages, hubs, start, time.Now(), err)
		wctx, cancel := context.WithTimeout(context.Background(), recordTimeout)
		defer cancel()
		if werr := runs.Write(wctx, runsKV, records); werr != nil {
			log.Printf("WARN recording the run: %v", werr)
		}
	}()

	// The hubs to scrape: the enabled watches (TICKETLINE_HUB_PAGES only
	// seeds the table). cfg.HubPages stays the env list until this
	// succeeds, so a failure here is still recorded against the
	// configured hubs.
	hubPages, err := watch.Resolve(ctx, d.DB, cfg.HubPages, time.Now())
	if err != nil {
		return err
	}
	if len(hubPages) == 0 {
		return errors.New("no enabled watch: add a hub page on the dashboard's Watches page, or enable one")
	}
	cfg.HubPages = hubPages

	// The scraper's own record of what it has published (see
	// streams.SeenBucketName).
	seen, err := streams.EnsureSeenBucket(ctx, d.JS)
	if err != nil {
		return fmt.Errorf("ensure seen bucket: %w", err)
	}

	// Sessions the parser rejected recently (see streams.SkippedBucketName).
	skipped, err := streams.EnsureSkippedBucket(ctx, d.JS)
	if err != nil {
		return fmt.Errorf("ensure skipped bucket: %w", err)
	}

	scraper := discovery.New(cfg, d.DB, d.JS, seen, skipped)

	// Load seen event IDs (seen bucket plus the events table)
	if err := scraper.LoadKnownIDs(ctx); err != nil {
		return fmt.Errorf("load known event IDs: %w", err)
	}

	derr := scraper.Discover(ctx)
	hubs = scraper.Stats.Hubs
	if derr != nil {
		log.Printf("Discovery encountered errors: %v", derr)
		return fmt.Errorf("discovery failed: %w", derr)
	}

	log.Printf("Scraper finished: found=%d published=%d skipped=%d errors=%d",
		scraper.Stats.EventsFound,
		scraper.Stats.EventsPublished,
		scraper.Stats.Skipped,
		scraper.Stats.Errors)
	return nil
}

// shouldRecord says whether a run that ended with err goes into the run log.
// A run cut short because the process is shutting down (a deploy, an
// eviction) is not a failure of the hubs: recording it would turn the header
// red and, within the start-up gap, make the next pod skip its scrape. A
// timeout (DeadlineExceeded) is a real failure and is recorded.
func shouldRecord(err error) bool {
	return !errors.Is(err, context.Canceled)
}

// Digest builds this week's digest from the events table and the run log
// and publishes it once for the Telegram channel (see package digest). It
// scrapes nothing and records no run.
func Digest(ctx context.Context, d Deps) error {
	runsKV, err := streams.EnsureRunsBucket(ctx, d.JS)
	if err != nil {
		return fmt.Errorf("ensure runs bucket: %w", err)
	}
	return digest.Send(ctx, d.DB, runs.NewReader(runsKV), d.JS, time.Now())
}
