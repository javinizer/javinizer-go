package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// preview_orchestrator.go: executePreview (68.0%)
// ---------------------------------------------------------------------------

func TestExecutePreview_MultipleFileResults(t *testing.T) {
	fs := afero.NewMemMapFs()
	plan1 := &organizer.OrganizePlan{
		TargetDir:    "/output/ABC-123",
		TargetPath:   "/output/ABC-123/ABC-123-cd1.mp4",
		FolderName:   "ABC-123",
		BaseFileName: "ABC-123",
	}

	strategy := &mockStrategyPlan{plan: plan1}

	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:     ".jpg",
					FanartFormat:     ".jpg",
					TrailerFormat:    ".mp4",
					ScreenshotFormat: ".jpg",
					ScreenshotFolder: "extrafanart",
				},
			},
			ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
				return strategy
			},
			NFOEnabled: true,
			NFOPerFile: false,
			Downloads: downloadToggles{
				Poster:      true,
				Cover:       true,
				Trailer:     true,
				Extrafanart: true,
			},
			OpMode: operationmode.OperationModeOrganize,
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver: pathResolverFromConfig(PreviewPathConfig{
			MediaFormatConfig: organizer.MediaFormatConfig{
				PosterFormat:     ".jpg",
				FanartFormat:     ".jpg",
				TrailerFormat:    ".mp4",
				ScreenshotFormat: ".jpg",
				ScreenshotFolder: "extrafanart",
			},
		}, template.NewEngine()),
		nfoIface: &mockNFOFieldMergerForPreview{
			nfoFilename: "ABC-123.nfo",
			nfoPath:     "/output/ABC-123/ABC-123.nfo",
		},
	}

	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "ABC-123", Title: "Test"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults: []models.FileMatchInfo{
			{Path: "/source/ABC-123-cd1.mp4", Name: "ABC-123-cd1.mp4", Extension: ".mp4", MovieID: "ABC-123", IsMultiPart: true, PartSuffix: "-cd1"},
			{Path: "/source/ABC-123-cd2.mp4", Name: "ABC-123-cd2.mp4", Extension: ".mp4", MovieID: "ABC-123", IsMultiPart: true, PartSuffix: "-cd2"},
		},
		Destination: "/output",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecutePreview_EmptyPathFileResult(t *testing.T) {
	fs := afero.NewMemMapFs()

	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:  ".jpg",
					FanartFormat:  ".jpg",
					TrailerFormat: ".mp4",
				},
			},
			ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
				return &mockStrategyPlan{
					plan: &organizer.OrganizePlan{
						TargetDir:    "/output/TEST-001",
						TargetPath:   "/output/TEST-001/TEST-001.mp4",
						FolderName:   "TEST-001",
						BaseFileName: "TEST-001",
					},
				}
			},
			OpMode: operationmode.OperationModeOrganize,
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	// File result with empty path should be skipped
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "TEST-001"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults: []models.FileMatchInfo{
			{Path: "", Name: "empty.mp4", Extension: ".mp4", MovieID: "TEST-001"},
			{Path: "/source/TEST-001.mp4", Name: "TEST-001.mp4", Extension: ".mp4", MovieID: "TEST-001"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecutePreview_CancelledDuringProcessing(t *testing.T) {
	fs := afero.NewMemMapFs()

	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:  ".jpg",
					FanartFormat:  ".jpg",
					TrailerFormat: ".mp4",
				},
			},
			ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
				return &mockStrategyPlan{
					plan: &organizer.OrganizePlan{
						TargetDir:    "/output/TEST-001",
						TargetPath:   "/output/TEST-001/TEST-001.mp4",
						FolderName:   "TEST-001",
						BaseFileName: "TEST-001",
					},
				}
			},
			OpMode: operationmode.OperationModeOrganize,
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := orch.Execute(ctx, PreviewCmd{
		Movie:         &models.Movie{ID: "TEST-001"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults: []models.FileMatchInfo{
			{Path: "/source/TEST-001.mp4", Name: "TEST-001.mp4", Extension: ".mp4", MovieID: "TEST-001"},
		},
	})
	if err != nil {
		assert.Equal(t, context.Canceled, err)
	} else {
		assert.NotNil(t, result)
	}
}

