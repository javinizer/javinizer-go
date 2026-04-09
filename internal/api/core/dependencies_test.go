package core

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerDependencies_EnsureRuntime(t *testing.T) {
	t.Run("creates runtime when nil", func(t *testing.T) {
		deps := &ServerDependencies{}
		rt := deps.EnsureRuntime()
		assert.NotNil(t, rt, "EnsureRuntime should create runtime when nil")
		assert.NotNil(t, deps.Runtime, "Runtime field should be set")
	})

	t.Run("returns existing runtime when set", func(t *testing.T) {
		existingRuntime := NewRuntimeState()
		deps := &ServerDependencies{Runtime: existingRuntime}
		rt := deps.EnsureRuntime()
		assert.Equal(t, existingRuntime, rt, "EnsureRuntime should return existing runtime")
	})
}

func TestServerDependencies_GetConfig(t *testing.T) {
	t.Run("returns config when set", func(t *testing.T) {
		cfg := &config.Config{ConfigVersion: 1}
		deps := &ServerDependencies{}
		deps.SetConfig(cfg)

		got := deps.GetConfig()
		assert.Equal(t, cfg, got, "GetConfig should return the set config")
	})

	t.Run("panics when config is nil", func(t *testing.T) {
		deps := &ServerDependencies{}

		defer func() {
			r := recover()
			assert.NotNil(t, r, "GetConfig should panic when config is nil")
			assert.Contains(t, r, "GetConfig() called with nil config", "Panic message should mention nil config")
		}()

		deps.GetConfig()
		t.Error("GetConfig should have panicked")
	})
}

func TestServerDependencies_SetConfig(t *testing.T) {
	t.Run("sets config successfully", func(t *testing.T) {
		cfg := &config.Config{ConfigVersion: 1}
		deps := &ServerDependencies{}

		deps.SetConfig(cfg)
		got := deps.GetConfig()
		assert.Equal(t, cfg, got, "SetConfig should store the config")
	})

	t.Run("panics when setting nil config", func(t *testing.T) {
		deps := &ServerDependencies{}

		defer func() {
			r := recover()
			assert.NotNil(t, r, "SetConfig should panic when config is nil")
			assert.Contains(t, r, "SetConfig() called with nil config", "Panic message should mention nil config")
		}()

		deps.SetConfig(nil)
		t.Error("SetConfig should have panicked")
	})
}

func TestServerDependencies_Shutdown(t *testing.T) {
	t.Run("no-op when runtime is nil", func(t *testing.T) {
		deps := &ServerDependencies{}
		assert.NotPanics(t, func() {
			deps.Shutdown()
		}, "Shutdown should not panic when runtime is nil")
	})

	t.Run("calls runtime shutdown", func(t *testing.T) {
		rt := NewRuntimeState()
		_ = rt.ResetWebSocketHub()
		require.NotNil(t, rt.wsHubShutdown, "Shutdown channel should be set")

		deps := &ServerDependencies{Runtime: rt}

		shutdownDone := make(chan struct{})
		go func() {
			deps.Shutdown()
			close(shutdownDone)
		}()

		select {
		case <-shutdownDone:
		case <-time.After(2 * time.Second):
			t.Fatal("Shutdown did not complete within 2 seconds")
		}

		assert.Nil(t, rt.wsHubCancel, "Runtime shutdown should clear cancel function")
	})
}

func TestServerDependencies_GetRegistry(t *testing.T) {
	registry := &models.ScraperRegistry{}
	deps := &ServerDependencies{Registry: registry}

	got := deps.GetRegistry()
	assert.Equal(t, registry, got, "GetRegistry should return the set registry")
}

func TestServerDependencies_GetAggregator(t *testing.T) {
	agg := &aggregator.Aggregator{}
	deps := &ServerDependencies{Aggregator: agg}

	got := deps.GetAggregator()
	assert.Equal(t, agg, got, "GetAggregator should return the set aggregator")
}

func TestServerDependencies_GetMatcher(t *testing.T) {
	mat := &matcher.Matcher{}
	deps := &ServerDependencies{Matcher: mat}

	got := deps.GetMatcher()
	assert.Equal(t, mat, got, "GetMatcher should return the set matcher")
}

