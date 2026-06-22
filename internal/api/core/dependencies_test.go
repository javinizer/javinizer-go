package core

import (
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCoreTestJPEG(t *testing.T, fs afero.Fs, path string, width, height int) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0755))
	file, err := fs.Create(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	require.NoError(t, jpeg.Encode(file, img, &jpeg.Options{Quality: 90}))
}

func TestAPIDeps_GetPosterManagerUsesInjectedFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := "/memfs-api-temp"
	deps := &APIDeps{Fs: fs}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(&config.Config{System: config.SystemConfig{TempDir: tempDir}})

	fullPosterPath := filepath.Join(tempDir, "posters", "job1", "ABC-123-full.jpg")
	writeCoreTestJPEG(t, fs, fullPosterPath, 200, 300)

	manager := rt.GetPosterManager()
	require.NotNil(t, manager)

	result, err := manager.CropWithBounds(context.Background(), "job1", "ABC-123", 0, 0, 100, 150, 500)
	require.NoError(t, err)
	_, err = fs.Stat(result.CroppedPath)
	require.NoError(t, err)
}

func TestAPIDeps_GetPosterManagerDefaultsToOsFs(t *testing.T) {
	tempDir := t.TempDir()
	osFs := afero.NewOsFs()
	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(&config.Config{System: config.SystemConfig{TempDir: tempDir}})

	fullPosterPath := filepath.Join(tempDir, "posters", "job1", "ABC-123-full.jpg")
	writeCoreTestJPEG(t, osFs, fullPosterPath, 200, 300)

	manager := rt.GetPosterManager()
	require.NotNil(t, manager)

	result, err := manager.CropWithBounds(context.Background(), "job1", "ABC-123", 0, 0, 100, 150, 500)
	require.NoError(t, err)
	_, err = osFs.Stat(result.CroppedPath)
	require.NoError(t, err)
}

func TestAPIDeps_BatchHandlerDependencies(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobStore := &worker.JobStore{}
	movieRepo := &database.MovieRepository{}
	jobRepo := &database.JobRepository{}
	batchFileOpRepo := &database.BatchFileOperationRepository{}
	rs := NewRuntimeState()
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{AllowedDirectories: []string{"/media"}},
		},
		System: config.SystemConfig{TempDir: "/memfs-api-temp"},
	}

	deps := &APIDeps{
		Repos: database.Repositories{
			ContentRepos: database.ContentRepos{
				MovieRepo: movieRepo,
			},
			HistoryRepos: database.HistoryRepos{
				JobRepo:         jobRepo,
				BatchFileOpRepo: batchFileOpRepo,
			},
		},
		JobStore: jobStore,
		Fs:       fs,
	}
	runtime := NewAPIRuntime(deps)
	runtime.Runtime = rs
	runtime.SetConfig(cfg)
	runtime.InitAPIConfig()

	// Per DEEP-3: BatchDeps interface removed; test directly through *APIDeps.
	assert.Equal(t, []string{"/media"}, runtime.GetAPIConfig().AllowedDirectories)
	assert.Equal(t, fs, deps.GetFs())
	assert.Equal(t, jobStore, deps.GetJobStore())
	assert.Equal(t, rs, runtime.GetRuntime())
	assert.NotNil(t, runtime.NewMatcher())
	assert.NotNil(t, runtime.GetPosterManager())
}

func TestAPIDeps_FileHandlerDependencies(t *testing.T) {
	movieRepo := &database.MovieRepository{}
	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{AllowedDirectories: []string{"/nfo"}},
		},
	}
	deps := &APIDeps{
		Repos: database.Repositories{
			ContentRepos: database.ContentRepos{
				MovieRepo: movieRepo,
			},
		},
	}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	rt.InitAPIConfig()

	// Per DEEP-3: FileDeps interface removed; test directly through *APIDeps.
	assert.Equal(t, []string{"/nfo"}, rt.GetAPIConfig().AllowedDirectories)
}

func TestAPIDeps_GetConfig(t *testing.T) {
	t.Run("returns config when set", func(t *testing.T) {
		cfg := &config.Config{ConfigVersion: 1}
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		rt.SetConfig(cfg)

		got := deps.CoreDeps.GetConfig()
		assert.Equal(t, cfg, got, "GetConfig should return the set config")
	})

	t.Run("panics when CoreDeps is nil", func(t *testing.T) {
		deps := &APIDeps{}
		assert.Panics(t, func() {
			deps.CoreDeps.GetConfig()
		}, "GetConfig should panic when CoreDeps is nil")
	})
}

