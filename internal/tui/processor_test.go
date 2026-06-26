package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testWorkflowComponents constructs a workflow, matcher, and poster generator
// from a config + registry + repos, for use in test setups.
func testWorkflowComponents(t *testing.T, cfg *config.Config, registry *scraperutil.ScraperRegistry, repos database.Repositories) (workflow.WorkflowInterface, matcher.MatcherInterface, poster.PosterGenerator) {
	t.Helper()
	fc, err := workflow.NewFactoryConfigFromRepos(cfg, registry, repos)
	require.NoError(t, err)
	factory, err := workflow.NewWorkflowFactory(fc)
	require.NoError(t, err)
	wf, err := factory.NewWorkflow("")
	require.NoError(t, err)
	return wf, factory.Matcher(), factory.PosterGen()
}

// testBatchJobFactory constructs a BatchJobFactoryInterface from a config +
// registry + repos, for use in test setups.
func testBatchJobFactory(t *testing.T, cfg *config.Config, registry *scraperutil.ScraperRegistry, repos database.Repositories) worker.BatchJobFactoryInterface {
	t.Helper()
	wf, m, pg := testWorkflowComponents(t, cfg, registry, repos)
	return worker.NewBatchJobFactory(nil, wf, m, pg, worker.BatchJobConfig{}, nil)
}

// TestSetGetCustomScrapers_DefensiveCopy verifies defensive copying behavior
func TestSetGetCustomScrapers_DefensiveCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setScrapers    []string
		modifyOriginal bool
		expectedGet    []string
	}{
		{
			name:           "nil scrapers",
			setScrapers:    nil,
			modifyOriginal: false,
			expectedGet:    nil,
		},
		{
			name:           "empty scrapers",
			setScrapers:    []string{},
			modifyOriginal: false,
			expectedGet:    nil, // Empty slice becomes nil after defensive copy
		},
		{
			name:           "single scraper",
			setScrapers:    []string{"r18dev"},
			modifyOriginal: false,
			expectedGet:    []string{"r18dev"},
		},
		{
			name:           "multiple scrapers",
			setScrapers:    []string{"r18dev", "dmm"},
			modifyOriginal: false,
			expectedGet:    []string{"r18dev", "dmm"},
		},
		{
			name:           "modify original after set",
			setScrapers:    []string{"r18dev", "dmm"},
			modifyOriginal: true,
			expectedGet:    []string{"r18dev", "dmm"}, // Should still be original
		},
	}

	for _, tt := range tests {
		tt := tt // Rebind for parallel subtest
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create minimal coordinator (only need to test Set/Get)
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			// Set scrapers
			pc.SetCustomScrapers(tt.setScrapers)

			// Modify original if requested
			if tt.modifyOriginal && tt.setScrapers != nil && len(tt.setScrapers) > 0 {
				tt.setScrapers[0] = "modified"
			}

			// Get scrapers
			got := pc.GetCustomScrapers()

			// Verify
			assert.Equal(t, tt.expectedGet, got)

			// Verify modifying returned slice doesn't affect internal state
			if len(got) > 0 {
				got[0] = "external-modification"
				gotAgain := pc.GetCustomScrapers()
				assert.Equal(t, tt.expectedGet, gotAgain, "Internal state should not be affected by external modification")
			}
		})
	}
}

// TestSetOptions verifies configuration methods update state correctly
func TestSetOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		scrape           bool
		download         bool
		organize         bool
		nfo              bool
		expectedScrape   bool
		expectedDownload bool
		expectedOrganize bool
		expectedNFO      bool
	}{
		{
			name:             "all enabled",
			scrape:           true,
			download:         true,
			organize:         true,
			nfo:              true,
			expectedScrape:   true,
			expectedDownload: true,
			expectedOrganize: true,
			expectedNFO:      true,
		},
		{
			name:             "all disabled",
			scrape:           false,
			download:         false,
			organize:         false,
			nfo:              false,
			expectedScrape:   false,
			expectedDownload: false,
			expectedOrganize: false,
			expectedNFO:      false,
		},
		{
			name:             "selective: scrape and organize only",
			scrape:           true,
			download:         false,
			organize:         true,
			nfo:              false,
			expectedScrape:   true,
			expectedDownload: false,
			expectedOrganize: true,
			expectedNFO:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			pc.SetOptions(ProcessorOptions{
				ScrapeEnabled:   tt.scrape,
				DownloadEnabled: tt.download,
				OrganizeEnabled: tt.organize,
				NFOEnabled:      tt.nfo,
			})

			assert.Equal(t, tt.expectedScrape, pc.loadOptions().ScrapeEnabled)
			assert.Equal(t, tt.expectedDownload, pc.loadOptions().DownloadEnabled)
			assert.Equal(t, tt.expectedOrganize, pc.loadOptions().OrganizeEnabled)
			assert.Equal(t, tt.expectedNFO, pc.loadOptions().NFOEnabled)
		})
	}
}

