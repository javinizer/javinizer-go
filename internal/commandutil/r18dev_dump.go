package commandutil

import (
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
// It returns (nil, nil) — meaning "no local lookup available, fall back to
// HTTP" — when the feature is disabled or the dump file has not been
// downloaded yet. A non-nil closer is returned alongside the lookup so callers
// (CLI Close, API hot-reload) can release the file handle.
func OpenR18DevDumpLookup(cfg *config.Config) (models.R18DevDumpLookup, io.Closer) {
	if cfg == nil {
		return nil, nil
	}
	rc := cfg.Metadata.R18DevDump
	if !rc.Enabled {
		return nil, nil
	}
	path := rc.Path
	if path == "" {
		path = DefaultR18DevDumpPath
	}
	if _, err := os.Stat(path); err != nil {
		// Not downloaded yet. Not an error — the scraper falls back to HTTP.
		logging.Debugf("R18.dev dump lookup: %s not present, using HTTP fallback", path)
		return nil, nil
	}
	store, err := r18devdump.Open(path)
	if err != nil {
		logging.Warnf("R18.dev dump lookup disabled: failed to open %s: %v", path, err)
		return nil, nil
	}
	logging.Infof("R18.dev dump lookup enabled: %s", path)
	return store, store
}