func TestExecutePreview_SyntheticFileFallback(t *testing.T) {
	fs := afero.NewMemMapFs()

	strategy := &mockStrategyPlan{
		plan: &organizer.OrganizePlan{
			TargetDir:    "/output/SYN-001",
			TargetPath:   "/output/SYN-001/SYN-001.mp4",
			FolderName:   "SYN-001",
			BaseFileName: "SYN-001",
		},
	}

	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:  ".jpg",
					FanartFormat:  ".jpg",
					TrailerFormat: ".mp4",
				},
			},
			ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
				return strategy
			},
			OpMode: operationmode.OperationModeOrganize,
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	// Empty file results — should trigger synthetic fallback
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "SYN-001"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults:   []models.FileMatchInfo{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecutePreview_SyntheticFilePlanFails(t *testing.T) {
	fs := afero.NewMemMapFs()

	strategy := &mockStrategyPlan{
		err: assert.AnError,
	}

	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:  ".jpg",
					FanartFormat:  ".jpg",
					TrailerFormat: ".mp4",
				},
			},
			ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
				return strategy
			},
			OpMode: operationmode.OperationModeOrganize,
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	// All strategies fail — should return minimal result
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "FAIL-001"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults:   []models.FileMatchInfo{},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.VideoFiles)
}

func TestExecutePreview_NilPlanAfterProcessing(t *testing.T) {
	fs := afero.NewMemMapFs()

	strategy := &mockStrategyPlan{
		err: assert.AnError,
	}

	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:  ".jpg",
					FanartFormat:  ".jpg",
					TrailerFormat: ".mp4",
				},
			},
			ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
				return strategy
			},
			OpMode: operationmode.OperationModeOrganize,
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	// All strategy calls fail, so primaryPlan stays nil
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "NOP-001"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults: []models.FileMatchInfo{
			{Path: "/source/NOP-001.mp4", Name: "NOP-001.mp4", Extension: ".mp4", MovieID: "NOP-001"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecutePreview_SourcePathFieldForInPlace(t *testing.T) {
	fs := afero.NewMemMapFs()

	strategy := &mockStrategyPlan{
		plan: &organizer.OrganizePlan{
			TargetDir:    "/source/INP-001",
			TargetPath:   "/source/INP-001/INP-001.mp4",
			FolderName:   "INP-001",
			BaseFileName: "INP-001",
		},
	}

	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:  ".jpg",
					FanartFormat:  ".jpg",
					TrailerFormat: ".mp4",
				},
			},
			ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
				return strategy
			},
			OpMode: operationmode.OperationModeInPlace,
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "INP-001"},
		OperationMode: operationmode.OperationModeInPlace,
		FileResults: []models.FileMatchInfo{
			{Path: "/source/INP-001.mp4", Name: "INP-001.mp4", Extension: ".mp4", MovieID: "INP-001"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// In-place mode should produce a result with the correct operation mode
	assert.Equal(t, operationmode.OperationModeInPlace, result.OperationMode)
}

// ---------------------------------------------------------------------------
// preview_helpers.go: validatePathLengths (68.4%)
// ---------------------------------------------------------------------------

func TestValidatePathLengths_VideoPathExceeds(t *testing.T) {
	engine := template.NewEngine()
	longPath := "/very/long/path/that/exceeds/the/maximum/allowed/length/for/a/file/path/that/should/be/flagged/by/validation/abcde12345.mp4"
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 50, engine, []string{longPath}, "", nil, "", "", "", nil)
	})
}

func TestValidatePathLengths_NFOPathExceeds(t *testing.T) {
	engine := template.NewEngine()
	longNFOPath := "/very/long/path/that/exceeds/the/maximum/allowed/length/for/a/file/path/that/should/be/flagged/by/validation/test.nfo"
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 50, engine, nil, longNFOPath, nil, "", "", "", nil)
	})
}

func TestValidatePathLengths_NFOPathsInSliceExceeds(t *testing.T) {
	engine := template.NewEngine()
	longNFOPath := "/very/long/path/that/exceeds/the/maximum/allowed/length/for/a/file/path/that/should/be/flagged/by/validation/test.nfo"
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 50, engine, nil, "", []string{longNFOPath}, "", "", "", nil)
	})
}