func TestAPIDeps_SetConfig(t *testing.T) {
	t.Run("sets config successfully", func(t *testing.T) {
		cfg := &config.Config{ConfigVersion: 1}
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		rt.SetConfig(cfg)
		got := deps.CoreDeps.GetConfig()
		assert.Equal(t, cfg, got, "SetConfig should store the config")
	})

	t.Run("panics when setting nil config", func(t *testing.T) {
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		assert.Panics(t, func() {
			rt.SetConfig(nil)
		}, "SetConfig should panic when called with nil config")
	})
}

func TestShutdownDeps(t *testing.T) {
	t.Run("no-op when runtime is nil", func(t *testing.T) {
		assert.NotPanics(t, func() {
			shutdownDeps(nil)
		}, "shutdownDeps should not panic when runtime is nil")
	})

	t.Run("calls runtime shutdown", func(t *testing.T) {
		rs := NewRuntimeState()
		_ = rs.ResetWebSocketHub()
		require.NotNil(t, rs.wsHubShutdown, "Shutdown channel should be set")

		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		rt.Runtime = rs

		shutdownDone := make(chan struct{})
		go func() {
			shutdownDeps(rt)
			close(shutdownDone)
		}()

		select {
		case <-shutdownDone:
		case <-time.After(2 * time.Second):
			t.Fatal("shutdownDeps did not complete within 2 seconds")
		}

		assert.Nil(t, rs.wsHubCancel, "Runtime shutdown should clear cancel function")
	})
}

func TestAPIDeps_GetRegistry(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	deps := &APIDeps{
		CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry},
	}

	got := deps.CoreDeps.GetRegistry()
	assert.Equal(t, registry, got, "GetRegistry should return the set registry")
}

func TestAPIDeps_ReplaceReloadable(t *testing.T) {
	t.Run("replaces all reloadable components", func(t *testing.T) {
		deps := &APIDeps{}

		cfg := &config.Config{ConfigVersion: 2}
		registry := scraperutil.NewScraperRegistry()

		rt := NewAPIRuntime(deps)
		rt.ReplaceReloadable(cfg, registry)

		assert.Equal(t, cfg, deps.CoreDeps.GetConfig(), "Config should be replaced")
		assert.Equal(t, registry, deps.CoreDeps.GetRegistry(), "Registry should be replaced")
	})
}

func TestAPIDeps_NilCoreDeps_Panics(t *testing.T) {
	deps := &APIDeps{}

	assert.Panics(t, func() {
		deps.CoreDeps.GetRegistry()
	}, "GetRegistry should panic when CoreDeps is nil")
}

func TestAPIDeps_ReloadConfig(t *testing.T) {
	t.Run("rebuilds registry and swaps config", func(t *testing.T) {
		cfg1 := &config.Config{ConfigVersion: 1}
		cfg2 := &config.Config{ConfigVersion: 2}
		registry := scraperutil.NewScraperRegistry()

		deps := &APIDeps{
			CoreDeps: &commandutil.CoreDeps{
				ScraperRegistry: registry,
			},
		}
		rt := NewAPIRuntime(deps)
		rt.SetConfig(cfg1)

		err := rt.ReloadConfig(cfg2)
		require.NoError(t, err, "ReloadConfig should succeed")
		assert.Equal(t, cfg2, deps.CoreDeps.GetConfig(), "Config should be updated")
	})
}