func TestServerDependencies_ReplaceReloadable(t *testing.T) {
	t.Run("replaces all reloadable components", func(t *testing.T) {
		deps := &ServerDependencies{}

		cfg := &config.Config{ConfigVersion: 2}
		registry := &models.ScraperRegistry{}
		agg := &aggregator.Aggregator{}
		mat := &matcher.Matcher{}

		deps.ReplaceReloadable(cfg, registry, agg, mat)

		assert.Equal(t, cfg, deps.GetConfig(), "Config should be replaced")
		assert.Equal(t, registry, deps.GetRegistry(), "Registry should be replaced")
		assert.Equal(t, agg, deps.GetAggregator(), "Aggregator should be replaced")
		assert.Equal(t, mat, deps.GetMatcher(), "Matcher should be replaced")
	})
}

func TestServerDependencies_NilFields(t *testing.T) {
	deps := &ServerDependencies{}

	assert.Nil(t, deps.GetRegistry(), "GetRegistry should return nil when not set")
	assert.Nil(t, deps.GetAggregator(), "GetAggregator should return nil when not set")
	assert.Nil(t, deps.GetMatcher(), "GetMatcher should return nil when not set")
}

func TestServerDependencies_AllFields(t *testing.T) {
	cfg := &config.Config{ConfigVersion: 1}
	registry := &models.ScraperRegistry{}
	db := &database.DB{}
	agg := &aggregator.Aggregator{}
	movieRepo := &database.MovieRepository{}
	actressRepo := &database.ActressRepository{}
	historyRepo := &database.HistoryRepository{}
	jobRepo := &database.JobRepository{}
	mat := &matcher.Matcher{}
	jobQueue := &worker.JobQueue{}
	rt := NewRuntimeState()
	tokenStore := &TokenStore{}

	deps := &ServerDependencies{
		ConfigFile:  "/path/to/config.yaml",
		Registry:    registry,
		DB:          db,
		Aggregator:  agg,
		MovieRepo:   movieRepo,
		ActressRepo: actressRepo,
		HistoryRepo: historyRepo,
		JobRepo:     jobRepo,
		Matcher:     mat,
		JobQueue:    jobQueue,
		Runtime:     rt,
		TokenStore:  tokenStore,
	}

	deps.SetConfig(cfg)

	assert.Equal(t, "/path/to/config.yaml", deps.ConfigFile)
	assert.Equal(t, cfg, deps.GetConfig())
	assert.Equal(t, registry, deps.GetRegistry())
	assert.Equal(t, db, deps.DB)
	assert.Equal(t, agg, deps.GetAggregator())
	assert.Equal(t, movieRepo, deps.MovieRepo)
	assert.Equal(t, actressRepo, deps.ActressRepo)
	assert.Equal(t, historyRepo, deps.HistoryRepo)
	assert.Equal(t, jobRepo, deps.JobRepo)
	assert.Equal(t, mat, deps.GetMatcher())
	assert.Equal(t, jobQueue, deps.JobQueue)
	assert.Equal(t, rt, deps.Runtime)
	assert.Equal(t, tokenStore, deps.TokenStore)
}

func TestServerDependencies_ConcurrentAccess(t *testing.T) {
	deps := &ServerDependencies{}
	cfg1 := &config.Config{ConfigVersion: 1}
	cfg2 := &config.Config{ConfigVersion: 2}

	deps.SetConfig(cfg1)

	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			_ = deps.GetConfig()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			deps.SetConfig(cfg2)
		}
		done <- true
	}()

	for i := 0; i < 2; i++ {
		<-done
	}

	finalCfg := deps.GetConfig()
	assert.Contains(t, []int{1, 2}, finalCfg.ConfigVersion, "Final config should be one of the set values")
}

type mockAuthProvider struct {
	initialized bool
	username    string
}

func (m *mockAuthProvider) SessionTTL() time.Duration {
	return 24 * time.Hour
}

func (m *mockAuthProvider) IsInitialized() bool {
	return m.initialized
}

func (m *mockAuthProvider) AuthenticateSession(sessionID string) (string, error) {
	return m.username, nil
}

func (m *mockAuthProvider) Setup(username, password string) error {
	m.username = username
	m.initialized = true
	return nil
}

func (m *mockAuthProvider) Login(username, password string, rememberMe bool) (string, error) {
	return "session-id", nil
}

func (m *mockAuthProvider) Logout(sessionID string) {}

func TestServerDependencies_AuthProvider(t *testing.T) {
	auth := &mockAuthProvider{initialized: false}
	deps := &ServerDependencies{Auth: auth}

	assert.Equal(t, 24*time.Hour, deps.Auth.SessionTTL())
	assert.False(t, deps.Auth.IsInitialized())

	err := deps.Auth.Setup("testuser", "testpass")
	require.NoError(t, err)
	assert.True(t, deps.Auth.IsInitialized())
}