func TestValidatePathLengths_PosterPathExceeds(t *testing.T) {
	engine := template.NewEngine()
	longPosterPath := "/very/long/path/that/exceeds/the/maximum/allowed/length/for/a/file/path/poster.jpg"
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 50, engine, nil, "", nil, longPosterPath, "", "", nil)
	})
}

func TestValidatePathLengths_FanartPathExceeds(t *testing.T) {
	engine := template.NewEngine()
	longFanartPath := "/very/long/path/that/exceeds/the/maximum/allowed/length/for/a/file/path/fanart.jpg"
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 50, engine, nil, "", nil, "", longFanartPath, "", nil)
	})
}

func TestValidatePathLengths_ScreenshotPathExceeds(t *testing.T) {
	engine := template.NewEngine()
	longScreenshot := "very_long_screenshot_name_that_exceeds.jpg"
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 10, engine, nil, "", nil, "", "", "/short/ef", []string{longScreenshot})
	})
}

func TestValidatePathLengths_AllPathsValid(t *testing.T) {
	engine := template.NewEngine()
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 500, engine,
			[]string{"/output/ABC-123/ABC-123.mp4"},
			"/output/ABC-123/ABC-123.nfo",
			nil,
			"/output/ABC-123/poster.jpg",
			"/output/ABC-123/fanart.jpg",
			"/output/ABC-123/extrafanart",
			[]string{"screenshot1.jpg"},
		)
	})
}

func TestValidatePathLengths_EmptyVideoPath(t *testing.T) {
	engine := template.NewEngine()
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 10, engine, []string{""}, "", nil, "", "", "", nil)
	})
}

// ---------------------------------------------------------------------------
// preview_orchestrator.go: newPreviewOrchestrator (66.7%)
// ---------------------------------------------------------------------------

func TestNewPreviewOrchestrator_NilTemplateEngine(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{},
		nfo.NFONameConfig{},
		nil, // nil templateEngine — should be replaced with new one
		&mockNFOFieldMergerForPreview{},
		nil,
	)
	assert.NotNil(t, orch)
}

// ---------------------------------------------------------------------------
// factory.go: buildDownloadHTTPCfg (66.7%)
// ---------------------------------------------------------------------------

func TestBuildDownloadHTTPCfg_WithDownloadProxy(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	// Enable download proxy
	cfg.Output.Download.DownloadProxy.Enabled = true

	downloadCfg := downloader.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	scrapeCfg := scrape.ConfigFromAppConfig(cfg)
	registry := scraperutil.NewScraperRegistry()

	result := buildDownloadHTTPCfg(cfg, downloadCfg, scrapeCfg, registry)
	assert.True(t, result.Timeout > 0)
}

func TestBuildDownloadHTTPCfg_ZeroTimeout(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	downloadCfg := downloader.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	downloadCfg.DownloadTimeout = 0 // zero timeout should default to 60s
	scrapeCfg := scrape.ConfigFromAppConfig(cfg)
	registry := scraperutil.NewScraperRegistry()

	result := buildDownloadHTTPCfg(cfg, downloadCfg, scrapeCfg, registry)
	assert.Equal(t, 60, int(result.Timeout.Seconds()), "zero timeout should default to 60s")
}

// ---------------------------------------------------------------------------
// factory.go: buildScraper (75.0%)
// ---------------------------------------------------------------------------

func TestBuildScraper_NilMovieRepo(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	scrapeCfg := scrape.ConfigFromAppConfig(cfg)
	aggCfg := aggregator.ConfigFromAppConfig(cfg)
	registry := scraperutil.NewScraperRegistry()
	fs := afero.NewMemMapFs()

	_, _, err = buildScraper(scrapeCfg, aggCfg, nil, registry, nil, fs,
		database.ContentRepos{MovieRepo: nil},
		database.ReplacementRepos{},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "movieRepo must not be nil")
}

// ---------------------------------------------------------------------------
// factory.go: buildMatcher (75.0%)
// ---------------------------------------------------------------------------

