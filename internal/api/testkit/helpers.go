package testkit

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// Test helpers for creating mock repositories

// MockScraperWithResults implements Scraper and returns predefined results
// For security testing, it echoes back the ID in the result to verify sanitization
type MockScraperWithResults struct {
	name    string
	enabled bool
	result  *models.ScraperResult
	err     error
}

// Name returns the mock scraper's configured name.
func (m *MockScraperWithResults) Name() string {
	return m.name
}

// Search returns the scraper's predefined result, echoing id into the result ID.
func (m *MockScraperWithResults) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := *m.result
	result.ID = id
	return &result, nil
}

// GetURL returns an empty URL, satisfying the Scraper interface for tests.
func (m *MockScraperWithResults) GetURL(_ context.Context, id string) (string, error) {
	return "", nil
}

// IsEnabled reports whether the mock scraper is enabled.
func (m *MockScraperWithResults) IsEnabled() bool {
	return m.enabled
}

// Config returns an empty ScraperSettings, satisfying the Scraper interface for tests.
func (m *MockScraperWithResults) Config() *models.ScraperSettings {
	return &models.ScraperSettings{}
}

// Close is a no-op, satisfying the Scraper interface for tests.
func (m *MockScraperWithResults) Close() error {
	return nil
}

// NewMockScraperWithResults creates a new mock scraper with predefined results
func NewMockScraperWithResults(name string, enabled bool, result *models.ScraperResult, err error) *MockScraperWithResults {
	return &MockScraperWithResults{
		name:    name,
		enabled: enabled,
		result:  result,
		err:     err,
	}
}

// NewMockMovieRepo creates a test movie repository with in-memory database.
func NewMockMovieRepo() *database.MovieRepository {
	db, err := database.New(&database.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	})
	if err != nil {
		panic(err)
	}
	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		panic(err)
	}
	return database.NewMovieRepository(db)
}

// NewMockActressRepo creates a test actress repository with in-memory database.
func NewMockActressRepo() *database.ActressRepository {
	db, err := database.New(&database.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	})
	if err != nil {
		panic(err)
	}
	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		panic(err)
	}
	return database.NewActressRepository(db)
}

// NoOpAuth is a test-only commandutil.AuthProvider that bypasses authentication.
// The auth middleware type-asserts to an IsDisabled() capability; only NoOpAuth
// satisfies it, so the production *AuthManager always enforces real auth and this
// path is unreachable in production. Test setup helpers auto-wire NoOpAuth via
// GetTestRuntime so integration tests reach protected handlers without
// per-test credentials. Tests that assert the fail-closed path set
// deps.Auth = nil after retrieving the runtime.
type NoOpAuth struct{}

// SessionTTL returns a fixed one-hour TTL. Unused on the pass-through path.
func (NoOpAuth) SessionTTL() time.Duration { return time.Hour }

// PersistentSessionTTL returns a fixed 30-day TTL. Unused on the pass-through path.
func (NoOpAuth) PersistentSessionTTL() time.Duration { return 30 * 24 * time.Hour }

// IsInitialized always reports true. Unused on the pass-through path.
func (NoOpAuth) IsInitialized() bool { return true }

// AuthenticateSession always succeeds with a placeholder username.
func (NoOpAuth) AuthenticateSession(string) (string, error) { return "test", nil }

// Setup is a no-op that always succeeds.
func (NoOpAuth) Setup(string, string) error { return nil }

// Login always succeeds and returns a placeholder session ID.
func (NoOpAuth) Login(string, string, bool) (string, error) { return "test-session", nil }

// Logout is a no-op.
func (NoOpAuth) Logout(string) {}

// ValidateToken always succeeds and returns a placeholder token ID.
func (NoOpAuth) ValidateToken(context.Context, string) (string, error) { return "test-token", nil }

// UpdateTokenLastUsed is a no-op that always succeeds.
func (NoOpAuth) UpdateTokenLastUsed(context.Context, string) error { return nil }

// GetEnv always returns the empty string.
func (NoOpAuth) GetEnv(string) string { return "" }

// IsDisabled reports that authentication is bypassed. Only NoOpAuth implements
// this; the production *AuthManager does not, so the middleware's type assertion
// never matches in production.
func (NoOpAuth) IsDisabled() bool { return true }

