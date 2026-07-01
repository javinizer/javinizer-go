package r18dev

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_RegistryConstruction_DumpInjected is a registry-level integration
// test: it constructs the r18.dev scraper through the real production path
// (scraperutil.ScraperRegistry → r18dev.Register → InitInstances →
// Constructor → newScraper) rather than calling newScraper directly. This
// catches wiring regressions in the ScraperDeps.R18DevDump → constructor →
// dumpLookup field → resolver chain that the direct-construction unit tests
// in dump_lookup_test.go bypass.
//
// It verifies that the live instance returned from the registry has a
// non-nil dumpLookup that resolves a known dvd_id, and that ResolveURL
// returns the dump-derived content_id URL with zero HTTP.
func TestE2E_RegistryConstruction_DumpInjected(t *testing.T) {
	// Seed a real dump sidecar on disk.
	dumpPath := filepath.Join(t.TempDir(), "r18dev_dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" +
		"118ipx00535\tIPX-535\n" +
		"\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), dumpPath, r18devdump.ImportOptions{
		SourceDate: "2026-04-28",
	})
	require.NoError(t, err)

	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Build the registry and register r18dev exactly as production does.
	reg := scraperutil.NewScraperRegistry()
	Register(reg)

	// Construct ScraperDeps with the dump lookup injected — this is the
	// production wiring from commandutil.OpenR18DevDumpLookup.
	settings := models.ScraperSettings{
		Enabled:  true,
		Language: "en",
	}
	depsMap := map[string]scraperutil.ScraperDeps{
		"r18dev": {
			Settings:       settings,
			R18DevDump:     store,
			TimeoutSeconds: 30,
		},
	}
	require.NoError(t, reg.InitInstances(depsMap))

	// Fetch the live instance and type-assert to the concrete *scraper so we
	// can inspect the unexported dumpLookup field.
	instance, ok := reg.GetInstance("r18dev")
	require.True(t, ok, "r18dev should be registered")
	require.NotNil(t, instance)

	s, ok := instance.(*scraper)
	require.True(t, ok, "instance should be *scraper")
	assert.NotNil(t, s.dumpLookup, "dumpLookup should be wired through the constructor")

	// Behaviorally verify the dump is reachable through the live instance:
	// Search must resolve via the dump with ZERO HTTP. A recording transport
	// proves no request was issued — the dump fast-path (searchFromDump) is
	// the sole entry point now, not ResolveURL.
	s.client.SetRetryCount(0)
	rt := &recordingTransport{err: errHTTPBlocked}
	s.client.SetTransport(rt)
	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err, "Search should resolve from the dump")
	require.NotNil(t, result)
	assert.Equal(t, 0, rt.count(), "dump-hit Search must issue zero HTTP requests")
	assert.Equal(t, "118ipx00535", result.ContentID, "ContentID should come from the dump")
}

// TestE2E_RegistryConstruction_NilDumpIsHTTPFallback verifies that when
// ScraperDeps.R18DevDump is nil (the default before `javinizer dump download`),
// the registry-constructed scraper has a nil dumpLookup and ResolveURL falls
// through to the HTTP probing path. With no HTTP server configured, the
// resolver returns false — proving the nil-dump path does not panic or
// short-circuit incorrectly.
func TestE2E_RegistryConstruction_NilDumpIsHTTPFallback(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	Register(reg)

	settings := models.ScraperSettings{Enabled: true, Language: "en"}
	depsMap := map[string]scraperutil.ScraperDeps{
		"r18dev": {
			Settings:       settings,
			R18DevDump:     nil, // no dump downloaded
			TimeoutSeconds: 30,
		},
	}
	require.NoError(t, reg.InitInstances(depsMap))

	instance, ok := reg.GetInstance("r18dev")
	require.True(t, ok)

	s, ok := instance.(*scraper)
	require.True(t, ok)
	assert.Nil(t, s.dumpLookup, "dumpLookup should be nil when not injected")

	// With no dump, Search falls straight through to HTTP. A recording
	// transport proves HTTP is attempted (count > 0) and, with every request
	// blocked, Search returns an error — hermetically, no real network.
	s.client.SetRetryCount(0)
	rt := &recordingTransport{err: errHTTPBlocked}
	s.client.SetTransport(rt)
	_, err := s.Search(context.Background(), "NOPE-999")
	assert.Error(t, err, "nil dump should fall back to HTTP which fails")
	assert.Greater(t, rt.count(), 0, "nil dump should attempt HTTP")
}

// TestE2E_RegistryConstruction_CloseReleasesDump verifies that Close() on the
// registry-constructed scraper is safe when a dump lookup is wired (the dump
// store is owned by the caller, not the scraper, so Close must not close it).
func TestE2E_RegistryConstruction_CloseDoesNotCloseDump(t *testing.T) {
	dumpPath := filepath.Join(t.TempDir(), "r18dev_dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), dumpPath, r18devdump.ImportOptions{})
	require.NoError(t, err)

	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	reg := scraperutil.NewScraperRegistry()
	Register(reg)
	depsMap := map[string]scraperutil.ScraperDeps{
		"r18dev": {Settings: models.ScraperSettings{Enabled: true, Language: "en"}, R18DevDump: store},
	}
	require.NoError(t, reg.InitInstances(depsMap))

	instance, _ := reg.GetInstance("r18dev")
	s := instance.(*scraper)

	// The scraper's Close() must not close the dump store — the caller owns
	// that lifecycle (via CoreDeps.Close / commandutil).
	require.NoError(t, s.Close())

	// The dump store must still be usable after the scraper closes.
	cid, err := store.LookupByDVDID(context.Background(), "IPX-535")
	require.NoError(t, err, "dump store should still work after scraper.Close()")
	assert.Equal(t, "118ipx00535", cid)
}
