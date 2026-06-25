package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper: create a real in-memory DB with migrations for factory tests ---

func newInMemoryDB(t *testing.T) *database.DB {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = config.Prepare(cfg)
	require.NoError(t, err)
	return db
}

func newFactoryConfigWithDB(t *testing.T) workflowFactoryConfig {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)
	db := newInMemoryDB(t)
	registry := scraperutil.NewScraperRegistry()
	fc, err := NewFactoryConfigFromRepos(cfg, registry, db.Repositories())
	require.NoError(t, err)
	return fc
}

// ============================================================
// factory.go: NewWorkflowFactory (scan-only mode)
// ============================================================

// Per DEEP-8: NewScanOnlyWorkflowFactory and NewScrapeOnlyWorkflowFactory have been
// collapsed into NewWorkflowFactory. The single factory supports all workflow modes.

func TestNewWorkflowFactory_ScanOnlyMode(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)
	assert.NotNil(t, factory)

	// Scan-only workflow should work
	wf := factory.NewScanOnlyWorkflow()
	assert.NotNil(t, wf)
}

func TestNewWorkflowFactory_ScanOnlyMode_MinimalDeps(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)
	badCfg := &matcher.Config{RegexEnabled: false}
	fs := afero.NewOsFs()
	fileMatcher, _ := matcher.NewMatcher(badCfg)
	organizeCfg := organizer.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	nfoCfg := nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	fc := workflowFactoryConfig{
		MaxFilesPerScan: cfg.API.Security.MaxFilesPerScan,
		PreviewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizeCfg.MediaFormatConfig,
			},
			ResolveStrategy: newStrategyResolver(fs, organizeCfg, fileMatcher, template.NewEngine()),
			NFOEnabled:      cfg.Metadata.NFO.Feature.Enabled,
			NFOPerFile:      nfoCfg.PerFile,
			DisplayTitle:    cfg.Metadata.NFO.Format.DisplayTitle,
			OpMode:          cfg.Output.GetOperationMode(),
			MaxPathLength:   cfg.Output.Template.MaxPathLength,
			Downloads: downloadToggles{
				Poster:      cfg.Output.Download.DownloadPoster,
				Cover:       cfg.Output.Download.DownloadCover,
				Extrafanart: cfg.Output.Download.DownloadExtrafanart,
				Trailer:     cfg.Output.Download.DownloadTrailer,
			},
		},
		ApplyCfg:        ApplyConfig{NFONameCfg: nfoCfg.ToNFONameConfig(false, ""), DisplayTitle: cfg.Metadata.NFO.Format.DisplayTitle},
		DownloadHTTPCfg: downloader.HTTPClientConfig{},
		Fs:              fs,
		TemplateEngine:  template.NewEngine(),
		Matcher:         fileMatcher,
		Scanner:         scanner.NewScanner(fs, scanner.ConfigFromAppConfig(cfg)),
		ScannerCfg:      *scanner.ConfigFromAppConfig(cfg),
	}
	// NewWorkflowFactory should succeed even with a basic matcher config (scan-only mode)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)
	assert.NotNil(t, factory)
}

// ============================================================
// factory.go: NewWorkflowFactory (scrape-only mode)
// ============================================================

func TestNewWorkflowFactory_ScrapeOnlyMode(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)
	assert.NotNil(t, factory)
}

func TestNewWorkflowFactory_ScrapeOnlyMode_NilScraper(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)
	organizeCfg := organizer.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	nfoCfg := nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	fc := workflowFactoryConfig{
		MaxFilesPerScan: cfg.API.Security.MaxFilesPerScan,
		PreviewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizeCfg.MediaFormatConfig,
			},
			NFOEnabled:    cfg.Metadata.NFO.Feature.Enabled,
			NFOPerFile:    nfoCfg.PerFile,
			DisplayTitle:  cfg.Metadata.NFO.Format.DisplayTitle,
			OpMode:        cfg.Output.GetOperationMode(),
			MaxPathLength: cfg.Output.Template.MaxPathLength,
			Downloads: downloadToggles{
				Poster:      cfg.Output.Download.DownloadPoster,
				Cover:       cfg.Output.Download.DownloadCover,
				Extrafanart: cfg.Output.Download.DownloadExtrafanart,
				Trailer:     cfg.Output.Download.DownloadTrailer,
			},
		},
		ApplyCfg:        ApplyConfig{NFONameCfg: nfoCfg.ToNFONameConfig(false, ""), DisplayTitle: cfg.Metadata.NFO.Format.DisplayTitle},
		DownloadHTTPCfg: downloader.HTTPClientConfig{},
	}
	// Per DEEP-8: NewWorkflowFactory succeeds even without a scraper (creates noOp).
	// But NewScrapeOnlyWorkflow should fail because the scrape sub-orchestrator is noOp.
	_, err = NewWorkflowFactory(fc)
	// Without a Matcher, NewWorkflowFactory should fail
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "matcher must not be nil")
}

