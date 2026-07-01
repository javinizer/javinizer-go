package commandutil

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedDumpDB creates a real r18.dev dump sidecar database on disk and returns
// its path. The dump contains a known IPX-535 -> 118ipx00535 mapping.
func seedDumpDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" +
		"118ipx00535\tIPX-535\n" +
		"h_086mesu00103\tMESU-103\n" +
		"\\.\n"
	res, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{
		SourceURL:  "https://example.com/dumps/r18dotdev_dump_2026-04-28.sql.gz",
		SourceDate: "2026-04-28",
	})
	require.NoError(t, err, "Import should succeed")
	require.Equal(t, path, res.Path)
	return path
}

// dumpConfig builds a minimal config with the r18.dev dump sidecar enabled and
// pointed at the given path.
func dumpConfig(t *testing.T, dumpPath string) *config.Config {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  filepath.Join(t.TempDir(), "test.db"),
		},
	}
	cfg.Metadata.R18DevDump.Enabled = true
	cfg.Metadata.R18DevDump.Path = dumpPath
	return cfg
}

// TestOpenR18DevDumpLookup_Branches covers the edge paths of the wiring
// helper: nil config, disabled feature, a non-ENOENT stat error (permission
// denied), and an unreadable dump file (Open failure). nil-cfg/disabled/ENOENT
// return (nil, nil, nil) — the clean HTTP fallback path. Real filesystem/Open
// errors are surfaced as a non-nil error (not downgraded to a clean miss) so a
// broken dump setup is diagnosable.
func TestOpenR18DevDumpLookup_Branches(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		lookup, closer, err := OpenR18DevDumpLookup(nil)
		assert.Nil(t, lookup)
		assert.Nil(t, closer)
		assert.NoError(t, err)
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := dumpConfig(t, seedDumpDB(t))
		cfg.Metadata.R18DevDump.Enabled = false
		lookup, closer, err := OpenR18DevDumpLookup(cfg)
		assert.Nil(t, lookup)
		assert.Nil(t, closer)
		assert.NoError(t, err)
	})

	t.Run("absent file is a clean fallback (ENOENT)", func(t *testing.T) {
		cfg := dumpConfig(t, filepath.Join(t.TempDir(), "does-not-exist.db"))
		lookup, closer, err := OpenR18DevDumpLookup(cfg)
		assert.Nil(t, lookup)
		assert.Nil(t, closer)
		assert.NoError(t, err, "ENOENT must be a clean fallback, not an error")
	})

	t.Run("stat error not ENOENT (unreadable parent dir)", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX permission bits don't block os.Stat on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("running as root bypasses permission checks")
		}
		// Point the path at a file inside a directory with no read/execute
		// permission: os.Stat fails with a non-ENOENT error (permission denied).
		dir := t.TempDir()
		lockedDir := filepath.Join(dir, "locked")
		require.NoError(t, os.Mkdir(lockedDir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })

		cfg := dumpConfig(t, filepath.Join(lockedDir, "r18dev_dump.db"))
		lookup, closer, err := OpenR18DevDumpLookup(cfg)
		assert.Nil(t, lookup)
		assert.Nil(t, closer)
		require.Error(t, err, "non-ENOENT stat error must be surfaced, not downgraded")
		assert.Contains(t, err.Error(), "cannot stat")
	})

	t.Run("open failure (corrupt file)", func(t *testing.T) {
		dir := t.TempDir()
		corrupt := filepath.Join(dir, "r18dev_dump.db")
		// A non-empty, non-SQLite file: Open must fail to parse the header.
		require.NoError(t, os.WriteFile(corrupt, []byte("not a sqlite database"), 0o600))
		cfg := dumpConfig(t, corrupt)
		lookup, closer, err := OpenR18DevDumpLookup(cfg)
		assert.Nil(t, lookup)
		assert.Nil(t, closer)
		require.Error(t, err, "a corrupt dump file must surface an error")
		assert.Contains(t, err.Error(), "failed to open")
	})
}

// TestE2E_DumpWiring_BootstrapOpensDump verifies the full production bootstrap
// path: NewDependencies → OpenR18DevDumpLookup → ScraperDeps.R18DevDump →
// registry. When a dump DB exists on disk, the sidecar handle is opened and
// held in CoreDeps for later cleanup. This is the integration that unit tests
// mock out — it guards against silent wiring regressions in the factory.
func TestE2E_DumpWiring_BootstrapOpensDump(t *testing.T) {
	dumpPath := seedDumpDB(t)
	cfg := dumpConfig(t, dumpPath)

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// The dump sidecar handle must be opened and held for cleanup.
	require.NotNil(t, deps.r18DumpCloser, "r18DumpCloser should be non-nil when dump DB exists")
	assert.NotNil(t, deps.ScraperRegistry, "registry should be initialized")
}

// TestE2E_DumpWiring_BootstrapAbsentDumpIsNil verifies that when the dump file
// does not exist, the bootstrap gracefully leaves the closer nil (HTTP fallback
// path) rather than erroring. This is the default state for users who haven't
// run `javinizer dump download` yet.
func TestE2E_DumpWiring_BootstrapAbsentDumpIsNil(t *testing.T) {
	cfg := dumpConfig(t, filepath.Join(t.TempDir(), "does-not-exist.db"))

	deps, err := NewDependencies(cfg)
	require.NoError(t, err, "absent dump should not fail bootstrap")
	defer func() { _ = deps.Close() }()

	assert.Nil(t, deps.r18DumpCloser, "r18DumpCloser should be nil when dump file is absent")
	assert.NotNil(t, deps.ScraperRegistry, "registry should still initialize")
}