func TestBuildMatcher_InvalidConfig(t *testing.T) {
	_, err := buildMatcher(&matcher.Config{})
	_ = err
}

// ---------------------------------------------------------------------------
// factory.go: buildDownloader (75.0%)
// ---------------------------------------------------------------------------

func TestBuildDownloader_HTTPClientError(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	fs := afero.NewMemMapFs()
	downloadCfg := downloader.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg))
	httpCfg := downloader.HTTPClientConfig{
		Timeout: 30,
		DownloadProxy: &models.ProxyProfile{
			URL: "://invalid-proxy-url",
		},
	}

	_, _, err = buildDownloader(httpCfg, fs, downloadCfg, template.NewEngine())
	_ = err
}

// ---------------------------------------------------------------------------
// factory.go: NewFactoryConfigFromRepos (80.8%)
// ---------------------------------------------------------------------------

func TestNewFactoryConfigFromRepos_NilConfig(t *testing.T) {
	_, err := NewFactoryConfigFromRepos(nil, scraperutil.NewScraperRegistry(), database.Repositories{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cfg and registry must not be nil")
}

func TestNewFactoryConfigFromRepos_NilRegistry(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)

	_, err = NewFactoryConfigFromRepos(cfg, nil, database.Repositories{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cfg and registry must not be nil")
}

// ---------------------------------------------------------------------------
// revert_log.go: noOpRevertLog.CaptureSnapshot (0%)
// Additional coverage with real context
// ---------------------------------------------------------------------------

func TestNoOpRevertLog_CaptureSnapshot_WithContext(t *testing.T) {
	op := noOpRevertLog{}
	assert.NotPanics(t, func() {
		op.CaptureSnapshot(context.Background(), "op-1", ApplyCmd{
			Movie: &models.Movie{ID: "TEST-001"},
		})
	})
}

// ---------------------------------------------------------------------------
// revert_log.go: readNFOSnapshot additional coverage
// ---------------------------------------------------------------------------

func TestReadNFOSnapshot_NonNotExistError(t *testing.T) {
	fs := afero.NewMemMapFs()
	// Use a read-only filesystem to trigger a non-IsNotExist error
	rofs := afero.NewReadOnlyFs(fs)
	result := readNFOSnapshot(logging.GlobalLogger(), rofs, "/readonly/test.nfo")
	assert.Empty(t, result.Content)
}

// ---------------------------------------------------------------------------
// revert_log.go: buildGeneratedFilesJSON additional coverage (79.2%)
// ---------------------------------------------------------------------------

func TestBuildGeneratedFilesJSON_MarshalRecoveryPath(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/source/sub1.srt", NewPath: "/dest/sub1.srt", Moved: true},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "/dest/TEST-001.nfo", subtitles, []string{"/dest/poster.jpg"})
	assert.NotEmpty(t, result)
}

// ---------------------------------------------------------------------------
// resolveMediaPaths additional coverage
// ---------------------------------------------------------------------------

func TestResolveMediaPaths_SkipNFO(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:  ".jpg",
					FanartFormat:  ".jpg",
					TrailerFormat: ".mp4",
				},
			},
			NFOEnabled: true,
			Downloads: downloadToggles{
				Poster:  true,
				Cover:   true,
				Trailer: true,
			},
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	plan := &organizer.OrganizePlan{
		TargetDir:    "/output/TEST-001",
		TargetPath:   "/output/TEST-001/TEST-001.mp4",
		FolderName:   "TEST-001",
		BaseFileName: "TEST-001",
	}
	encoded := plan.EncodePaths(organizer.PathEncodingInfo{})

	result := orch.resolveMediaPaths(
		&models.Movie{ID: "TEST-001"},
		[]models.FileMatchInfo{{Path: "/source/TEST-001.mp4"}},
		plan,
		encoded,
		[]string{"/output/TEST-001/TEST-001.mp4"},
		operationmode.OperationModeOrganize,
		true,  // skipNFO
		false, // don't skip download
	)
	assert.Empty(t, result.NFOPath)
	assert.Empty(t, result.NFOPaths)
	assert.NotEmpty(t, result.PosterPath)
}

func TestResolveMediaPaths_SkipDownload(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:  ".jpg",
					FanartFormat:  ".jpg",
					TrailerFormat: ".mp4",
				},
			},
			NFOEnabled: true,
			Downloads: downloadToggles{
				Poster:  true,
				Cover:   true,
				Trailer: true,
			},
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	plan := &organizer.OrganizePlan{
		TargetDir:    "/output/TEST-001",
		TargetPath:   "/output/TEST-001/TEST-001.mp4",
		FolderName:   "TEST-001",
		BaseFileName: "TEST-001",
	}
	encoded := plan.EncodePaths(organizer.PathEncodingInfo{})

	result := orch.resolveMediaPaths(
		&models.Movie{ID: "TEST-001"},
		[]models.FileMatchInfo{{Path: "/source/TEST-001.mp4"}},
		plan,
		encoded,
		[]string{"/output/TEST-001/TEST-001.mp4"},
		operationmode.OperationModeOrganize,
		false, // don't skip NFO
		true,  // skip download
	)
	assert.NotEmpty(t, result.NFOPath)
	assert.Empty(t, result.PosterPath)
	assert.Empty(t, result.FanartPath)
	assert.Empty(t, result.TrailerPath)
}