func TestAPIDeps_AllFields(t *testing.T) {
	cfg := &config.Config{ConfigVersion: 1}
	registry := scraperutil.NewScraperRegistry()
	db := &database.DB{}
	movieRepo := &database.MovieRepository{}
	actressRepo := &database.ActressRepository{}
	historyRepo := &database.HistoryRepository{}
	jobRepo := &database.JobRepository{}
	jobStore := &worker.JobStore{}
	rs := NewRuntimeState()
	tokenStore := NewTokenStore()

	deps := &APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: registry,
			DB:              db,
		},
		ConfigFile: "/path/to/config.yaml",
		Repos: database.Repositories{
			ContentRepos: database.ContentRepos{
				MovieRepo:   movieRepo,
				ActressRepo: actressRepo,
			},
			HistoryRepos: database.HistoryRepos{
				HistoryRepo: historyRepo,
				JobRepo:     jobRepo,
			},
		},
		JobStore:   jobStore,
		TokenStore: tokenStore,
	}

	runtime := NewAPIRuntime(deps)
	runtime.Runtime = rs
	runtime.SetConfig(cfg)

	assert.Equal(t, "/path/to/config.yaml", deps.ConfigFile)
	assert.Equal(t, cfg, deps.CoreDeps.GetConfig())
	assert.Equal(t, registry, deps.CoreDeps.GetRegistry())
	assert.Equal(t, db, deps.CoreDeps.DB)
	assert.Equal(t, movieRepo, deps.Repos.MovieRepo)
	assert.Equal(t, actressRepo, deps.Repos.ActressRepo)
	assert.Equal(t, historyRepo, deps.Repos.HistoryRepo)
	assert.Equal(t, jobRepo, deps.Repos.JobRepo)
	assert.Equal(t, jobStore, deps.JobStore)
	assert.Equal(t, rs, runtime.Runtime)
	assert.Equal(t, tokenStore, deps.TokenStore)
}

func TestAPIDeps_ConcurrentAccess(t *testing.T) {
	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	cfg1 := &config.Config{ConfigVersion: 1}
	cfg2 := &config.Config{ConfigVersion: 2}

	rt.SetConfig(cfg1)

	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			_ = deps.CoreDeps.GetConfig()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			rt.SetConfig(cfg2)
		}
		done <- true
	}()

	for i := 0; i < 2; i++ {
		<-done
	}

	finalCfg := deps.CoreDeps.GetConfig()
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

func (m *mockAuthProvider) ValidateToken(_ context.Context, _ string) (string, error) {
	return "token-id", nil
}

func (m *mockAuthProvider) UpdateTokenLastUsed(_ context.Context, _ string) error {
	return nil
}

func (m *mockAuthProvider) GetEnv(key string) string {
	return os.Getenv(key)
}

func TestAPIDeps_AuthProvider(t *testing.T) {
	auth := &mockAuthProvider{initialized: false}
	deps := &APIDeps{Auth: auth}

	assert.Equal(t, 24*time.Hour, deps.Auth.SessionTTL())
	assert.False(t, deps.Auth.IsInitialized())

	err := deps.Auth.Setup("testuser", "testpass")
	require.NoError(t, err)
	assert.True(t, deps.Auth.IsInitialized())
}

func TestRepositories(t *testing.T) {
	t.Run("all repo fields are accessible via Repos bundle", func(t *testing.T) {
		movieRepo := &database.MovieRepository{}
		actressRepo := &database.ActressRepository{}
		historyRepo := &database.HistoryRepository{}
		jobRepo := &database.JobRepository{}
		batchFileOpRepo := &database.BatchFileOperationRepository{}
		eventRepo := &database.EventRepository{}
		apiTokenRepo := &database.ApiTokenRepository{}
		genreReplacementRepo := &database.GenreReplacementRepository{}
		wordReplacementRepo := &database.WordReplacementRepository{}
		genreTranslationRepo := &database.GenreTranslationRepository{}
		actressTranslationRepo := &database.ActressTranslationRepository{}
		genreRepo := &database.GenreRepository{}

		repos := database.Repositories{
			ContentRepos: database.ContentRepos{
				MovieRepo:   movieRepo,
				ActressRepo: actressRepo,
			},
			HistoryRepos: database.HistoryRepos{
				HistoryRepo:     historyRepo,
				JobRepo:         jobRepo,
				BatchFileOpRepo: batchFileOpRepo,
			},
			SystemRepos: database.SystemRepos{
				EventRepo:    eventRepo,
				ApiTokenRepo: apiTokenRepo,
			},
			TranslationRepos: database.TranslationRepos{
				GenreTranslationRepo:   genreTranslationRepo,
				ActressTranslationRepo: actressTranslationRepo,
			},
			ReplacementRepos: database.ReplacementRepos{
				GenreRepo:            genreRepo,
				GenreReplacementRepo: genreReplacementRepo,
				WordReplacementRepo:  wordReplacementRepo,
			},
		}

		assert.Equal(t, movieRepo, repos.MovieRepo)
		assert.Equal(t, actressRepo, repos.ActressRepo)
		assert.Equal(t, historyRepo, repos.HistoryRepo)
		assert.Equal(t, jobRepo, repos.JobRepo)
		assert.Equal(t, batchFileOpRepo, repos.BatchFileOpRepo)
		assert.Equal(t, eventRepo, repos.EventRepo)
		assert.Equal(t, apiTokenRepo, repos.ApiTokenRepo)
		assert.Equal(t, genreReplacementRepo, repos.GenreReplacementRepo)
		assert.Equal(t, wordReplacementRepo, repos.WordReplacementRepo)
		assert.Equal(t, genreTranslationRepo, repos.GenreTranslationRepo)
		assert.Equal(t, actressTranslationRepo, repos.ActressTranslationRepo)
		assert.Equal(t, genreRepo, repos.GenreRepo)
	})

	t.Run("Repos bundle works on APIDeps", func(t *testing.T) {
		movieRepo := &database.MovieRepository{}
		deps := &APIDeps{
			Repos: database.Repositories{
				ContentRepos: database.ContentRepos{
					MovieRepo: movieRepo,
				},
			},
		}
		assert.Equal(t, movieRepo, deps.Repos.MovieRepo)
	})
}