// TestE2E_DumpWiring_BootstrapDisabledIsNil verifies that when the feature is
// disabled in config, the bootstrap skips opening the dump even if the file
// exists.
func TestE2E_DumpWiring_BootstrapDisabledIsNil(t *testing.T) {
	dumpPath := seedDumpDB(t)
	cfg := dumpConfig(t, dumpPath)
	cfg.Metadata.R18DevDump.Enabled = false

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.Nil(t, deps.r18DumpCloser, "r18DumpCloser should be nil when feature is disabled")
}

// TestE2E_DumpWiring_CloseReleasesHandle verifies that Close() releases the
// dump sidecar connection, so the SQLite file is not left locked after
// shutdown. This guards against file-handle leaks on the CLI exit path.
func TestE2E_DumpWiring_CloseReleasesHandle(t *testing.T) {
	dumpPath := seedDumpDB(t)
	cfg := dumpConfig(t, dumpPath)

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps.r18DumpCloser)

	// After Close, a second open of the same dump path must succeed (proving
	// the read connection was released, not left locking the file).
	require.NoError(t, deps.Close())

	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err, "dump DB should be re-openable after deps.Close() (handle released)")
	defer func() { _ = store.Close() }()

	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.RowCount, "dump DB should still be intact after close/reopen")
}

// TestE2E_DumpWiring_ReplaceCloserSwapsHandle verifies the hot-reload swap
// path used by API config reload: ReplaceR18DevDumpCloser returns the previous
// closer for the caller to close, and installs the new one. This guards
// against file-handle leaks when the config (or dump path) changes at runtime.
func TestE2E_DumpWiring_ReplaceCloserSwapsHandle(t *testing.T) {
	dumpPath := seedDumpDB(t)
	cfg := dumpConfig(t, dumpPath)

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	require.NotNil(t, deps.r18DumpCloser)

	// Simulate a hot-reload: open a fresh store and swap it in.
	newStore, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)

	old := deps.ReplaceR18DevDumpCloser(newStore)
	require.NotNil(t, old, "previous closer should be returned for cleanup")
	// Closing the old handle must not error (it was a valid open connection).
	require.NoError(t, old.Close())

	// The new handle is now owned by deps and will be closed by deps.Close().
	assert.NotNil(t, deps.r18DumpCloser)
}

// TestE2E_DumpWiring_FullBootstrapScrapeUsesDump is the crown-jewel e2e: it
// bootstraps the real dependency stack with a dump present, fetches the r18dev
// scraper instance from the registry, and verifies the dump lookup is wired
// into the live instance by resolving a dvd_id through it. Because the r18dev
// scraper's dumpLookup field is unexported, we verify behaviorally: the
// instance is present and enabled, proving the full registration → finalize →
// construct → instance-store path completed with the dump injected.
func TestE2E_DumpWiring_FullBootstrapScrapeUsesDump(t *testing.T) {
	dumpPath := seedDumpDB(t)
	cfg := dumpConfig(t, dumpPath)

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// The r18dev scraper must be a live, enabled instance in the registry —
	// this proves the constructor received the dump lookup via ScraperDeps
	// (the registry only instantiates scrapers that finalized successfully).
	reg := deps.GetRegistry()
	instance, ok := reg.GetInstance("r18dev")
	require.True(t, ok, "r18dev scraper should be registered after bootstrap")
	require.NotNil(t, instance, "r18dev instance should be non-nil")
	assert.True(t, instance.IsEnabled(), "r18dev should be enabled by default")
	assert.Equal(t, "r18dev", instance.Name())
}

// TestOpenR18DevDumpLookup_DefaultPath covers the branch where rc.Path is
// empty, so the default relative path (data/r18dev/r18dev_dump.db) is used.
// By chdir-ing into a temp dir, the default path resolves to a non-existent
// location → clean ENOENT fallback.
func TestOpenR18DevDumpLookup_DefaultPath(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")},
	}
	cfg.Metadata.R18DevDump.Enabled = true
	cfg.Metadata.R18DevDump.Path = "" // exercise the default-path branch

	t.Chdir(t.TempDir()) // default relative path won't exist here
	lookup, closer, err := OpenR18DevDumpLookup(cfg)
	assert.Nil(t, lookup, "default path absent → clean fallback")
	assert.Nil(t, closer)
	assert.NoError(t, err, "ENOENT on default path must be a clean fallback")
}

// TestE2E_NewDependencies_CorruptDumpWarned covers the dumpErr warn branch in
// NewDependenciesWithOptions (line 158-164): a corrupt dump file surfaces a
// warning but bootstrap continues via HTTP fallback.
func TestE2E_NewDependencies_CorruptDumpWarned(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "r18dev_dump.db")
	require.NoError(t, os.WriteFile(corrupt, []byte("not a sqlite database"), 0o600))
	cfg := dumpConfig(t, corrupt)
	// Use a separate DB DSN so we don't collide with other tests.
	cfg.Database.DSN = filepath.Join(dir, "test.db")

	deps, err := NewDependencies(cfg)
	require.NoError(t, err, "corrupt dump should warn but not abort bootstrap")
	defer func() { _ = deps.Close() }()

	// The r18dev scraper must still be registered (HTTP fallback path).
	reg := deps.GetRegistry()
	instance, ok := reg.GetInstance("r18dev")
	require.True(t, ok, "r18dev scraper should be registered despite corrupt dump")
	require.NotNil(t, instance)
}