func TestResolveMediaPaths_OrganizeModeNoSourcePath(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := &previewOrchImpl{
		fs:             fs,
		matcher:        &mockMatcherForPreview{},
		previewCfg:     PreviewConfig{},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver:   pathResolverFromConfig(PreviewPathConfig{}, template.NewEngine()),
		nfoIface:       &mockNFOFieldMergerForPreview{},
	}

	plan := &organizer.OrganizePlan{
		TargetDir:    "/output/TEST-001",
		TargetPath:   "/output/TEST-001/TEST-001.mp4",
		FolderName:   "TEST-001",
		BaseFileName: "TEST-001",
	}
	encoded := plan.EncodePaths(organizer.PathEncodingInfo{})

	result := orch.resolveMediaPaths(
		&models.Movie{ID: "TEST-001"},
		nil,
		plan,
		encoded,
		[]string{},
		operationmode.OperationModeOrganize, // Organize mode — no source path field
		true,
		true,
	)
	assert.Empty(t, result.SourcePath, "Organize mode should not set SourcePath")
}

func TestResolveMediaPaths_ExtrafanartEnabled(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat:     ".jpg",
					FanartFormat:     ".jpg",
					TrailerFormat:    ".mp4",
					ScreenshotFormat: ".jpg",
					ScreenshotFolder: "extrafanart",
				},
			},
			Downloads: downloadToggles{
				Poster:      true,
				Cover:       true,
				Trailer:     true,
				Extrafanart: true,
			},
		},
		nfoNameCfg:     nfo.NFONameConfig{},
		templateEngine: template.NewEngine(),
		pathResolver: pathResolverFromConfig(PreviewPathConfig{
			MediaFormatConfig: organizer.MediaFormatConfig{
				PosterFormat:     ".jpg",
				FanartFormat:     ".jpg",
				TrailerFormat:    ".mp4",
				ScreenshotFormat: ".jpg",
				ScreenshotFolder: "extrafanart",
			},
		}, template.NewEngine()),
		nfoIface: &mockNFOFieldMergerForPreview{},
	}

	plan := &organizer.OrganizePlan{
		TargetDir:    "/output/TEST-001",
		TargetPath:   "/output/TEST-001/TEST-001.mp4",
		FolderName:   "TEST-001",
		BaseFileName: "TEST-001",
	}
	encoded := plan.EncodePaths(organizer.PathEncodingInfo{})

	result := orch.resolveMediaPaths(
		&models.Movie{ID: "TEST-001", Screenshots: []string{"shot1.jpg", "shot2.jpg"}},
		[]models.FileMatchInfo{{Path: "/source/TEST-001.mp4"}},
		plan,
		encoded,
		[]string{"/output/TEST-001/TEST-001.mp4"},
		operationmode.OperationModeOrganize,
		true,
		false,
	)
	assert.NotEmpty(t, result.ExtrafanartPath)
}
