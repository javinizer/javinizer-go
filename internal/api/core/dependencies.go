package core

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// AuthProvider is the minimal auth contract used by API handlers.
type AuthProvider interface {
	SessionTTL() time.Duration
	IsInitialized() bool
	AuthenticateSession(sessionID string) (string, error)
	Setup(username, password string) error
	Login(username, password string, rememberMe bool) (string, error)
	Logout(sessionID string)
}

// ServerDependencies holds all dependencies needed to create the API server.
// Access to Config, Registry, Aggregator, and Matcher must be synchronized
// to prevent data races during config reload.
type ServerDependencies struct {
	mu              sync.RWMutex
	config          atomic.Pointer[config.Config]
	ConfigFile      string
	Registry        *models.ScraperRegistry
	DB              *database.DB
	Aggregator      *aggregator.Aggregator
	MovieRepo       *database.MovieRepository
	ActressRepo     *database.ActressRepository
	HistoryRepo     *database.HistoryRepository
	JobRepo         *database.JobRepository
	BatchFileOpRepo *database.BatchFileOperationRepository
	EventRepo       *database.EventRepository
	EventEmitter    eventlog.EventEmitter
	Reverter        *history.Reverter
	Matcher         *matcher.Matcher
	JobQueue        *worker.JobQueue
	Auth            AuthProvider
	Runtime         *RuntimeState
	TokenStore      *TokenStore
}

// EnsureRuntime initializes runtime state when absent.
func (d *ServerDependencies) EnsureRuntime() *RuntimeState {
	if d.Runtime == nil {
		d.Runtime = NewRuntimeState()
	}
	return d.Runtime
}

// GetConfig returns the current configuration (thread-safe).
func (d *ServerDependencies) GetConfig() *config.Config {
	cfg := d.config.Load()
	if cfg == nil {
		logging.Errorf("CRITICAL: GetConfig() called before SetConfig() - this is a programming error")
		panic("GetConfig() called with nil config - ensure SetConfig() is called during ServerDependencies initialization")
	}
	return cfg
}

// SetConfig atomically sets the configuration (thread-safe).
func (d *ServerDependencies) SetConfig(cfg *config.Config) {
	if cfg == nil {
		logging.Errorf("CRITICAL: SetConfig() called with nil config - this is a programming error")
		panic("SetConfig() called with nil config - config must not be nil")
	}
	d.config.Store(cfg)
}

// Shutdown gracefully shuts down runtime resources.
func (d *ServerDependencies) Shutdown() {
	if d.Runtime != nil {
		d.Runtime.Shutdown()
	}
}

// GetRegistry returns the current scraper registry (thread-safe).
func (d *ServerDependencies) GetRegistry() *models.ScraperRegistry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Registry
}

// GetAggregator returns the current aggregator (thread-safe).
func (d *ServerDependencies) GetAggregator() *aggregator.Aggregator {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Aggregator
}

// GetMatcher returns the current matcher (thread-safe).
func (d *ServerDependencies) GetMatcher() *matcher.Matcher {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Matcher
}

// ReplaceReloadable swaps config-coupled runtime components atomically.
func (d *ServerDependencies) ReplaceReloadable(
	cfg *config.Config,
	registry *models.ScraperRegistry,
	aggregator *aggregator.Aggregator,
	mat *matcher.Matcher,
) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Registry = registry
	d.Aggregator = aggregator
	d.Matcher = mat
	d.SetConfig(cfg)
}
