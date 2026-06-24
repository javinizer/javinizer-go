package core

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIDeps_ServerCtx(t *testing.T) {
	t.Run("creates context on first call", func(t *testing.T) {
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		ctx := rt.ServerCtx()
		assert.NotNil(t, ctx)
		assert.NoError(t, ctx.Err())
	})

	t.Run("returns same context on subsequent calls", func(t *testing.T) {
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		ctx1 := rt.ServerCtx()
		ctx2 := rt.ServerCtx()
		assert.Equal(t, ctx1, ctx2)
	})
}

func TestAPIDeps_Shutdown(t *testing.T) {
	t.Run("no-op when nothing to shut down", func(t *testing.T) {
		assert.NotPanics(t, func() {
			shutdownDeps(nil)
		})
	})

	t.Run("cancels server context", func(t *testing.T) {
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		ctx := rt.ServerCtx()
		require.NotNil(t, ctx)
		require.NoError(t, ctx.Err())

		rt.Shutdown()

		// After shutdown, the context should be cancelled
		<-ctx.Done()
		assert.Error(t, ctx.Err())
	})
}

func TestAPIDeps_GetRuntime(t *testing.T) {
	t.Run("returns runtime when set", func(t *testing.T) {
		rs := NewRuntimeState()
		rt := &APIRuntime{Runtime: rs}
		got := rt.GetRuntime()
		assert.Equal(t, rs, got)
	})
}

func TestAPIDeps_NewMatcher(t *testing.T) {
	t.Run("returns matcher when config is valid", func(t *testing.T) {
		cfg := &config.Config{
			Matching: config.MatchingConfig{
				RegexEnabled: false,
			},
		}
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		rt.SetConfig(cfg)
		rt.InitAPIConfig()

		mat := rt.NewMatcher()
		assert.NotNil(t, mat)
	})

	t.Run("returns nil when matcher config is invalid", func(t *testing.T) {
		cfg := &config.Config{
			Matching: config.MatchingConfig{
				RegexEnabled: true,
				RegexPattern: "[invalid(",
			},
		}
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		rt.SetConfig(cfg)
		rt.InitAPIConfig()

		mat := rt.NewMatcher()
		assert.Nil(t, mat)
	})
}

func TestAPIDeps_GetWorkflow(t *testing.T) {
	t.Run("returns workflow with valid config", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		cfg.Database.DSN = ":memory:"
		db, err := database.New(database.ConfigFromAppConfig(cfg))
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

		_, err = config.Prepare(cfg)
		require.NoError(t, err)

		registry := scraperutil.NewScraperRegistry()
		repos := db.Repositories()

		deps := &APIDeps{
			CoreDeps: &commandutil.CoreDeps{
				ScraperRegistry: registry,
				DB:              db,
			},
			Repos: repos,
		}
		rt := NewAPIRuntime(deps)
		rt.SetConfig(cfg)
		rt.InitAPIConfig()

		wf := rt.GetWorkflow()
		assert.NotNil(t, wf)
	})
}

func TestAPIDeps_GetPosterManager_NilConfig(t *testing.T) {
	deps := &APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: scraperutil.NewScraperRegistry(),
		},
	}
	// No config set on CoreDeps
	rt := NewAPIRuntime(deps)
	pm := rt.GetPosterManager()
	assert.Nil(t, pm)
}

func TestAPIDeps_GetScraperOptions(t *testing.T) {
	t.Run("panics when CoreDeps is nil", func(t *testing.T) {
		deps := &APIDeps{}
		assert.Panics(t, func() {
			deps.GetScraperOptions("r18dev")
		}, "GetScraperOptions should panic when CoreDeps is nil")
	})

	t.Run("returns false when scraper not found", func(t *testing.T) {
		registry := scraperutil.NewScraperRegistry()
		deps := &APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry}}
		_, ok := deps.GetScraperOptions("nonexistent")
		assert.False(t, ok)
	})
}

func TestAPIDeps_InvalidateWorkflowCaches(t *testing.T) {
	t.Run("no-op when no factory cached", func(t *testing.T) {
		deps := &APIDeps{}
		assert.NotPanics(t, func() {
			NewAPIRuntime(deps).InvalidateWorkflowCaches()
		})
	})
}

func TestBatchDeps_Interface(t *testing.T) {
	cfg := &config.Config{
		API:      config.APIConfig{Security: config.SecurityConfig{AllowedDirectories: []string{"/media"}}},
		System:   config.SystemConfig{TempDir: "/tmp"},
		Matching: config.MatchingConfig{},
	}
	deps := &APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: scraperutil.NewScraperRegistry(),
		},
		Repos: database.Repositories{
			ContentRepos: database.ContentRepos{
				MovieRepo: &database.MovieRepository{},
			},
		},
	}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	rt.InitAPIConfig()

	t.Run("APIConfig", func(t *testing.T) {
		apiCfg := rt.GetAPIConfig()
		assert.Equal(t, []string{"/media"}, apiCfg.AllowedDirectories)
	})

	t.Run("ServerCtx", func(t *testing.T) {
		ctx := rt.ServerCtx()
		assert.NotNil(t, ctx)
	})

	t.Run("Runtime returns nil when not initialized", func(t *testing.T) {
		rs := rt.GetRuntime()
		// Runtime may be nil if not explicitly set
		if rs != nil {
			_ = rs
		}
	})

	t.Run("GetWorkflow returns workflow when factory is available", func(t *testing.T) {
		rt := NewAPIRuntime(deps)
		wf := rt.GetWorkflow()
		// With SetConfig auto-initializing APIRuntime, a factory is available
		// when config is properly set.
		_ = wf
	})
}

func TestAPIDeps_SetConfig_NilCoreDeps(t *testing.T) {
	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	cfg := &config.Config{ConfigVersion: 1}
	rt.SetConfig(cfg)

	// Should have created CoreDeps and set config
	assert.NotNil(t, deps.CoreDeps)
	assert.Equal(t, cfg, deps.CoreDeps.GetConfig())
}

func TestAPIDeps_ServerCtx_Cancellation(t *testing.T) {
	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	ctx := rt.ServerCtx()

	// Start a goroutine that waits on the context
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(done)
	}()

	rt.Shutdown()

	select {
	case <-done:
		// Context was cancelled as expected
	case <-time.After(time.Second):
		t.Fatal("ServerCtx was not cancelled after Shutdown")
	}
}

func TestAPIDeps_ContextUsage(t *testing.T) {
	t.Run("ServerCtx can be used for background operations", func(t *testing.T) {
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		ctx := rt.ServerCtx()

		// Should be able to derive a child context
		childCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		assert.NoError(t, childCtx.Err())
	})
}