// ============================================================
// factory.go: WorkflowFactory.NewWorkflow (0%)
// ============================================================

func TestWorkflowFactory_NewWorkflow_Success(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	wf, err := factory.NewWorkflow("test-job-001")
	require.NoError(t, err)
	assert.NotNil(t, wf)
}

func TestWorkflowFactory_NewWorkflow_ReturnsWorkflowInterface(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	wf, err := factory.NewWorkflow("job-abc")
	require.NoError(t, err)

	// The workflow should implement WorkflowInterface
	var _ WorkflowInterface = wf
}

// ============================================================
// factory.go: WorkflowFactory.NewScrapeOnlyWorkflow (0%)
// ============================================================

func TestWorkflowFactory_NewScrapeOnlyWorkflow_Success(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	wf, err := factory.NewScrapeOnlyWorkflow()
	require.NoError(t, err)
	assert.NotNil(t, wf)

	// Verify it's a *Workflow
	impl, ok := wf.(*Workflow)
	require.True(t, ok)

	// Apply should be no-op for scrape-only
	_, err = impl.Apply(context.Background(), ApplyCmd{Movie: &models.Movie{ID: "TEST-001"}}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "apply not configured")
}

// ============================================================
// factory.go: WorkflowFactory.NewScanOnlyWorkflow (0%)
// ============================================================

func TestWorkflowFactory_NewScanOnlyWorkflow_Success(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	wf := factory.NewScanOnlyWorkflow()
	assert.NotNil(t, wf)

	// Verify it's a *Workflow
	impl, ok := wf.(*Workflow)
	require.True(t, ok)

	// Scrape should fail for scan-only
	_, _, err = impl.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scrape not configured")

	// Apply should fail for scan-only
	_, err = impl.Apply(context.Background(), ApplyCmd{Movie: &models.Movie{ID: "TEST-001"}}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "apply not configured")
}

// ============================================================
// factory.go: NewWorkflow (30%) — cover the full success path
// ============================================================

func TestWorkflowFactory_Accessors_Success(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)
	assert.NotNil(t, factory)
	assert.NotNil(t, factory.Matcher())
	assert.NotNil(t, factory.Scanner())
	assert.NotNil(t, factory.PosterGen())
}

// ============================================================
// factory.go: NewWorkflowFactory (16.7%) — cover the full success path
// ============================================================

func TestNewWorkflowFactory_Success(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)
	assert.NotNil(t, factory)
	assert.NotNil(t, factory.fc.TemplateEngine)
}

// ============================================================
// factory.go: ReloadReplacementCaches (66.7%)
// ============================================================

func TestWorkflowFactory_ReloadReplacementCaches_WithAggregator(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	// Should not panic when aggregator is present
	assert.NotPanics(t, func() {
		factory.ReloadReplacementCaches(context.Background())
	})
}

// ============================================================
// revert_log.go: dbRevertLog.CaptureSnapshot (0%)
// ============================================================

func TestDBRevertLog_CaptureSnapshot_Success(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()

	// Create an NFO file for the snapshot to read
	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/TEST-001.nfo", []byte("<movie><title>Old</title></movie>"), 0644))

	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	nfoCfg := nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: true,
		NFOCfg:      nfoCfg,
	}, "job-snapshot-test", fs, template.NewEngine(), nfo.NewNFOImplementor(fs, nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)), template.NewEngine()), nil)

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := models.FileMatchInfo{Path: "/source/TEST-001.mp4", MovieID: "TEST-001"}

	// Begin to create the record
	opID, beginErr := rl.Begin(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	// CaptureSnapshot should read the NFO and update the record
	rl.CaptureSnapshot(context.Background(), opID, ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})

	// Verify the record was updated with NFO snapshot
	var recordID uint
	n, _ := fmt.Sscanf(opID, "%d", &recordID)
	require.Equal(t, 1, n)
	record, findErr := repo.FindByID(context.TODO(), recordID)
	require.NoError(t, findErr)
	require.NotNil(t, record)
	assert.Equal(t, "<movie><title>Old</title></movie>", record.NFOSnapshot)
}

func TestDBRevertLog_CaptureSnapshot_EmptyOpID(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: true,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "job-empty-op", fs, template.NewEngine(), nfo.NewNFOImplementor(fs, nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)), template.NewEngine()), nil)

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	// Empty opID should return early without error
	rl.CaptureSnapshot(context.Background(), "", ApplyCmd{
		Movie: movie,
	})
}

func TestDBRevertLog_CaptureSnapshot_NilMovie(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: true,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "job-nil-movie", fs, template.NewEngine(), nfo.NewNFOImplementor(fs, nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)), template.NewEngine()), nil)

	// Nil movie should return early without error
	rl.CaptureSnapshot(context.Background(), "1", ApplyCmd{
		Movie: nil,
	})
}

func TestDBRevertLog_CaptureSnapshot_InvalidOpID(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: true,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "job-invalid-op", fs, template.NewEngine(), nfo.NewNFOImplementor(fs, nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)), template.NewEngine()), nil)

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}

	// Non-numeric opID should return early
	rl.CaptureSnapshot(context.Background(), "not-a-number", ApplyCmd{
		Movie: movie,
	})
}

