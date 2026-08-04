package sources

import (
	"fmt"
	"io"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	legacycsvsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/legacycsv"
	minnanoavsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/minnanoav"
	r18devsource "github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dev"
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
		return r18devsource.NewFromLister(store.ListActresses)
	})
	return store, nil
}
