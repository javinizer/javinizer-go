package commandutil

import (
	"fmt"
	"io"
	"os"

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
	return store, store, nil
}