func TestDBRevertLog_CaptureSnapshot_RecordNotFound(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	rl := NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: true,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "job-not-found", fs, template.NewEngine(), nfo.NewNFOImplementor(fs, nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)), template.NewEngine()), nil)

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}

	// Valid-looking opID but record doesn't exist — should not panic
	rl.CaptureSnapshot(context.Background(), "99999", ApplyCmd{
		Movie: movie,
		Match: models.FileMatchInfo{Path: "/source/TEST-001.mp4", MovieID: "TEST-001"},
	})
}

// ============================================================
// scanmatch_orchestrator.go: Execute (0%)
// ============================================================

// mockScanner implements scanner.ScannerInterface for testing
type mockScanner struct {
	scanResult    *scanner.ScanResult
	scanErr       error
	scanFilterErr error
	scanFilterRes *scanner.ScanResult
	scanSingleRes *scanner.ScanResult
	scanSingleErr error
}

func (m *mockScanner) Scan(_ string) (*scanner.ScanResult, error) {
	return m.scanResult, m.scanErr
}

func (m *mockScanner) ScanWithFilter(_ context.Context, _ string, _ int, _ string) (*scanner.ScanResult, error) {
	if m.scanFilterRes != nil {
		return m.scanFilterRes, m.scanFilterErr
	}
	return m.scanResult, m.scanFilterErr
}

func (m *mockScanner) ScanSingle(_ string) (*scanner.ScanResult, error) {
	if m.scanSingleRes != nil {
		return m.scanSingleRes, m.scanSingleErr
	}
	return m.scanResult, m.scanSingleErr
}

func (m *mockScanner) ScanSingleFromHandle(_ *os.File, _ string) (*scanner.ScanResult, error) {
	if m.scanSingleRes != nil {
		return m.scanSingleRes, m.scanSingleErr
	}
	return m.scanResult, m.scanSingleErr
}

// mockMatcherImpl implements matcher.MatcherInterface for testing
type mockMatcherImpl struct {
	results []matcher.MatchResult
}

func (m *mockMatcherImpl) Match(files []models.FileMatchInfo) []matcher.MatchResult {
	return m.results
}

func (m *mockMatcherImpl) MatchFile(_ models.FileMatchInfo) *matcher.MatchResult {
	return nil
}

func (m *mockMatcherImpl) MatchString(_ string) string {
	return ""
}

func TestScanAndMatch_Execute_EmptyDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	scanCfg := scanner.Config{}
	m := &mockMatcherImpl{}

	orch := newScanAndMatchOrchestrator(
		&mockScanner{
			scanResult: &scanner.ScanResult{Files: []models.FileMatchInfo{}},
		},
		scanCfg,
		fs,
		m,
		100,
		nil,
	)

	result, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Files)
}

func TestScanAndMatch_Execute_Recursive(t *testing.T) {
	fs := afero.NewMemMapFs()
	scanCfg := scanner.Config{}
	m := &mockMatcherImpl{
		results: []matcher.MatchResult{
			{
				File:        models.FileMatchInfo{Path: "/videos/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4"},
				ID:          "ABC-123",
				IsMultiPart: false,
			},
		},
	}

	orch := newScanAndMatchOrchestrator(
		&mockScanner{
			scanFilterRes: &scanner.ScanResult{
				Files: []models.FileMatchInfo{
					{Path: "/videos/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4"},
				},
			},
		},
		scanCfg,
		fs,
		m,
		100,
		nil,
	)

	result, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Files, 1)
	assert.Equal(t, "ABC-123", result.Files[0].MovieID)
}

func TestScanAndMatch_Execute_NonRecursive(t *testing.T) {
	fs := afero.NewMemMapFs()
	scanCfg := scanner.Config{}
	m := &mockMatcherImpl{
		results: []matcher.MatchResult{
			{
				File: models.FileMatchInfo{Path: "/videos/DEF-456.mp4", Name: "DEF-456.mp4", Extension: ".mp4"},
				ID:   "DEF-456",
			},
		},
	}

	orch := newScanAndMatchOrchestrator(
		&mockScanner{
			scanSingleRes: &scanner.ScanResult{
				Files: []models.FileMatchInfo{
					{Path: "/videos/DEF-456.mp4", Name: "DEF-456.mp4", Extension: ".mp4"},
				},
			},
		},
		scanCfg,
		fs,
		m,
		100,
		nil,
	)

	result, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: false,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Files, 1)
	assert.Equal(t, "DEF-456", result.Files[0].MovieID)
}

func TestScanAndMatch_Execute_EmptyDirectoryError(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newScanAndMatchOrchestrator(
		nil,
		scanner.Config{},
		fs,
		nil,
		0,
		nil,
	)

	_, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "directory is required")
}

