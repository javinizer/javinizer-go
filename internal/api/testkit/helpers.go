package testkit

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// Test helpers for creating mock repositories

// mockScraperWithResults implements Scraper and returns predefined results
// For security testing, it echoes back the ID in the result to verify sanitization
type MockScraperWithResults struct {
	name    string
	enabled bool
	result  *models.ScraperResult
	err     error
}

func (m *MockScraperWithResults) Name() string {
	return m.name
}

func (m *MockScraperWithResults) Search(_ context.Context, id string) (*models.ScraperResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := *m.result
	result.ID = id
	return &result, nil
}

func (m *MockScraperWithResults) GetURL(id string) (string, error) {
	return "", nil
}

func (m *MockScraperWithResults) IsEnabled() bool {
	return m.enabled
}

func (m *MockScraperWithResults) Config() *config.ScraperSettings {
	return &config.ScraperSettings{}
}

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
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(cfg)
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(); err != nil {
		panic(err)
	}
	return database.NewMovieRepository(db)
}

// NewMockActressRepo creates a test actress repository with in-memory database.
func NewMockActressRepo() *database.ActressRepository {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(cfg)
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(); err != nil {
		panic(err)
	}
	return database.NewActressRepository(db)
}

// CreateTestDeps creates minimal ServerDependencies for testing.
func CreateTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.ServerDependencies {
	t.Helper()

	// Initialize in-memory database with a unique name per test to avoid cross-test pollution
	// Using file:TESTNAME:?mode=memory&cache=shared&_busy_timeout=5000 ensures:
	// 1. Isolation between different tests (each gets its own DB)
	// 2. Shared cache within a test (concurrent goroutines see same data)
	// 3. Busy timeout (5s) reduces "database is locked" errors in concurrent scenarios
	// Note: WAL mode is not compatible with in-memory databases
	dbName := t.Name()
	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  "file:" + dbName + ":?mode=memory&cache=shared&_busy_timeout=5000",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(dbCfg)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	// Initialize repositories
	movieRepo := database.NewMovieRepository(db)
	actressRepo := database.NewActressRepository(db)
	jobRepo := database.NewJobRepository(db)
	batchFileOpRepo := database.NewBatchFileOperationRepository(db)

	// Initialize scraper registry
	registry := models.NewScraperRegistry()

	// Initialize aggregator
	agg := aggregator.NewWithDatabase(cfg, db)

	// Initialize matcher
	mat, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	// Initialize job queue with jobRepo for persistence
	jobQueue := worker.NewJobQueue(jobRepo, "")

	deps := &core.ServerDependencies{
		ConfigFile:      configFile,
		Registry:        registry,
		DB:              db,
		Aggregator:      agg,
		MovieRepo:       movieRepo,
		ActressRepo:     actressRepo,
		JobRepo:         jobRepo,
		BatchFileOpRepo: batchFileOpRepo,
		Matcher:         mat,
		JobQueue:        jobQueue,
		Runtime:         core.NewRuntimeState(),
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)
	core.SetDefaultRuntimeState(deps.Runtime)

	return deps
}

var wsTestMu sync.Mutex

// InitTestWebSocket initializes runtime websocket state for tests.
func InitTestWebSocket(t *testing.T) {
	t.Helper()

	wsTestMu.Lock()
	defer wsTestMu.Unlock()

	runtime := core.DefaultRuntimeState()
	if runtime == nil {
		runtime = core.NewRuntimeState()
		core.SetDefaultRuntimeState(runtime)
	}

	runtime.ResetWebSocketHub()
	runtime.SetWebSocketUpgraderForTesting(websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	})

	t.Cleanup(func() {
		runtime.Shutdown()
	})
}

// CleanupServerHub gracefully shuts down websocket runtime for dependencies.
func CleanupServerHub(t *testing.T, deps *core.ServerDependencies) {
	t.Helper()
	if deps == nil {
		return
	}
	deps.Shutdown()
	time.Sleep(100 * time.Millisecond)
}

// CurrentHub returns the default websocket hub for assertions.
func CurrentHub() *ws.Hub {
	runtime := core.DefaultRuntimeState()
	if runtime == nil {
		return nil
	}
	return runtime.WebSocketHub()
}

// StartStandaloneHub starts a websocket hub with cancellation for dedicated tests.
func StartStandaloneHub() (*ws.Hub, context.CancelFunc, <-chan struct{}) {
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		hub.Run(ctx)
		close(done)
	}()
	return hub, cancel, done
}