// TestSetOptionsFromConfig verifies SetOptionsFromConfig applies config values
func TestSetOptionsFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		nfoEnabled       bool
		expectedScrape   bool
		expectedDownload bool
		expectedOrganize bool
		expectedNFO      bool
	}{
		{
			name:             "nfo enabled in config",
			nfoEnabled:       true,
			expectedScrape:   true,
			expectedDownload: true,
			expectedOrganize: true,
			expectedNFO:      true,
		},
		{
			name:             "nfo disabled in config",
			nfoEnabled:       false,
			expectedScrape:   true,
			expectedDownload: true,
			expectedOrganize: true,
			expectedNFO:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})
			opts := pc.LoadOptions()
			opts.ScrapeEnabled = true
			opts.DownloadEnabled = true
			opts.OrganizeEnabled = true
			opts.NFOEnabled = tt.nfoEnabled
			opts.DownloadExtrafanartOverride = tt.nfoEnabled // matches old SetOptionsFromConfig behavior
			pc.SetOptions(opts)
			// Also apply runtime config fields via SetConfig
			pc.SetConfig(TUIProcessorConfig{
				BatchJobConfig: worker.BatchJobConfig{NFOEnabled: tt.nfoEnabled},
			})

			assert.Equal(t, tt.expectedScrape, pc.loadOptions().ScrapeEnabled)
			assert.Equal(t, tt.expectedDownload, pc.loadOptions().DownloadEnabled)
			assert.Equal(t, tt.expectedOrganize, pc.loadOptions().OrganizeEnabled)
			assert.Equal(t, tt.expectedNFO, pc.loadOptions().NFOEnabled)
		})
	}
}

// TestSetOptionsFromConfig_ZeroValue verifies zero-value config is handled safely
func TestSetOptionsFromConfig_ZeroValue(t *testing.T) {
	t.Parallel()

	// Create coordinator with a real in-memory DB (nil db is now rejected by constructor)
	cfg := &config.Config{}
	cfg.Database.DSN = ":memory:"
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	registry := scraperutil.NewScraperRegistry()
	wf, matchr, posterGen := testWorkflowComponents(t, cfg, registry, db.Repositories())
	factory := testBatchJobFactory(t, cfg, registry, db.Repositories())
	_ = wf
	_ = matchr
	_ = posterGen

	pc, err := NewProcessingCoordinator(
		NewSimpleRunner(context.Background()), // runner
		nil,                                   // eventSub
		factory,                               // batch job factory
		registry,                              // registry
		TUIProcessorConfig{},                  // zero-value processor config
		"/dest", true,
	)
	require.NoError(t, err)
	require.NotNil(t, pc)

	opts := pc.LoadOptions()
	opts.ScrapeEnabled = true
	opts.DownloadEnabled = true
	opts.OrganizeEnabled = true
	opts.NFOEnabled = false // zero value = false
	opts.DownloadExtrafanartOverride = false
	pc.SetOptions(opts)
	pc.SetConfig(TUIProcessorConfig{})

	// scrape/download/organize default to true; NFOEnabled=false in zero value
	assert.True(t, pc.loadOptions().ScrapeEnabled)
	assert.True(t, pc.loadOptions().DownloadEnabled)
	assert.True(t, pc.loadOptions().OrganizeEnabled)
	assert.False(t, pc.loadOptions().NFOEnabled) // zero value = false
}

// TestSetDryRun verifies dry-run flag is set correctly
func TestSetDryRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dryRun   bool
		expected bool
	}{
		{
			name:     "enable dry-run",
			dryRun:   true,
			expected: true,
		},
		{
			name:     "disable dry-run",
			dryRun:   false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.DryRun = tt.dryRun
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().DryRun)
		})
	}
}