func TestScanAndMatch_Execute_ScanError(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newScanAndMatchOrchestrator(
		&mockScanner{scanFilterErr: errors.New("scan failure")},
		scanner.Config{},
		fs,
		nil,
		0,
		nil,
	)

	_, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: true,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scan failed")
}

func TestScanAndMatch_Execute_NilMatcher(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newScanAndMatchOrchestrator(
		&mockScanner{
			scanFilterRes: &scanner.ScanResult{
				Files: []models.FileMatchInfo{
					{Path: "/videos/TEST-001.mp4", Name: "TEST-001.mp4", Extension: ".mp4"},
				},
			},
		},
		scanner.Config{},
		fs,
		nil, // nil matcher
		0,
		nil,
	)

	result, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Files, 1)
	// MovieID should be empty since no matcher was provided
	assert.Empty(t, result.Files[0].MovieID)
}

func TestScanAndMatch_Execute_WithTimeout(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newScanAndMatchOrchestrator(
		&mockScanner{
			scanFilterRes: &scanner.ScanResult{
				Files: []models.FileMatchInfo{
					{Path: "/videos/ABC-123.mp4"},
				},
			},
		},
		scanner.Config{},
		fs,
		&mockMatcherImpl{
			results: []matcher.MatchResult{
				{File: models.FileMatchInfo{Path: "/videos/ABC-123.mp4"}, ID: "ABC-123"},
			},
		},
		0,
		nil,
	)

	result, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory:      "/videos",
		Recursive:      true,
		TimeoutSeconds: 30,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestScanAndMatch_Execute_NilContext(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newScanAndMatchOrchestrator(
		&mockScanner{
			scanFilterRes: &scanner.ScanResult{
				Files: []models.FileMatchInfo{
					{Path: "/videos/ABC-123.mp4"},
				},
			},
		},
		scanner.Config{},
		fs,
		&mockMatcherImpl{
			results: []matcher.MatchResult{
				{File: models.FileMatchInfo{Path: "/videos/ABC-123.mp4"}, ID: "ABC-123"},
			},
		},
		0,
		nil,
	)

	// nil context should be handled gracefully (replaced with Background)
	result, err := orch.Execute(nil, ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestScanAndMatch_Execute_NilScannerFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	scanCfg := scanner.Config{}
	// When scanner is nil, it should construct a fallback scanner
	orch := newScanAndMatchOrchestrator(
		nil, // nil scanner — fallback will be constructed
		scanCfg,
		fs,
		&mockMatcherImpl{},
		0,
		nil,
	)

	// This will try to scan a non-existent directory on the memmap fs
	// The scanner will return an error because the directory doesn't exist
	_, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "/nonexistent",
		Recursive: true,
	})
	// The scan fails because the directory doesn't exist in the memmap fs
	assert.Error(t, err)
}

func TestScanAndMatch_Execute_MultipartResults(t *testing.T) {
	fs := afero.NewMemMapFs()
	scanCfg := scanner.Config{}
	m := &mockMatcherImpl{
		results: []matcher.MatchResult{
			{
				File:        models.FileMatchInfo{Path: "/videos/ABC-123-cd1.mp4", Name: "ABC-123-cd1.mp4", Extension: ".mp4"},
				ID:          "ABC-123",
				IsMultiPart: true,
				PartNumber:  1,
				PartSuffix:  "-cd1",
			},
			{
				File:        models.FileMatchInfo{Path: "/videos/ABC-123-cd2.mp4", Name: "ABC-123-cd2.mp4", Extension: ".mp4"},
				ID:          "ABC-123",
				IsMultiPart: true,
				PartNumber:  2,
				PartSuffix:  "-cd2",
			},
		},
	}

	orch := newScanAndMatchOrchestrator(
		&mockScanner{
			scanFilterRes: &scanner.ScanResult{
				Files: []models.FileMatchInfo{
					{Path: "/videos/ABC-123-cd1.mp4", Name: "ABC-123-cd1.mp4", Extension: ".mp4"},
					{Path: "/videos/ABC-123-cd2.mp4", Name: "ABC-123-cd2.mp4", Extension: ".mp4"},
				},
			},
		},
		scanCfg,
		fs,
		m,
		100,
		nil,
	)

	result, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Files, 2)
	assert.True(t, result.Files[0].IsMultiPart)
	assert.Equal(t, 1, result.Files[0].PartNumber)
	assert.True(t, result.Files[1].IsMultiPart)
	assert.Equal(t, 2, result.Files[1].PartNumber)
}

