// Package health writes a liveness heartbeat file that an exec probe can
// check, for services that have no HTTP port.
package health

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// DefaultFile is where Heartbeat writes when HEALTH_FILE is unset.
const DefaultFile = "/tmp/healthy"

// CheckTimeout bounds one call of the check function.
const CheckTimeout = 2 * time.Second

// Heartbeat runs check now and then every interval until ctx is done. Each
// time check succeeds it writes the current Unix time to path, so a probe
// can fail once the file is older than a few intervals. A failing check
// leaves the file alone.
func Heartbeat(ctx context.Context, path string, interval time.Duration, check func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		beat(ctx, path, check)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func beat(ctx context.Context, path string, check func(context.Context) error) {
	checkCtx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	if err := check(checkCtx); err != nil {
		if ctx.Err() == nil {
			log.Printf("Health check failed: %v", err)
		}
		return
	}
	if err := writeStamp(path, time.Now().Unix()); err != nil {
		log.Printf("Writing health file %s: %v", path, err)
	}
}

// writeStamp replaces path atomically, so the probe never reads a
// half-written file.
func writeStamp(path string, unix int64) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".healthy-*")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(strconv.FormatInt(unix, 10) + "\n")
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
