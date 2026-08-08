package sources

import (
	"context"
	"fmt"
	"io"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	legacycsvsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/legacycsv"
	minnanoavsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/minnanoav"
	r18devsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dev"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

// RegisterAll registers the self-contained cache sources. The r18dev source
// needs a dump store whose lifetime the caller owns — use RegisterR18Dev.
func RegisterAll(registry *actresscache.Registry) {
	registry.Register("legacy-jvthumbs", legacycsvsource.New)
	registry.Register("minnanoav", minnanoavsource.New)
}

// RegisterR18Dev opens the dump store at dumpPath and registers the r18dev
// source backed by it. The returned closer must be closed by the caller after
// the build finishes.
func RegisterR18Dev(registry *actresscache.Registry, dumpPath string) (io.Closer, error) {
	store, err := r18devdump.Open(dumpPath)
	if err != nil {
		return nil, fmt.Errorf("open r18.dev dump: %w", err)
	}
	registry.Register("r18dev", func() actresscache.Source {
		return r18devsource.NewFromLister(r18DevBoundedLister(store, r18devsource.MaxScanRows))
	})
	return store, nil
}

// r18DevBoundedLister caps the dump scan; cap+1 distinguishes a complete scan
// from a truncated one, and above-cap scans fail closed instead of feeding a
// partially-full cache to the builder (pruning would mark beyond-cap entries stale).
// A user-requested window (limit > 0) tightens the cap inside the query — it
// narrows but never widens the safety bound.
func r18DevBoundedLister(store *r18devdump.Store, maxRows int) r18devsource.Lister {
	return func(ctx context.Context, limit int) ([]models.DumpActress, error) {
		cap := maxRows
		userWindow := limit > 0 && limit < maxRows
		if userWindow {
			cap = limit
		}
		actresses, err := store.ListActressesLimit(ctx, cap+1)
		if err != nil {
			return nil, err
		}
		if len(actresses) > cap {
			if userWindow {
				// cap+1 rows is the window-overflow sentinel: return them so
				// the source marks the enumeration truncated.
				return actresses, nil
			}
			return nil, fmt.Errorf("r18dev dump exceeds the scan safety cap of %d actress rows; refusing to assemble a truncated cache (--limit caps, not widens, the scan window in this phase)", maxRows)
		}
		return actresses, nil
	}
}