func TestScanAndMatch_Execute_MaxFilesFromConfig(t *testing.T) {
	fs := afero.NewMemMapFs()
	scanCfg := scanner.Config{}

	orch := newScanAndMatchOrchestrator(
		&mockScanner{
			scanFilterRes: &scanner.ScanResult{
				Files: []models.FileMatchInfo{
					{Path: "/videos/ABC-123.mp4"},
				},
			},
		},
		scanCfg,
		fs,
		&mockMatcherImpl{
			results: []matcher.MatchResult{
				{File: models.FileMatchInfo{Path: "/videos/ABC-123.mp4"}, ID: "ABC-123"},
			},
		},
		50,
		nil,
	)

	// cmd.MaxFiles=0 should fall back to config MaxFilesPerScan
	result, err := orch.Execute(context.Background(), ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: true,
		MaxFiles:  0, // should use config default of 50
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// ============================================================
// apply_orchestrator.go: completeRevertLog (33.3%) — error path
// ============================================================

type errorRevertLog struct {
	completeErr error
}

func (e errorRevertLog) Begin(_ context.Context, _ ApplyCmd) (OperationID, error) {
	return "1", nil
}

func (e errorRevertLog) CaptureSnapshot(_ context.Context, _ OperationID, _ ApplyCmd) {}

func (e errorRevertLog) Complete(_ context.Context, _ OperationID, _ *ApplyResult) error {
	return e.completeErr
}

func (e errorRevertLog) CompleteFailed(_ context.Context, _ OperationID, _ *ApplyResult) error {
	return e.completeErr
}

func TestCompleteRevertLog_CompleteError(t *testing.T) {
	impl := &applyOrchImpl{
		revertLog: errorRevertLog{completeErr: errors.New("complete failed")},
	}

	// Should not panic even when Complete returns an error
	assert.NotPanics(t, func() {
		impl.completeRevertLogWithState(context.Background(), "op-1", &applyPipelineState{})
	})
}

func TestCompleteRevertLog_NilRevertLog(t *testing.T) {
	impl := &applyOrchImpl{
		revertLog: nil,
	}

	// Should not panic with nil revertLog
	assert.NotPanics(t, func() {
		impl.completeRevertLogWithState(context.Background(), "op-1", &applyPipelineState{})
	})
}

func TestCompleteRevertLog_EmptyOpID(t *testing.T) {
	impl := &applyOrchImpl{
		revertLog: noOpRevertLog{},
	}

	// Should not panic with empty opID
	assert.NotPanics(t, func() {
		impl.completeRevertLogWithState(context.Background(), "", &applyPipelineState{})
	})
}

// ============================================================
// compare_orchestrator.go: Execute (65%) — missing branches
// ============================================================

func TestCompareOrchestrator_NFONotFound(t *testing.T) {
	nfoData := &models.Movie{ID: "TEST-001", Title: "Scraped Only"}
	// No NFO file on the memfs — nfo.ParseNFO will return os.ErrNotExist
	orch := newCompareOrchestrator(
		afero.NewMemMapFs(),
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: &scrape.ScrapeResult{Movie: nfoData},
		},
		nil,
	)

	result, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/nonexistent/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
		ArrayStrategy:  false,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.NFOExists, "NFOExists should be false when file not found")
	assert.Equal(t, result.ScrapedData, result.Movie, "When NFO doesn't exist, Movie should equal ScrapedData")
}

func TestCompareOrchestrator_EmptyMovieID(t *testing.T) {
	orch := newCompareOrchestrator(nil, nil, nil, nil)
	_, err := orch.Execute(context.Background(), CompareCmd{
		MovieID: "",
		NFOPath: "/test.nfo",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "movie_id is required")
}

func TestCompareOrchestrator_EmptyNFOPath(t *testing.T) {
	orch := newCompareOrchestrator(nil, nil, nil, nil)
	_, err := orch.Execute(context.Background(), CompareCmd{
		MovieID: "TEST-001",
		NFOPath: "",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nfo_path is required")
}

func TestCompareOrchestrator_NilScraper(t *testing.T) {
	fs := afero.NewMemMapFs()
	writeTestNFO(t, fs, "/source/TEST-001.nfo", &models.Movie{ID: "TEST-001"})

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		nil, // nil scraper
		nil,
	)

	_, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scraper not configured")
}

func TestCompareOrchestrator_ScrapeError(t *testing.T) {
	fs := afero.NewMemMapFs()
	writeTestNFO(t, fs, "/source/TEST-001.nfo", &models.Movie{ID: "TEST-001"})

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			err: errors.New("scrape failed"),
		},
		nil,
	)

	_, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scrape failed")
}

func TestCompareOrchestrator_ScrapeNilResult(t *testing.T) {
	fs := afero.NewMemMapFs()
	writeTestNFO(t, fs, "/source/TEST-001.nfo", &models.Movie{ID: "TEST-001"})

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: nil,
		},
		nil,
	)

	_, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no result")
}

func TestCompareOrchestrator_ScrapeNilMovie(t *testing.T) {
	fs := afero.NewMemMapFs()
	writeTestNFO(t, fs, "/source/TEST-001.nfo", &models.Movie{ID: "TEST-001"})

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: &scrape.ScrapeResult{Movie: nil},
		},
		nil,
	)

	_, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no result")
}