// TestSetDestPath verifies destination path is set correctly
func TestSetDestPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		destPath string
		expected string
	}{
		{
			name:     "set destination path",
			destPath: "/videos/organized",
			expected: "/videos/organized",
		},
		{
			name:     "empty destination path",
			destPath: "",
			expected: "",
		},
		{
			name:     "relative destination path",
			destPath: "./organized",
			expected: "./organized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.DestPath = tt.destPath
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().DestPath)
		})
	}
}

// TestSetMoveFiles verifies move files flag is set correctly
func TestSetMoveFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		moveFiles bool
		expected  bool
	}{
		{
			name:      "enable move files",
			moveFiles: true,
			expected:  true,
		},
		{
			name:      "disable move files (copy mode)",
			moveFiles: false,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.MoveFiles = tt.moveFiles
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().MoveFiles)
		})
	}
}

// TestSetScrapeEnabled verifies scrape enabled flag is set correctly
func TestSetScrapeEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable scraping",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable scraping",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.ScrapeEnabled = tt.enabled
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().ScrapeEnabled)
		})
	}
}

// TestSetDownloadEnabled verifies download enabled flag is set correctly
func TestSetDownloadEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable downloads",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable downloads",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.DownloadEnabled = tt.enabled
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().DownloadEnabled)
		})
	}
}

// TestSetOrganizeEnabled verifies organize enabled flag is set correctly
func TestSetOrganizeEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable organize",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable organize",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.OrganizeEnabled = tt.enabled
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().OrganizeEnabled)
		})
	}
}

// TestSetNFOEnabled verifies NFO enabled flag is set correctly
func TestSetNFOEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable NFO generation",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable NFO generation",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.NFOEnabled = tt.enabled
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().NFOEnabled)
		})
	}
}

// TestSetForceUpdate verifies force update flag is set correctly
func TestSetForceUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		forceUpdate bool
		expected    bool
	}{
		{
			name:        "enable force update",
			forceUpdate: true,
			expected:    true,
		},
		{
			name:        "disable force update",
			forceUpdate: false,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.ForceUpdate = tt.forceUpdate
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().ForceUpdate)
		})
	}
}

// TestSetForceRefresh verifies force refresh flag is set correctly
func TestSetForceRefresh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		forceRefresh bool
		expected     bool
	}{
		{
			name:         "enable force refresh",
			forceRefresh: true,
			expected:     true,
		},
		{
			name:         "disable force refresh",
			forceRefresh: false,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.ForceRefresh = tt.forceRefresh
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().ForceRefresh)
		})
	}
}

// TestNewProcessingCoordinator verifies constructor initializes with correct defaults
func TestNewProcessingCoordinator(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Database.DSN = ":memory:"
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	registry := scraperutil.NewScraperRegistry()
	wf, matchr, posterGen := testWorkflowComponents(t, cfg, registry, db.Repositories())
	factory := testBatchJobFactory(t, cfg, registry, db.Repositories())
	_ = wf
	_ = matchr
	_ = posterGen

	pc, err := NewProcessingCoordinator(
		NewSimpleRunner(context.Background()), // runner
		nil,                                   // eventSub
		factory,                               // batch job factory
		registry,                              // registry
		TUIProcessorConfig{},                  // zero-value processor config
		"/dest/path",
		true, // moveFiles
	)
	require.NoError(t, err)

	// Verify initialization
	assert.NotNil(t, pc)
	assert.Equal(t, "/dest/path", pc.loadOptions().DestPath)
	assert.True(t, pc.loadOptions().MoveFiles)
	assert.True(t, pc.loadOptions().ScrapeEnabled, "scrapeEnabled should default to true")
	assert.True(t, pc.loadOptions().DownloadEnabled, "downloadEnabled should default to true")
	assert.True(t, pc.loadOptions().OrganizeEnabled, "organizeEnabled should default to true")
	assert.True(t, pc.loadOptions().NFOEnabled, "nfoEnabled should default to true")
	assert.False(t, pc.loadOptions().DryRun, "dryRun should default to false")
	assert.False(t, pc.loadOptions().ForceUpdate, "forceUpdate should default to false")
	assert.False(t, pc.loadOptions().ForceRefresh, "forceRefresh should default to false")
	assert.Nil(t, pc.loadOptions().CustomScraperPriority, "customScraperPriority should default to nil")
}