func TestAPIDeps_InitAPIConfig(t *testing.T) {
	t.Run("populates apiCfg from config", func(t *testing.T) {
		cfg := &config.Config{
			Server:  config.ServerConfig{Host: "0.0.0.0", Port: 9090},
			API:     config.APIConfig{Security: config.SecurityConfig{MaxFilesPerScan: 42}},
			System:  config.SystemConfig{TempDir: "/tmp/jav"},
			Logging: config.LoggingConfig{Level: "debug"},
		}
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		rt.SetConfig(cfg)
		rt.InitAPIConfig()

		apiCfg := rt.GetAPIConfig()
		assert.Equal(t, "0.0.0.0", apiCfg.Host)
		assert.Equal(t, 9090, apiCfg.Port)
		assert.Equal(t, 42, apiCfg.MaxFilesPerScan)
		assert.Equal(t, "/tmp/jav", apiCfg.TempDir)
		assert.Equal(t, "debug", apiCfg.LogLevel)
	})

	t.Run("no-op when config is nil", func(t *testing.T) {
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		// Should not panic
		rt.InitAPIConfig()
		apiCfg := rt.GetAPIConfig()
		assert.Equal(t, APIConfig{}, apiCfg, "APIConfig should remain zero-value when config is nil")
	})
}

func TestAPIDeps_GetAPIConfig(t *testing.T) {
	t.Run("returns zero value when not initialized", func(t *testing.T) {
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		apiCfg := rt.GetAPIConfig()
		assert.Equal(t, APIConfig{}, apiCfg)
	})

	t.Run("returns populated config after InitAPIConfig", func(t *testing.T) {
		cfg := &config.Config{
			Server:      config.ServerConfig{Host: "localhost", Port: 8080},
			API:         config.APIConfig{Security: config.SecurityConfig{AllowUNC: true}},
			Performance: config.PerformanceConfig{MaxWorkers: 10, WorkerTimeout: 120},
			Output: config.OutputConfig{
				Operation: config.OutputOperationConfig{
					AllowRevert: true,
				},
			},
		}
		deps := &APIDeps{}
		rt := NewAPIRuntime(deps)
		rt.SetConfig(cfg)
		rt.InitAPIConfig()

		apiCfg := rt.GetAPIConfig()
		assert.Equal(t, "localhost", apiCfg.Host)
		assert.Equal(t, 8080, apiCfg.Port)
		assert.True(t, apiCfg.AllowUNC)
		assert.Equal(t, 10, apiCfg.MaxWorkers)
		assert.Equal(t, 120*time.Second, apiCfg.WorkerTimeout)
		assert.True(t, apiCfg.AllowRevert)
	})
}