// CreateTestDeps creates minimal *core.APIDeps for testing.
// Per DEEP-2: the APIRuntime is stored internally and can be retrieved
// via GetTestRuntime(deps). This avoids requiring all test callers to
// change their variable unpacking while still providing runtime access.
func CreateTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.APIDeps {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	dbCfg := &database.Config{
		Type: "sqlite",
		DSN:  dbPath + "?_journal_mode=WAL&_busy_timeout=10000",
	}

	db, err := database.New(dbCfg)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Register DB close immediately so it runs even if a later step fails
	// (e.g. RunMigrationsOnStartup) and calls t.Fatalf before the runtime
	// cleanup below is registered. LIFO ordering: this runs AFTER the runtime
	// shutdown cleanup (registered later), so the hub is stopped first.
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("Warning: failed to close test database: %v", err)
		}
		// On Windows, SQLite's file handles (and WAL sidecar files) may not be
		// released the instant db.Close() returns. database/sql closes pooled
		// connections, but the OS can hold the file open briefly, causing the
		// subsequent t.TempDir() RemoveAll to fail with "The process cannot access
		// the file because it is being used by another process." Wait for the DB
		// file to become removable (or timeout) so the LIFO RemoveAll cleanup
		// registered by t.TempDir() (which runs after this) succeeds.
		waitForFileRelease(t, dbPath)
	})

	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	// Initialize scraper registry
	registry := scraperutil.NewScraperRegistry()

	// Initialize repositories via db.Repositories()
	repos := db.Repositories()

	// Initialize job queue with jobRepo for persistence
	jobStore := worker.NewJobStore(repos.JobRepo, repos.BatchFileOpRepo, repos.MovieRepo, cfg.System.TempDir, nil, nil, worker.WithActressRepo(repos.ActressRepo))

	deps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: registry,
			DB:              db,
		},
		ConfigFile: configFile,
		Repos:      repos,
		JobStore:   jobStore,
	}

	// Create APIRuntime which owns mutable state
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	rt.EnsureRuntime()

	// Initialize web socket on the runtime for tests that need it
	rtState := rt.GetRuntime()
	rtState.ResetWebSocketHub()
	rtState.SetWebSocketUpgraderForTesting(websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	})

	// Store the runtime so tests can retrieve it via GetTestRuntime
	runtimeMap.Store(deps, rt)

	// Register cleanup AFTER all goroutine-spawning setup so the WebSocket hub
	// is shut down BEFORE the DB is closed. t.Cleanup is LIFO and t.TempDir()
	// registered its auto-RemoveAll first (above), so this runs before the dir
	// removal; the db.Close() cleanup registered above runs after this (LIFO),
	// so the hub is stopped first. Without the hub shutdown, the hub goroutine
	// outlives db.Close(), races the temp-dir RemoveAll, and the resulting
	// leftover poisons the shared parent temp dir for subsequent tests (cascade
	// "directory not empty" / "process cannot access the file" flakes).
	t.Cleanup(func() {
		if rtState != nil {
			rtState.Shutdown()
		}
		runtimeMap.Delete(deps)
	})

	return deps
}

// runtimeMap stores the APIRuntime associated with each APIDeps created by CreateTestDeps.
// This allows tests to retrieve the runtime without the APIDeps back-reference that
// was removed in DEEP-2.
var runtimeMap sync.Map

// GetTestRuntime returns the APIRuntime associated with the given APIDeps.
// If no runtime has been registered (e.g., the deps was created manually
// rather than via CreateTestDeps), a new APIRuntime is created, stored,
// and returned. This prevents nil-pointer dereferences in test helpers
// that construct *APIDeps directly and then call runtime methods.
func GetTestRuntime(deps *core.APIDeps) *core.APIRuntime {
	// Tests that don't care about auth get a pass-through NoOpAuth so the
	// fail-closed middleware (nil deps.Auth → 503) doesn't block them. Tests
	// that need real auth set deps.Auth before calling this; tests that need
	// to assert the fail-closed path set deps.Auth = nil AFTER this call.
	if deps != nil && deps.Auth == nil {
		deps.Auth = NoOpAuth{}
	}
	val, ok := runtimeMap.Load(deps)
	if ok {
		return val.(*core.APIRuntime)
	}

	// Auto-create and register a runtime for manually-constructed deps.
	// Initialize it the same way CreateTestDeps does (EnsureRuntime + SetConfig)
	// so handlers using rt.GetAPIConfig()/rt.GetRuntime() see populated state
	// rather than zero-value security/scanner settings. SetConfig is only safe
	// to call when the deps actually carries a config (HasConfig), since
	// CoreDeps.GetConfig panics on nil config by design.
	rt := core.NewAPIRuntime(deps)
	rt.EnsureRuntime()
	if deps.CoreDeps != nil && deps.CoreDeps.HasConfig() {
		rt.SetConfig(deps.CoreDeps.GetConfig())
	}
	runtimeMap.Store(deps, rt)
	return rt
}

// SetTestRuntime associates an APIRuntime with the given APIDeps.
// This is useful for test helpers that construct *APIDeps directly
// (without CreateTestDeps) and need to register the runtime so that
// GetTestRuntime can find it later.
func SetTestRuntime(deps *core.APIDeps, rt *core.APIRuntime) {
	runtimeMap.Store(deps, rt)
}

var wsTestMu sync.Mutex

// InitTestWebSocket initializes the given runtime's websocket state for tests.
func InitTestWebSocket(t *testing.T, runtime *core.RuntimeState) {
	t.Helper()

	wsTestMu.Lock()
	defer wsTestMu.Unlock()

	runtime.ResetWebSocketHub()
	runtime.SetWebSocketUpgraderForTesting(websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	})

	t.Cleanup(func() {
		runtime.Shutdown()
	})
}