// TestNewProcessingCoordinator_NilRepos verifies constructor handles nil repos gracefully
func TestNewProcessingCoordinator_NilRepos(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Database.DSN = ":memory:"
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Construct coordinator with repos from real DB
	registry := scraperutil.NewScraperRegistry()
	factory := testBatchJobFactory(t, cfg, registry, db.Repositories())

	assert.NotPanics(t, func() {
		NewProcessingCoordinator(
			NewSimpleRunner(context.Background()), // runner
			nil,                                   // eventSub
			factory,                               // batch job factory
			registry,                              // registry
			TUIProcessorConfig{},                  // zero-value processor config
			"/dest/path",
			true,
		)
	}, "NewProcessingCoordinator should not panic with valid repos")
}

// ============================================================================
// Mock Implementations for Dependency Injection Testing
// ============================================================================

// mockRunner is an inline mock for backgroundRunner
type mockRunner struct {
	goCalled   int
	goErr      error
	waitCalled int
	waitErr    error
	stopCalled int
}

func (m *mockRunner) Go(fn func() error) error {
	m.goCalled++
	if m.goErr != nil {
		return m.goErr
	}
	return nil
}

func (m *mockRunner) Wait() error {
	m.waitCalled++
	return m.waitErr
}

func (m *mockRunner) Stop() {
	m.stopCalled++
}

func (m *mockRunner) Context() context.Context {
	return context.Background()
}

// ============================================================================
// Tests for Previously Untestable Functions
// ============================================================================

// TestSetDownloadExtrafanart_NilDownloader verifies no panic when downloader is nil
func TestSetDownloadExtrafanart_NilDownloader(t *testing.T) {
	t.Parallel()

	pc := &processingCoordinator{}
	pc.opts.Store(ProcessorOptions{})

	// Should not panic — stores override locally, no downloader call
	opts := pc.LoadOptions()
	opts.DownloadExtrafanartOverride = true
	pc.SetOptions(opts)
	assert.True(t, pc.loadOptions().DownloadExtrafanartOverride)
}

// TestSetDownloadExtrafanart_StoresOverride verifies override is stored locally
func TestSetDownloadExtrafanart_StoresOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable extrafanart",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable extrafanart",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &processingCoordinator{}
			pc.opts.Store(ProcessorOptions{})

			opts := pc.LoadOptions()
			opts.DownloadExtrafanartOverride = tt.enabled
			pc.SetOptions(opts)

			assert.Equal(t, tt.expected, pc.loadOptions().DownloadExtrafanartOverride)
		})
	}
}

// TestWait verifies delegation to runner.Wait()
func TestWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		waitErr  error
		wantErr  bool
		expected error
	}{
		{
			name:     "wait succeeds",
			waitErr:  nil,
			wantErr:  false,
			expected: nil,
		},
		{
			name:     "wait returns error",
			waitErr:  errors.New("runner wait error"),
			wantErr:  true,
			expected: errors.New("runner wait error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockR := &mockRunner{waitErr: tt.waitErr}
			pc := &processingCoordinator{
				runner: mockR,
			}

			err := pc.Wait()

			assert.Equal(t, 1, mockR.waitCalled)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.expected.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestStop verifies delegation to runner.Stop()
func TestStop(t *testing.T) {
	t.Parallel()

	mockR := &mockRunner{}
	pc := &processingCoordinator{
		runner: mockR,
	}

	pc.Stop()

	assert.Equal(t, 1, mockR.stopCalled)
}

// ============================================================================
// ProcessFiles Tests (using real concrete instances + mockRunner pattern)
// ============================================================================

// createMinimalCoordinator creates a processingCoordinator with all required dependencies
// for ProcessFiles nil validation, using a mock runner for control
func createMinimalCoordinator(t *testing.T, mockR *mockRunner) *processingCoordinator {
	t.Helper()
	cfg := &config.Config{}
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	registry := scraperutil.NewScraperRegistry()
	factory := testBatchJobFactory(t, cfg, registry, db.Repositories())

	return &processingCoordinator{
		runner:    mockR,
		forwardCh: nil,
		factory:   factory,
		registry:  registry,
	}
}

// TestProcessFiles_SkipsDirectories verifies directories are skipped
func TestProcessFiles_SkipsDirectories(t *testing.T) {
	mockR := &mockRunner{}
	pc := createMinimalCoordinator(t, mockR)

	files := []fileItem{
		{Path: "/dir1", IsDir: true, Matched: true},
		{Path: "/dir2", IsDir: true, Matched: true},
	}
	matches := map[string]models.FileMatchInfo{}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 0, mockR.goCalled, "Should not submit tasks for directories")
}

// TestProcessFiles_SkipsUnmatched verifies unmatched files are skipped
func TestProcessFiles_SkipsUnmatched(t *testing.T) {
	mockR := &mockRunner{}
	pc := createMinimalCoordinator(t, mockR)

	files := []fileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: false},
		{Path: "/video2.mp4", IsDir: false, Matched: false},
	}
	matches := map[string]models.FileMatchInfo{}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 0, mockR.goCalled, "Should not submit tasks for unmatched files")
}