func TestAPIDeps_ReloadConfig_RebuildsAPIConfig(t *testing.T) {
	cfg1 := &config.Config{
		ConfigVersion: 1,
		Server:        config.ServerConfig{Host: "0.0.0.0", Port: 8080},
		Logging:       config.LoggingConfig{Level: "info"},
	}
	cfg2 := &config.Config{
		ConfigVersion: 2,
		Server:        config.ServerConfig{Host: "127.0.0.1", Port: 9090},
		Logging:       config.LoggingConfig{Level: "debug"},
	}

	registry := scraperutil.NewScraperRegistry()
	deps := &APIDeps{
		CoreDeps: &commandutil.CoreDeps{ScraperRegistry: registry},
		Repos: database.Repositories{
			ContentRepos: database.ContentRepos{
				ContentIDMappingRepo: &database.ContentIDMappingRepository{},
			},
		},
	}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg1)
	rt.InitAPIConfig()

	// Verify initial state
	apiCfg := rt.GetAPIConfig()
	assert.Equal(t, "0.0.0.0", apiCfg.Host)
	assert.Equal(t, 8080, apiCfg.Port)
	assert.Equal(t, "info", apiCfg.LogLevel)

	// Reload with new config
	err := rt.ReloadConfig(cfg2)
	require.NoError(t, err)

	// Verify APIConfig was rebuilt
	apiCfg = rt.GetAPIConfig()
	assert.Equal(t, "127.0.0.1", apiCfg.Host)
	assert.Equal(t, 9090, apiCfg.Port)
	assert.Equal(t, "debug", apiCfg.LogLevel)
}

func TestAPIDeps_ReplaceReloadable_RebuildsAPIConfig(t *testing.T) {
	cfg1 := &config.Config{
		Server:  config.ServerConfig{Host: "0.0.0.0", Port: 8080},
		Logging: config.LoggingConfig{Level: "info"},
	}
	cfg2 := &config.Config{
		Server:  config.ServerConfig{Host: "192.168.1.1", Port: 7070},
		Logging: config.LoggingConfig{Level: "warn"},
	}

	registry := scraperutil.NewScraperRegistry()
	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg1)
	rt.InitAPIConfig()

	// Verify initial state
	apiCfg := rt.GetAPIConfig()
	assert.Equal(t, "0.0.0.0", apiCfg.Host)

	// Replace with new config
	rt.ReplaceReloadable(cfg2, registry)

	// Verify APIConfig was rebuilt
	apiCfg = rt.GetAPIConfig()
	assert.Equal(t, "192.168.1.1", apiCfg.Host)
	assert.Equal(t, 7070, apiCfg.Port)
	assert.Equal(t, "warn", apiCfg.LogLevel)
}

func TestAPIDeps_GetAPIConfig_ConcurrentAccess(t *testing.T) {
	cfg1 := &config.Config{
		Server:  config.ServerConfig{Host: "host1", Port: 1111},
		Logging: config.LoggingConfig{Level: "info"},
	}
	cfg2 := &config.Config{
		Server:  config.ServerConfig{Host: "host2", Port: 2222},
		Logging: config.LoggingConfig{Level: "debug"},
	}

	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg1)
	rt.InitAPIConfig()

	var wg sync.WaitGroup
	wg.Add(2)

	// Reader goroutine: continuously read APIConfig
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			apiCfg := rt.GetAPIConfig()
			// Each snapshot should be internally consistent (host and port from same config)
			if apiCfg.Host == "host1" {
				assert.Equal(t, 1111, apiCfg.Port, "host1 should have port 1111")
			}
			if apiCfg.Host == "host2" {
				assert.Equal(t, 2222, apiCfg.Port, "host2 should have port 2222")
			}
		}
	}()

	// Writer goroutine: swap between two configs
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			rt.ReplaceReloadable(cfg1, scraperutil.NewScraperRegistry())
			rt.ReplaceReloadable(cfg2, scraperutil.NewScraperRegistry())
		}
	}()

	wg.Wait()
}

