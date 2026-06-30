package testkit

import (
	"context"
	"net/http"
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

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("Warning: failed to close test database: %v", err)
		}
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