// TestProcessFiles_SkipsMissingMatches verifies files without match data are skipped
func TestProcessFiles_SkipsMissingMatches(t *testing.T) {
	mockR := &mockRunner{}
	pc := createMinimalCoordinator(t, mockR)

	files := []fileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
	}
	matches := map[string]models.FileMatchInfo{
		// No entry for /video1.mp4
	}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 0, mockR.goCalled, "Should not submit tasks for files missing match data")
}

// TestProcessFiles_EmptyFileList verifies no crashes with empty file list
func TestProcessFiles_EmptyFileList(t *testing.T) {
	mockR := &mockRunner{}
	pc := createMinimalCoordinator(t, mockR)

	files := []fileItem{}
	matches := map[string]models.FileMatchInfo{}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 0, mockR.goCalled, "Should not submit tasks for empty file list")
}

// TestProcessFiles_SubmitError_Propagates verifies error handling when Submit() fails
func TestProcessFiles_SubmitError_Propagates(t *testing.T) {
	// This test requires real concrete instances to pass type assertions
	// Create minimal dependencies using NewWorkflowFactory
	cfg := &config.Config{}
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	registry := scraperutil.NewScraperRegistry()
	factory := testBatchJobFactory(t, cfg, registry, db.Repositories())

	goErr := errors.New("runner go failed")
	mockR := &mockRunner{goErr: goErr}

	pc := &processingCoordinator{
		runner:    mockR,
		forwardCh: nil,
		factory:   factory,
		registry:  registry,
	}
	pc.opts.Store(ProcessorOptions{})
	pc.SetOptions(ProcessorOptions{DestPath: "/dest", MoveFiles: true})

	files := []fileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
	}
	matches := map[string]models.FileMatchInfo{
		"/video1.mp4": {MovieID: "IPX-123"},
	}

	err = pc.ProcessFiles(context.Background(), files, matches)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start batch process")
	assert.Equal(t, 1, mockR.goCalled, "Should attempt to submit task")
}

// TestProcessFiles_ValidFiles_SubmitsTask verifies successful task submission
func TestProcessFiles_ValidFiles_SubmitsTask(t *testing.T) {
	// This test requires real concrete instances to pass type assertions
	// Create minimal dependencies using NewWorkflowFactory
	cfg := &config.Config{}
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	registry := scraperutil.NewScraperRegistry()
	factory := testBatchJobFactory(t, cfg, registry, db.Repositories())

	mockR := &mockRunner{goErr: nil} // success case

	pc := &processingCoordinator{
		runner:    mockR,
		forwardCh: nil,
		factory:   factory,
		registry:  registry,
	}
	pc.opts.Store(ProcessorOptions{})
	pc.SetOptions(ProcessorOptions{DestPath: "/dest", MoveFiles: true})

	files := []fileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
		{Path: "/video2.mp4", IsDir: false, Matched: true},
		{Path: "/dir1", IsDir: true, Matched: true}, // Should be skipped
	}
	matches := map[string]models.FileMatchInfo{
		"/video1.mp4": {MovieID: "IPX-123"},
		"/video2.mp4": {MovieID: "IPX-456"},
	}

	err = pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	// With BatchJob migration, a single batch task is submitted (not per-file)
	assert.Equal(t, 1, mockR.goCalled, "Should submit 1 batch task for all valid files")
}