func TestConfigFromAppConfig(t *testing.T) {
	t.Run("returns zero value for nil config", func(t *testing.T) {
		result := ConfigFromAppConfig(nil)
		assert.Equal(t, APIConfig{}, result)
	})

	t.Run("extracts all fields correctly", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{Host: "0.0.0.0", Port: 8080},
			API: config.APIConfig{
				Security: config.SecurityConfig{
					AllowedDirectories: []string{"/media"},
					DeniedDirectories:  []string{"/etc"},
					MaxFilesPerScan:    100,
					ScanTimeoutSeconds: 30,
					AllowedOrigins:     []string{"http://localhost:3000"},
					AllowUNC:           true,
					AllowedUNCServers:  []string{"//nas"},
					RateLimit:          config.RateLimitConfig{RequestsPerMinute: 60},
					TrustedProxies:     []string{"10.0.0.1"},
					ForceSecureCookies: true,
				},
			},
			Scrapers: config.ScrapersConfig{
				Priority:     []string{"r18dev", "dmm"},
				UserAgent:    "TestAgent",
				Referer:      "https://example.com/",
				Proxy:        models.ProxyConfig{Enabled: true, DefaultProfile: "myproxy"},
				FlareSolverr: models.FlareSolverrConfig{Enabled: true, URL: "http://fs:8191"},
			},
			Metadata: config.MetadataConfig{
				NFO: config.NFOConfig{
					Feature: config.NFOFeatureConfig{
						Enabled: true,
						PerFile: true,
					},
					Format: config.NFOFormatConfig{
						FilenameTemplate: "{id}.nfo",
						DisplayTitle:     "original",
					},
				},
				Translation: config.TranslationConfig{
					Enabled:        true,
					Provider:       "openai",
					TargetLanguage: "en",
				},
			},
			Output: config.OutputConfig{
				Operation: config.OutputOperationConfig{
					AllowRevert: true,
				},
			},
			Performance: config.PerformanceConfig{
				MaxWorkers:    8,
				WorkerTimeout: 120,
			},
			Matching: config.MatchingConfig{
				RegexEnabled: true,
				RegexPattern: "([A-Z]+-\\d+)",
			},
			System: config.SystemConfig{
				TempDir:             "/tmp/jav",
				VersionCheckEnabled: true,
			},
			Logging: config.LoggingConfig{Level: "debug"},
			Database: config.DatabaseConfig{
				DSN:      "data/javinizer.db",
				LogLevel: "warn",
			},
		}

		result := ConfigFromAppConfig(cfg)

		// Security
		assert.Equal(t, []string{"/media"}, result.AllowedDirectories)
		assert.Equal(t, []string{"/etc"}, result.DeniedDirectories)
		assert.Equal(t, 100, result.MaxFilesPerScan)
		assert.Equal(t, 30, result.ScanTimeoutSeconds)
		assert.Equal(t, []string{"http://localhost:3000"}, result.AllowedOrigins)
		assert.True(t, result.AllowUNC)
		assert.Equal(t, []string{"//nas"}, result.AllowedUNCServers)
		assert.Equal(t, 60, result.RateLimitRPM)
		assert.Equal(t, []string{"10.0.0.1"}, result.TrustedProxies)
		assert.True(t, result.ForceSecureCookies)

		// Server
		assert.Equal(t, "0.0.0.0", result.Host)
		assert.Equal(t, 8080, result.Port)

		// Scrapers
		assert.Equal(t, []string{"r18dev", "dmm"}, result.ScraperPriority)
		assert.Equal(t, "TestAgent", result.ScraperUserAgent)
		assert.Equal(t, "https://example.com/", result.ScraperReferer)
		assert.True(t, result.ProxyConfig.Enabled)
		assert.Equal(t, "myproxy", result.ProxyConfig.DefaultProfile)
		assert.True(t, result.FlareSolverrConfig.Enabled)
		assert.Equal(t, "http://fs:8191", result.FlareSolverrConfig.URL)

		// Metadata
		assert.True(t, result.NFOEnabled)
		assert.Equal(t, "{id}.nfo", result.NFOFilenameTemplate)
		assert.True(t, result.NFOPerFile)
		assert.Equal(t, "original", result.NFODisplayTitle)
		assert.True(t, result.TranslationConfig.Enabled)
		assert.Equal(t, "openai", result.TranslationConfig.Provider)

		// Output
		assert.Equal(t, string(operationmode.OperationModeOrganize), result.OperationMode)
		assert.True(t, result.AllowRevert)

		// Performance
		assert.Equal(t, 8, result.MaxWorkers)
		assert.Equal(t, 120*time.Second, result.WorkerTimeout)

		// Matching
		assert.True(t, result.RegexEnabled)
		assert.Equal(t, "([A-Z]+-\\d+)", result.RegexPattern)

		// System
		assert.Equal(t, "/tmp/jav", result.TempDir)
		assert.True(t, result.VersionCheckEnabled)

		// Logging
		assert.Equal(t, "debug", result.LogLevel)

		// Database
		assert.Equal(t, "data/javinizer.db", result.DatabaseDSN)
		assert.Equal(t, "warn", result.DatabaseLogLevel)
	})
}