// CleanupServerHub gracefully shuts down websocket runtime for the given APIRuntime.
func CleanupServerHub(t *testing.T, rt *core.APIRuntime) {
	t.Helper()
	if rt == nil {
		return
	}
	rtState := rt.GetRuntime()
	if rtState != nil {
		rtState.Shutdown()
	}
	time.Sleep(100 * time.Millisecond)
}

// waitForFileRelease waits until the SQLite DB file and its WAL/SHM sidecars
// can be removed, which on Windows may lag behind db.Close() because the OS
// holds file handles open briefly. It polls at a fixed 20ms interval (capped to
// the remaining deadline) for up to ~2s.
// On Unix this is effectively a no-op (files are deletable while open) but it
// runs unconditionally to keep the code path uniform.
//
// This directly addresses the Windows CI flake where t.TempDir()'s auto-
// RemoveAll failed with "The process cannot access the file because it is being
// used by another process" on test.db during teardown of batch tests that
// exercise the apply phase (which opens pooled SQLite connections).
//
// Probe strategy: os.Rename fails on Windows when the source file is open by
// another process (sharing violation), so renaming the DB file to a sibling
// and back is a faithful test of "is this file removable" without actually
// deleting it (the LIFO t.TempDir() RemoveAll owns deletion). The -wal/-shm
// sidecars are probed directly with os.Remove (SQLite recreates them as
// needed, so removing them is safe and matches the exact RemoveAll failure
// mode on those files).
var (
	// waitForFileReleaseDeadline is the maximum time waitForFileRelease polls
	// before giving up. Overridable in tests to keep the suite fast.
	waitForFileReleaseDeadline = 2 * time.Second
	// waitForFileReleasePollInterval is the sleep between polls.
	waitForFileReleasePollInterval = 20 * time.Millisecond
)

func waitForFileRelease(t *testing.T, dbPath string) {
	t.Helper()
	deadline := time.Now().Add(waitForFileReleaseDeadline)
	sidecars := []string{dbPath + "-wal", dbPath + "-shm"}
	for time.Now().Before(deadline) {
		if dbFileReleased(dbPath) && allSidecarsRemoved(sidecars) {
			return
		}
		// Sleep with a cap so a long step can't overshoot the deadline.
		remaining := time.Until(deadline)
		sleep := waitForFileReleasePollInterval
		if sleep > remaining {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
	t.Logf("waitForFileRelease: DB file still locked after %s; teardown RemoveAll may fail on Windows", waitForFileReleaseDeadline)
}

// dbFileReleased reports whether the main DB file is removable on Windows by
// attempting a rename to a sibling path and back. os.Rename fails with a
// sharing violation on Windows if any process still holds the file open.
// renameFunc is the os.Rename seam, overridable in tests to simulate restore
// failures that are otherwise impossible to trigger deterministically
// single-threaded (the rename-back happens inside dbFileReleased).
var renameFunc = os.Rename

func dbFileReleased(dbPath string) bool {
	// A stranded probe from a prior restore-failure means the DB is still
	// locked: clean it up before deciding, otherwise the absent-DB fast path
	// below would return true while the probe file lingers and trips the
	// RemoveAll this helper exists to protect.
	probe := dbPath + ".release-probe"
	if _, err := os.Stat(probe); err == nil {
		if err := os.Remove(probe); err != nil {
			return false // still locked — keep polling
		}
	} else if !os.IsNotExist(err) {
		// A non-IsNotExist stat error (permission/IO) means we can't determine
		// whether a stranded probe exists — treat as not-released so polling
		// continues rather than risk returning true while the probe lingers.
		return false
	}
	// An absent DB file has nothing to unlock — treat it as released so
	// waitForFileRelease returns immediately instead of spinning to the deadline.
	if _, err := os.Stat(dbPath); err != nil && os.IsNotExist(err) {
		return true
	}
	if err := renameFunc(dbPath, probe); err != nil {
		return false
	}
	// Restore the file to its original path. If this fails the DB is stranded
	// at the probe path, so report not-released so the caller keeps polling
	// (and eventually logs) rather than masking the restore failure.
	if err := renameFunc(probe, dbPath); err != nil {
		// Best-effort cleanup of the stranded probe so the next poll is clean.
		_ = os.Remove(probe)
		return false
	}
	return true
}

// allSidecarsRemoved attempts to remove each sidecar file, returning true only
// when all are gone (already absent or successfully removed here). It ignores
// only os.IsNotExist errors; any other remove error (e.g. a transient Windows
// sharing violation) is treated as "still locked" so waitForFileRelease keeps
// polling rather than returning early and reintroducing the RemoveAll flake.
// The -wal/-shm files are safe to delete because SQLite recreates them on
// next access, and t.TempDir()'s RemoveAll would delete them anyway.
func allSidecarsRemoved(sidecars []string) bool {
	for _, p := range sidecars {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return false
		}
	}
	return true
}
