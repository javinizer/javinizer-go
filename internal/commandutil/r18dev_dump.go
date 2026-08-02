package commandutil

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

// DefaultR18DevDumpPath is the sidecar SQLite path used when the config leaves
// the path empty. Relative to the working directory, matching the main DB.
const DefaultR18DevDumpPath = "data/r18dev/r18dev_dump.db"

// OpenR18DevDumpLookup opens the local r18.dev dump sidecar described by cfg.
// It returns (nil, nil, nil) — meaning "no local lookup available, fall back to
// HTTP" — when the feature is disabled or the dump file has not been
// downloaded yet (ENOENT). These are expected states, not errors.
//
// A non-nil error is returned when the dump is configured and present on disk
// but cannot be stat'd or opened (e.g. permission denied, I/O error, corrupt
// file). Surfacing these — rather than downgrading them to a clean (nil,nil)
// fallback — lets callers distinguish a genuinely broken dump setup from one
// that was simply never downloaded, so the failure is diagnosable instead of
// silently looking absent. A non-nil closer is returned alongside the lookup
// so callers (CLI Close, API hot-reload) can release the file handle.
func OpenR18DevDumpLookup(cfg *config.Config) (models.R18DevDumpLookup, io.Closer, error) {
	if cfg == nil {
		return nil, nil, nil
	}
	rc := cfg.Metadata.R18DevDump
	if !rc.Enabled {
		return nil, nil, nil
	}
	path := rc.Path
	if path == "" {
		path = DefaultR18DevDumpPath
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			// Not downloaded yet. Not an error — the scraper falls back to HTTP.
			logging.Debugf("R18.dev dump lookup: %s not present, using HTTP fallback", path)
			return nil, nil, nil
		}
		// A real filesystem problem (permission denied, I/O error). Surface it
		// rather than downgrading to a clean fallback so a broken dump setup is
		// diagnosable instead of indistinguishable from "never downloaded".
		return nil, nil, fmt.Errorf("r18.dev dump lookup disabled: cannot stat %s: %w", path, err)
	}
	store, err := r18devdump.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("r18.dev dump lookup disabled: failed to open %s: %w", path, err)
	}
	logging.Infof("R18.dev dump lookup enabled: %s", path)
	// Ref-count the handle so hot-reload can swap in a new dump without
	// closing the one in-flight queries still reference: Close only releases
	// SQLite once the last lookup drains; late callers degrade to HTTP.
	dump := &refCountedDumpLookup{inner: store, closer: store}
	return dump, dump, nil
}

// refCountedDumpLookup guards the dump handle against mid-query replacement.
type refCountedDumpLookup struct {
	inner  models.R18DevDumpLookup
	closer io.Closer

	mu     sync.Mutex
	closed bool
	active int
}

func (r *refCountedDumpLookup) acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.active++
	return true
}

func (r *refCountedDumpLookup) release() {
	r.mu.Lock()
	r.active--
	drained := r.closed && r.active == 0
	r.mu.Unlock()
	if drained {
		_ = r.closer.Close()
	}
}

// Close marks the dump retired; the handle is released once the last
// in-flight lookup drains (immediately when idle).
func (r *refCountedDumpLookup) Close() error {
	r.mu.Lock()
	r.closed = true
	drained := r.active == 0
	r.mu.Unlock()
	if !drained {
		return nil
	}
	return r.closer.Close()
}

func (r *refCountedDumpLookup) LookupByDVDID(ctx context.Context, dvdID string) (string, error) {
	if !r.acquire() {
		return "", models.ErrDumpMiss
	}
	defer r.release()
	return r.inner.LookupByDVDID(ctx, dvdID)
}

func (r *refCountedDumpLookup) LookupByContentID(ctx context.Context, contentID string) (string, error) {
	if !r.acquire() {
		return "", models.ErrDumpMiss
	}
	defer r.release()
	return r.inner.LookupByContentID(ctx, contentID)
}

func (r *refCountedDumpLookup) LookupMovie(ctx context.Context, dvdID string) (*models.DumpMovie, error) {
	if !r.acquire() {
		return nil, models.ErrDumpMiss
	}
	defer r.release()
	return r.inner.LookupMovie(ctx, dvdID)
}

func (r *refCountedDumpLookup) Stats(ctx context.Context) (models.DumpStats, error) {
	if !r.acquire() {
		return models.DumpStats{}, models.ErrDumpMiss
	}
	defer r.release()
	return r.inner.Stats(ctx)
}