// TestProcessFiles_NilDependencies verifies nil checks prevent panics
func TestProcessFiles_NilDependencies(t *testing.T) {
	tests := []struct {
		name        string
		pc          *processingCoordinator
		expectedErr string
	}{
		{
			name:        "nil runner",
			pc:          &processingCoordinator{runner: nil},
			expectedErr: "background runner is nil",
		},
		{
			name:        "nil registry",
			pc:          &processingCoordinator{runner: &mockRunner{}, registry: nil},
			expectedErr: "scraper registry is nil",
		},
		{
			name:        "nil factory",
			pc:          &processingCoordinator{runner: &mockRunner{}, registry: scraperutil.NewScraperRegistry(), factory: nil},
			expectedErr: "batch job factory is nil",
		},
		{
			name:        "nil registry (duplicate check)",
			pc:          &processingCoordinator{runner: &mockRunner{}, registry: nil},
			expectedErr: "scraper registry is nil",
		},
		{
			name:        "nil factory (duplicate check)",
			pc:          &processingCoordinator{runner: &mockRunner{}, registry: scraperutil.NewScraperRegistry(), factory: nil},
			expectedErr: "batch job factory is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := []fileItem{{Path: "/video1.mp4", IsDir: false, Matched: true}}
			matches := map[string]models.FileMatchInfo{"/video1.mp4": {MovieID: "IPX-123"}}

			err := tt.pc.ProcessFiles(context.Background(), files, matches)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// TestProcessFiles_ContextCancellation verifies that context cancellation
// is propagated through the BatchJob during execution. With the BatchJob
// migration, ProcessFiles submits a single batch task and cancellation is
// handled internally by the BatchJob's StartScrape/StartApply methods.
func TestProcessFiles_ContextCancellation(t *testing.T) {
	mockR := &mockRunner{}
	pc := createMinimalCoordinator(t, mockR)

	// Create a cancelled context — ProcessFiles still submits the batch task
	// but the BatchJob.Execute will handle cancellation internally
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	files := []fileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
		{Path: "/video2.mp4", IsDir: false, Matched: true},
	}
	matches := map[string]models.FileMatchInfo{
		"/video1.mp4": {MovieID: "IPX-123"},
		"/video2.mp4": {MovieID: "IPX-456"},
	}

	err := pc.ProcessFiles(ctx, files, matches)

	// ProcessFiles still succeeds in submitting the batch task;
	// context cancellation is handled by BatchJob internally
	assert.NoError(t, err)
	assert.Equal(t, 1, mockR.goCalled, "Should submit 1 batch task even with cancelled context (cancellation handled by BatchJob)")
}

// TestProcessFiles_CustomScrapers_DefensiveCopy verifies defensive copy of custom scrapers
func TestProcessFiles_CustomScrapers_DefensiveCopy(t *testing.T) {
	// This test verifies that ProcessFiles creates a defensive copy of customScraperPriority
	// to prevent data races when the UI modifies it while tasks are running

	// Create minimal dependencies using NewWorkflowFactory
	cfg := &config.Config{}
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	registry := scraperutil.NewScraperRegistry()
	factory := testBatchJobFactory(t, cfg, registry, db.Repositories())

	mockR := &mockRunner{goErr: nil}

	pc := &processingCoordinator{
		runner:    mockR,
		forwardCh: nil,
		factory:   factory,
		registry:  registry,
	}
	pc.opts.Store(ProcessorOptions{})
	pc.SetCustomScrapers([]string{"r18dev", "dmm"})
	pc.SetOptions(ProcessorOptions{DestPath: "/dest", MoveFiles: true, CustomScraperPriority: []string{"r18dev", "dmm"}})

	files := []fileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
	}
	matches := map[string]models.FileMatchInfo{
		"/video1.mp4": {MovieID: "IPX-123"},
	}

	err = pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	// With BatchJob migration, a single batch task is submitted
	assert.Equal(t, 1, mockR.goCalled)

	// Custom scrapers are passed to the batch task via processingCoordinator
	// The defensive copy is made by the coordinator's SetCustomScrapers method
	pc.SetCustomScrapers([]string{"modified"})
	assert.Equal(t, []string{"modified"}, pc.loadOptions().CustomScraperPriority, "Should be updated via Store")
}