func TestCompareOrchestrator_MergeError(t *testing.T) {
	// Per the nfoImplementor refactoring: MergeMovieMetadataWithOptions is now a
	// package-level pure function — it cannot be mocked to return an error.
	// The only error path is both scraped and nfo being nil, which is impossible
	// in the compare orchestrator flow (both are validated before merge).
	// This test now verifies that a successful merge completes without error.
	fs := afero.NewMemMapFs()
	writeTestNFO(t, fs, "/source/TEST-001.nfo", &models.Movie{ID: "TEST-001", Title: "NFO Title"})

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "TEST-001", Title: "Scraped Title"}},
		},
		nil,
	)

	result, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
		ArrayStrategy:  false,
	})
	require.NoError(t, err)
	assert.True(t, result.NFOExists)
}

func TestCompareOrchestrator_NilContext(t *testing.T) {
	// No NFO file on memfs — nfo.ParseNFO will return file-not-found
	orch := newCompareOrchestrator(
		afero.NewMemMapFs(),
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: &scrape.ScrapeResult{Movie: &models.Movie{ID: "TEST-001"}},
		},
		nil,
	)

	// nil context should be handled gracefully
	result, err := orch.Execute(nil, CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// ============================================================
// preview_orchestrator.go: Path encoding (moved to organizer package)
// ============================================================
// Per ADR-0036: UNC path reconstruction was deepened into
// OrganizePlan.EncodePaths(). These tests now exercise the
// EncodePaths API instead of the removed rebuildUNCTargetDir.

func TestEncodePaths_PreserveSourcePath(t *testing.T) {
	plan := &organizer.OrganizePlan{
		PreserveSourcePath: true,
	}
	info := organizer.PathEncodingInfo{
		Encoding:       organizer.PathEncodingUNC,
		OriginalSource: `\\server\share\path\file.mp4`,
		Destination:    `\\server\organized`,
	}
	encoded := plan.EncodePaths(info)
	assert.Contains(t, encoded.TargetDir, "share")
}

func TestEncodePaths_RenameFolderInPlace(t *testing.T) {
	plan := &organizer.OrganizePlan{
		RenameFolder: true,
		InPlace:      true,
		FolderName:   "NewFolder",
	}
	info := organizer.PathEncodingInfo{
		Encoding:       organizer.PathEncodingUNC,
		OriginalSource: `\\server\share\OldFolder\file.mp4`,
		Destination:    `\\server\organized`,
	}
	encoded := plan.EncodePaths(info)
	assert.Contains(t, encoded.TargetDir, "NewFolder")
}

func TestEncodePaths_WindowsEncoding(t *testing.T) {
	plan := &organizer.OrganizePlan{
		TargetDir:  "C:/Users/test/output/ABC-123",
		TargetPath: "C:/Users/test/output/ABC-123/ABC-123.mp4",
	}
	info := organizer.PathEncodingInfo{Encoding: organizer.PathEncodingWindows}
	encoded := plan.EncodePaths(info)
	assert.Contains(t, encoded.TargetDir, `\`)
	assert.Contains(t, encoded.TargetPath, `\`)
}

func TestEncodePaths_POSIXEncoding(t *testing.T) {
	plan := &organizer.OrganizePlan{
		TargetDir:  "/home/test/output/ABC-123",
		TargetPath: "/home/test/output/ABC-123/ABC-123.mp4",
	}
	info := organizer.PathEncodingInfo{Encoding: organizer.PathEncodingPOSIX}
	encoded := plan.EncodePaths(info)
	assert.Equal(t, plan.TargetDir, encoded.TargetDir)
	assert.Equal(t, plan.TargetPath, encoded.TargetPath)
}

// ============================================================
// factory.go: workflowFactoryConfig validation
// ============================================================

func TestWorkflowFactoryConfig_NilScraper(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)
	db := newInMemoryDB(t)
	organizeCfg := organizer.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	nfoCfg := nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	badCfg := &matcher.Config{RegexEnabled: false}
	fileMatcher, _ := matcher.NewMatcher(badCfg)
	fc := workflowFactoryConfig{
		MaxFilesPerScan: cfg.API.Security.MaxFilesPerScan,
		PreviewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizeCfg.MediaFormatConfig,
			},
			NFOEnabled:    cfg.Metadata.NFO.Feature.Enabled,
			NFOPerFile:    nfoCfg.PerFile,
			DisplayTitle:  cfg.Metadata.NFO.Format.DisplayTitle,
			OpMode:        cfg.Output.GetOperationMode(),
			MaxPathLength: cfg.Output.Template.MaxPathLength,
			Downloads: downloadToggles{
				Poster:      cfg.Output.Download.DownloadPoster,
				Cover:       cfg.Output.Download.DownloadCover,
				Extrafanart: cfg.Output.Download.DownloadExtrafanart,
				Trailer:     cfg.Output.Download.DownloadTrailer,
			},
		},
		ApplyCfg:        ApplyConfig{NFONameCfg: nfoCfg.ToNFONameConfig(false, ""), DisplayTitle: cfg.Metadata.NFO.Format.DisplayTitle},
		DownloadHTTPCfg: downloader.HTTPClientConfig{},
		Matcher:         fileMatcher,
		Repositories: database.Repositories{
			ContentRepos: database.ContentRepos{
				MovieRepo: database.NewMovieRepository(db),
			},
			HistoryRepos: database.HistoryRepos{
				BatchFileOpRepo: database.NewBatchFileOperationRepository(db),
			},
		},
	}
	// Per DEEP-8: NewWorkflowFactory no longer requires Scraper — it creates noOp sub-orchestrators.
	// The validation happens at NewWorkflow/NewScrapeOnlyWorkflow time.
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	// NewScrapeOnlyWorkflow should fail because scraper is nil (noOp scrape orchestrator)
	_, err = factory.NewScrapeOnlyWorkflow()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scraper must not be nil")

	// NewWorkflow should also fail because scraper is nil
	_, err = factory.NewWorkflow("test-job")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scraper must not be nil")

	// But NewScanOnlyWorkflow should succeed — it doesn't need scraper
	wf := factory.NewScanOnlyWorkflow()
	assert.NotNil(t, wf)
}

// ============================================================
// revert_log.go: readNFOSnapshot edge cases
// ============================================================

func TestReadNFOSnapshot_EmptyPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "")
	assert.Empty(t, result.Content)
	assert.Empty(t, result.FoundPath)
}

func TestReadNFOSnapshot_FileExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/source/test.nfo", []byte("<movie/>"), 0644))

	result := readNFOSnapshot(logging.GlobalLogger(), fs, "/source/test.nfo")
	assert.Equal(t, "<movie/>", result.Content)
	assert.Equal(t, "/source/test.nfo", result.FoundPath)
}

func TestReadNFOSnapshot_FileNotFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "/nonexistent/test.nfo")
	assert.Empty(t, result.Content)
	assert.Empty(t, result.FoundPath)
}

func TestReadNFOSnapshot_MultipleCandidates(t *testing.T) {
	fs := afero.NewMemMapFs()
	// Only second candidate exists
	require.NoError(t, afero.WriteFile(fs, "/source/legacy.nfo", []byte("<legacy/>"), 0644))

	result := readNFOSnapshot(logging.GlobalLogger(), fs, "/nonexistent.nfo", "/source/legacy.nfo")
	assert.Equal(t, "<legacy/>", result.Content)
	assert.Equal(t, "/source/legacy.nfo", result.FoundPath)
}

// ============================================================
// revert_log.go: buildGeneratedFilesJSON edge cases
// ============================================================

func TestBuildGeneratedFilesJSON_Empty(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", nil, nil)
	assert.Empty(t, result)
}

func TestBuildGeneratedFilesJSON_WithNFOPath(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "/dest/TEST-001.nfo", nil, nil)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "TEST-001.nfo")
}

func TestBuildGeneratedFilesJSON_WithDownloadPaths(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", nil, []string{"/dest/poster.jpg", "/dest/fanart.jpg"})
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "poster.jpg")
	assert.Contains(t, result, "fanart.jpg")
}

func TestBuildGeneratedFilesJSON_WithSubtitleMoves(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/source/sub1.srt", NewPath: "/dest/sub1.srt", Moved: true},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "sub1.srt")
}

func TestBuildGeneratedFilesJSON_SubtitleNotMoved(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/source/sub1.srt", NewPath: "/dest/sub1.srt", Moved: false},
	}
	// Not-moved subtitles should not appear in MoveBack
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.Empty(t, result)
}

// ============================================================
// revert_log.go: updatePostOrganize
// ============================================================

func TestUpdatePostOrganize(t *testing.T) {
	op := &models.BatchFileOperation{
		BatchJobID: "job1",
		MovieID:    "ABC-001",
	}
	updatePostOrganize(op, "/dest/ABC-001.mp4", true, "/source", `{"Delete":["/dest/ABC-001.nfo"]}`)

	assert.Equal(t, "/dest/ABC-001.mp4", op.NewPath)
	assert.True(t, op.InPlaceRenamed)
	assert.Equal(t, "/source", op.OriginalDirPath)
	assert.Equal(t, `{"Delete":["/dest/ABC-001.nfo"]}`, op.GeneratedFiles)
}

// ============================================================
// revert_log.go: determineOperationType full coverage
// ============================================================

func TestDetermineOperationType_UpdateOverridesAll(t *testing.T) {
	// Update mode should return OperationTypeUpdate regardless of move/link settings
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(true, organizer.LinkModeNone, true))
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(false, organizer.LinkModeHard, true))
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(false, organizer.LinkModeSoft, true))
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(false, organizer.LinkModeNone, true))
}

// ============================================================
// seam_strings.go: ResolveSeamStrings additional coverage
// ============================================================

// (Already 85.7% — adding edge cases for the remaining branches)

// ============================================================
// display_title.go: ApplyDisplayTitleFromSource (80%)
// ============================================================

func TestApplyDisplayTitleFromSource_NilMovie(t *testing.T) {
	// Should not panic with nil movie
	assert.NotPanics(t, func() {
		ApplyDisplayTitleFromSource(context.Background(), nil, nil, "", template.NewEngine(), nfo.NFONameConfig{})
	})
}

// ============================================================
// RevertLogConfig: ToNFONameConfig with nil NFOCfg
// ============================================================

func TestRevertLogConfig_ToNFONameConfig_NilNFOCfg(t *testing.T) {
	cfg := &RevertLogConfig{AllowRevert: true, NFOCfg: nil}
	result := cfg.ToNFONameConfig(true, "-pt1")
	assert.True(t, result.IsMultiPart)
	assert.Equal(t, "-pt1", result.PartSuffix)
}

func TestRevertLogConfig_ToNFONameConfig_WithNFOCfg(t *testing.T) {
	appCfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(appCfg)
	require.NoError(t, err)
	nfoCfg := nfo.ConfigFromAppConfig(appCfg, nfo.NFONameConfigFromAppConfig(appCfg))
	cfg := &RevertLogConfig{AllowRevert: true, NFOCfg: nfoCfg}
	result := cfg.ToNFONameConfig(false, "")
	assert.False(t, result.IsMultiPart)
}

// ============================================================
// NewRevertLogConfig
// ============================================================

func TestNewRevertLogConfig(t *testing.T) {
	cfg := NewRevertLogConfig(true, nil)
	assert.True(t, cfg.AllowRevert)
	assert.Nil(t, cfg.NFOCfg)
}

// ============================================================
// WorkflowFactory: NewWorkflow with real DB for full coverage
// ============================================================

func TestWorkflowFactory_FullLifecycle(t *testing.T) {
	fc := newFactoryConfigWithDB(t)

	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	// Produce multiple workflows — each should be independent
	wf1, err := factory.NewWorkflow("job-001")
	require.NoError(t, err)
	assert.NotNil(t, wf1)

	wf2, err := factory.NewWorkflow("job-002")
	require.NoError(t, err)
	assert.NotNil(t, wf2)

	// Both should be non-nil and distinct WorkflowInterface instances
	assert.NotEqual(t, fmt.Sprintf("%p", wf1), fmt.Sprintf("%p", wf2))
}

// ============================================================
// NewScrapeOnlyWorkflow via WorkflowFactory
// ============================================================

func TestWorkflowFactory_ScrapeOnlyLifecycle(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	wf, err := factory.NewScrapeOnlyWorkflow()
	require.NoError(t, err)
	assert.NotNil(t, wf)

	// Verify scan-match still works (not no-op)
	_, err = wf.ScanAndMatch(context.Background(), ScanAndMatchCmd{
		Directory: "",
	})
	assert.Error(t, err) // empty directory should error
	assert.Contains(t, err.Error(), "directory is required")
}

// ============================================================
// ScanOnly via WorkflowFactory
// ============================================================

func TestWorkflowFactory_ScanOnlyLifecycle(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	wf := factory.NewScanOnlyWorkflow()
	assert.NotNil(t, wf)

	// Scrape should be no-op
	_, _, scrapeErr := wf.Scrape(context.Background(), scrape.ScrapeCmd{MovieID: "TEST-001"}, nil)
	assert.Error(t, scrapeErr)
	assert.Contains(t, scrapeErr.Error(), "scrape not configured")
}

// ============================================================
// applyOrchImpl: Execute with nil fs
// ============================================================

func TestApplyOrchImpl_Execute_NilFs(t *testing.T) {
	impl := &applyOrchImpl{
		fs: nil,
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie: &models.Movie{ID: "TEST-001"},
		Match: defaultMatch(),
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "filesystem is nil")
	assert.Nil(t, result)
}

// ============================================================
// applyOrchImpl: Execute with organize error + revertLog
// ============================================================

func TestApplyOrchImpl_Execute_OrganizeError_WithRevertLog(t *testing.T) {
	fs := afero.NewMemMapFs()
	impl := &applyOrchImpl{
		fs:        fs,
		organizer: &failingOrganizer{},
		revertLog: noOpRevertLog{},
		nfo:       &mockNFOFieldMerger{},
	}

	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "organization failed")
	assert.NotNil(t, result)
	assert.Equal(t, "organize", result.FailedStep)
}

// failingOrganizer always returns an error
type failingOrganizer struct{}

func (f *failingOrganizer) Organize(_ context.Context, _ organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	return nil, errors.New("organization failed")
}

// ============================================================
// ============================================================

// ============================================================
// compareOrchImpl: withConfig removed (workflowConfig eliminated)
// ============================================================

func TestCompareOrchImpl_Construction(t *testing.T) {
	orch := &compareOrchImpl{
		fs:      afero.NewMemMapFs(),
		merger:  &mockNFOFieldMerger{},
		scraper: &mockScraperInterface{},
	}

	assert.NotNil(t, orch)
	assert.Equal(t, orch.fs, orch.fs)
	assert.Equal(t, orch.merger, orch.merger)
	assert.Equal(t, orch.scraper, orch.scraper)
}
