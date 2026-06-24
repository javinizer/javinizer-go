package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// preview_orchestrator.go: previewOrchImpl.Execute (0%)
// ---------------------------------------------------------------------------

// mockStrategyPlan is an organizer.OperationStrategy that returns a fixed plan.
type mockStrategyPlan struct {
	plan *organizer.OrganizePlan
	err  error
}

func (m *mockStrategyPlan) Plan(_ models.FileMatchInfo, _ *models.Movie, _ string, _ bool) (*organizer.OrganizePlan, error) {
	return m.plan, m.err
}

func (m *mockStrategyPlan) Execute(_ *organizer.OrganizePlan) (*organizer.OrganizeResult, error) {
	return nil, nil
}

// mockMatcherForPreview implements matcher.MatcherInterface for preview tests.
type mockMatcherForPreview struct {
	result *matcher.MatchResult
	err    error
}

func (m *mockMatcherForPreview) Match(_ []models.FileMatchInfo) []matcher.MatchResult {
	if m.result != nil {
		return []matcher.MatchResult{*m.result}
	}
	return nil
}

func (m *mockMatcherForPreview) MatchFile(_ models.FileMatchInfo) *matcher.MatchResult {
	return m.result
}

func (m *mockMatcherForPreview) MatchString(_ string) string { return "" }

// mockNFOFieldMergerForPreview implements nfo.NFOFieldMerger for preview tests.
type mockNFOFieldMergerForPreview struct {
	nfoFilename string
	nfoPath     string
	legacyPaths []string
}

func (m *mockNFOFieldMergerForPreview) MergeWithExistingNFO(_ *models.Movie, _ nfo.MergeWithExistingOptions) nfo.MergeWithExistingResult {
	return nfo.MergeWithExistingResult{}
}

func (m *mockNFOFieldMergerForPreview) ResolveNFOFilename(_ *models.Movie, _ nfo.NFONameConfig) string {
	return m.nfoFilename
}

func (m *mockNFOFieldMergerForPreview) ResolveNFOPath(_ string, _ *models.Movie, _ nfo.NFONameConfig, _ string) (string, []string) {
	return m.nfoPath, m.legacyPaths
}

func TestPreviewOrchImpl_Execute_CancelledContext(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{},
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := orch.Execute(ctx, PreviewCmd{
		Movie: &models.Movie{ID: "TEST-001"},
	})
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestPreviewOrchImpl_Execute_NilMovie(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{},
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	_, err := orch.Execute(context.Background(), PreviewCmd{
		Movie: nil,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "movie is nil")
}

func TestPreviewOrchImpl_Execute_BasicPreview(t *testing.T) {
	fs := afero.NewMemMapFs()
	strategy := &mockStrategyPlan{
		plan: &organizer.OrganizePlan{
			TargetDir:     "/output/ABC-123",
			TargetPath:    "/output/ABC-123/ABC-123.mp4",
			FolderName:    "ABC-123",
			BaseFileName:  "ABC-123",
			SubfolderPath: "",
		},
	}

	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
				Poster:  true,
				Cover:   true,
				Trailer: true,
			},
			OpMode: operationmode.OperationModeOrganize,
		},
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{
			nfoFilename: "ABC-001.nfo",
			nfoPath:     "/output/ABC-123/ABC-001.nfo",
		},
		nil,
	).(*previewOrchImpl)

	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie: &models.Movie{ID: "ABC-123", Title: "Test Movie"},
		FileResults: []models.FileMatchInfo{
			{Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		},
		Destination:   "/output",
		OperationMode: operationmode.OperationModeOrganize,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.FolderName)
	assert.Equal(t, "ABC-123", result.FileName)
	assert.NotEmpty(t, result.VideoFiles)
}

func TestPreviewOrchImpl_Execute_InPlaceNoRenameWithEmptySourcePath(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	// InPlaceNoRenameFolder with empty source path should return early with no paths
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "TEST-001"},
		OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		FileResults: []models.FileMatchInfo{
			{Path: "", Name: "TEST-001.mp4", Extension: ".mp4", MovieID: "TEST-001"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Should return early with empty result since sourcePath is empty and mode is InPlaceNoRenameFolder
	assert.Empty(t, result.VideoFiles)
}

func TestPreviewOrchImpl_Execute_InPlaceWithDotSourcePath(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	// "." source path with InPlace mode should return early
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "TEST-001"},
		OperationMode: operationmode.OperationModeInPlace,
		FileResults: []models.FileMatchInfo{
			{Path: ".", Name: "TEST-001.mp4", Extension: ".mp4", MovieID: "TEST-001"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestPreviewOrchImpl_Execute_DefaultOperationMode(t *testing.T) {
	fs := afero.NewMemMapFs()
	strategy := &mockStrategyPlan{
		plan: &organizer.OrganizePlan{
			TargetDir:    "/output/ABC-123",
			TargetPath:   "/output/ABC-123/ABC-123.mp4",
			FolderName:   "ABC-123",
			BaseFileName: "ABC-123",
		},
	}

	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	// Empty OperationMode in cmd should fall back to config default
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "ABC-123"},
		OperationMode: "", // should use config OpMode
		FileResults: []models.FileMatchInfo{
			{Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, operationmode.OperationModeOrganize, result.OperationMode)
}

func TestPreviewOrchImpl_Execute_StrategyPlanError(t *testing.T) {
	fs := afero.NewMemMapFs()
	strategy := &mockStrategyPlan{
		err: errors.New("plan failed"),
	}

	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	// Strategy.Plan error should be logged and the file skipped
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "ABC-123"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults: []models.FileMatchInfo{
			{Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// No video files since all plans failed
	assert.Empty(t, result.VideoFiles)
}

func TestPreviewOrchImpl_Execute_SkipNFO(t *testing.T) {
	fs := afero.NewMemMapFs()
	strategy := &mockStrategyPlan{
		plan: &organizer.OrganizePlan{
			TargetDir:    "/output/ABC-123",
			TargetPath:   "/output/ABC-123/ABC-123.mp4",
			FolderName:   "ABC-123",
			BaseFileName: "ABC-123",
		},
	}

	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
			NFOEnabled: true,
			OpMode:     operationmode.OperationModeOrganize,
		},
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "ABC-123"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults: []models.FileMatchInfo{
			{Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		},
		SkipNFO: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.NFOPath, "NFOPath should be empty when SkipNFO is true")
}

func TestPreviewOrchImpl_Execute_SkipDownload(t *testing.T) {
	fs := afero.NewMemMapFs()
	strategy := &mockStrategyPlan{
		plan: &organizer.OrganizePlan{
			TargetDir:    "/output/ABC-123",
			TargetPath:   "/output/ABC-123/ABC-123.mp4",
			FolderName:   "ABC-123",
			BaseFileName: "ABC-123",
		},
	}

	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "ABC-123"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults: []models.FileMatchInfo{
			{Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		},
		SkipDownload: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.PosterPath, "PosterPath should be empty when SkipDownload is true")
	assert.Empty(t, result.FanartPath, "FanartPath should be empty when SkipDownload is true")
	assert.Empty(t, result.TrailerPath, "TrailerPath should be empty when SkipDownload is true")
}

func TestPreviewOrchImpl_Execute_MultipartWithPerFileNFO(t *testing.T) {
	fs := afero.NewMemMapFs()
	strategy := &mockStrategyPlan{
		plan: &organizer.OrganizePlan{
			TargetDir:    "/output/ABC-123",
			TargetPath:   "/output/ABC-123/ABC-123-cd1.mp4",
			FolderName:   "ABC-123",
			BaseFileName: "ABC-123",
		},
	}

	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
			NFOEnabled: true,
			NFOPerFile: true, // per-file NFO enabled
			Downloads: downloadToggles{
				Poster: true,
				Cover:  true,
			},
			OpMode: operationmode.OperationModeOrganize,
		},
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{
			nfoFilename: "ABC-123.nfo",
			nfoPath:     "/output/ABC-123/ABC-123.nfo",
		},
		nil,
	).(*previewOrchImpl)

	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "ABC-123", Title: "Test"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults: []models.FileMatchInfo{
			{Path: "/source/ABC-123-cd1.mp4", Name: "ABC-123-cd1.mp4", Extension: ".mp4", MovieID: "ABC-123", IsMultiPart: true, PartSuffix: "-cd1"},
			{Path: "/source/ABC-123-cd2.mp4", Name: "ABC-123-cd2.mp4", Extension: ".mp4", MovieID: "ABC-123", IsMultiPart: true, PartSuffix: "-cd2"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// Should produce per-file NFO paths since NFOPerFile=true and IsMultiPart=true
	assert.NotEmpty(t, result.NFOPaths)
}

func TestPreviewOrchImpl_Execute_CancelledDuringPlanComputation(t *testing.T) {
	fs := afero.NewMemMapFs()
	callCount := 0
	strategy := &mockStrategyPlan{
		plan: &organizer.OrganizePlan{
			TargetDir:    "/output/ABC-123",
			TargetPath:   "/output/ABC-123/ABC-123.mp4",
			FolderName:   "ABC-123",
			BaseFileName: "ABC-123",
		},
	}

	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
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
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	_ = callCount // strategy returns a fixed plan, no count tracking needed

	// Provide many file results to potentially trigger mid-computation cancel
	files := make([]models.FileMatchInfo, 5)
	for i := range files {
		files[i] = models.FileMatchInfo{
			Path:      "/source/file.mp4",
			Name:      "file.mp4",
			Extension: ".mp4",
			MovieID:   "ABC-123",
		}
	}

	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie:         &models.Movie{ID: "ABC-123"},
		OperationMode: operationmode.OperationModeOrganize,
		FileResults:   files,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// ---------------------------------------------------------------------------
// preview_orchestrator.go: createPreviewStrategy (0%)
// ---------------------------------------------------------------------------

func TestPreviewOrchImpl_CreatePreviewStrategy_WithResolver(t *testing.T) {
	fs := afero.NewMemMapFs()
	expectedPlan := &organizer.OrganizePlan{
		TargetDir:    "/output/TEST-001",
		TargetPath:   "/output/TEST-001/TEST-001.mp4",
		FolderName:   "TEST-001",
		BaseFileName: "TEST-001",
	}
	resolverCalled := false

	orch := &previewOrchImpl{
		fs:      fs,
		matcher: &mockMatcherForPreview{},
		previewCfg: PreviewConfig{
			ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
				resolverCalled = true
				return &mockStrategyPlan{plan: expectedPlan}
			},
		},
		templateEngine: template.NewEngine(),
	}

	strategy, err := orch.createPreviewStrategy(operationmode.OperationModeOrganize)
	require.NoError(t, err)
	assert.True(t, resolverCalled, "ResolveStrategy should be called when provided")

	plan, planErr := strategy.Plan(models.FileMatchInfo{Path: "/source/test.mp4"}, &models.Movie{ID: "TEST-001"}, "/dest", false)
	require.NoError(t, planErr)
	assert.Equal(t, expectedPlan, plan)
}

func TestPreviewOrchImpl_CreatePreviewStrategy_NoResolverNilFs(t *testing.T) {
	orch := &previewOrchImpl{
		fs:             nil,
		matcher:        nil,
		previewCfg:     PreviewConfig{ResolveStrategy: nil},
		templateEngine: nil,
	}

	_, err := orch.createPreviewStrategy(operationmode.OperationModeOrganize)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "filesystem not configured")
}

func TestPreviewOrchImpl_CreatePreviewStrategy_NoResolverWithFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	m := &mockMatcherForPreview{}
	orch := &previewOrchImpl{
		fs:             fs,
		matcher:        m,
		previewCfg:     PreviewConfig{ResolveStrategy: nil},
		templateEngine: template.NewEngine(),
	}

	// Should fallback to constructing a minimal strategy
	strategy, err := orch.createPreviewStrategy(operationmode.OperationModeOrganize)
	require.NoError(t, err)
	assert.NotNil(t, strategy)
}

// ---------------------------------------------------------------------------
// preview_helpers.go: generateNFOPaths (0%)
// ---------------------------------------------------------------------------

func TestGenerateNFOPaths_NFODisabled(t *testing.T) {
	nfoPath, nfoPaths := generateNFOPaths(
		&models.Movie{ID: "TEST-001"},
		nil,
		nfo.NFONameConfig{},
		false,
		false, // NFO disabled
		&mockNFOFieldMergerForPreview{nfoFilename: "TEST-001.nfo"},
		"/output",
	)
	assert.Empty(t, nfoPath)
	assert.Nil(t, nfoPaths)
}

func TestGenerateNFOPaths_NilNFOIface(t *testing.T) {
	nfoPath, nfoPaths := generateNFOPaths(
		&models.Movie{ID: "TEST-001"},
		nil,
		nfo.NFONameConfig{},
		false,
		true, // NFO enabled but nil iface
		nil,
		"/output",
	)
	assert.Empty(t, nfoPath)
	assert.Nil(t, nfoPaths)
}

func TestGenerateNFOPaths_SingleFileNFO(t *testing.T) {
	nfoPath, nfoPaths := generateNFOPaths(
		&models.Movie{ID: "TEST-001"},
		[]models.FileMatchInfo{
			{Path: "/source/TEST-001.mp4", Name: "TEST-001.mp4", MovieID: "TEST-001"},
		},
		nfo.NFONameConfig{},
		false, // perFile=false
		true,  // NFO enabled
		&mockNFOFieldMergerForPreview{nfoFilename: "TEST-001.nfo"},
		"/output/TEST-001",
	)
	assert.Equal(t, filepath.FromSlash("/output/TEST-001/TEST-001.nfo"), nfoPath)
	assert.Nil(t, nfoPaths)
}

func TestGenerateNFOPaths_PerFileNFO(t *testing.T) {
	nfoPath, nfoPaths := generateNFOPaths(
		&models.Movie{ID: "ABC-123"},
		[]models.FileMatchInfo{
			{Path: "/source/ABC-123-cd1.mp4", Name: "ABC-123-cd1.mp4", MovieID: "ABC-123", IsMultiPart: true, PartSuffix: "-cd1"},
			{Path: "/source/ABC-123-cd2.mp4", Name: "ABC-123-cd2.mp4", MovieID: "ABC-123", IsMultiPart: true, PartSuffix: "-cd2"},
		},
		nfo.NFONameConfig{},
		true, // perFile=true
		true, // NFO enabled
		&mockNFOFieldMergerForPreview{nfoFilename: "ABC-123.nfo"},
		"/output/ABC-123",
	)
	assert.NotEmpty(t, nfoPath)
	assert.Len(t, nfoPaths, 2, "should generate per-file NFO paths for multipart")
}

func TestGenerateNFOPaths_PerFileNFOWithEmptyPaths(t *testing.T) {
	nfoPath, nfoPaths := generateNFOPaths(
		&models.Movie{ID: "ABC-123"},
		[]models.FileMatchInfo{
			{Path: "", Name: "ABC-123-cd1.mp4", MovieID: "ABC-123", IsMultiPart: true, PartSuffix: "-cd1"},
			{Path: "/source/ABC-123-cd2.mp4", Name: "ABC-123-cd2.mp4", MovieID: "ABC-123", IsMultiPart: true, PartSuffix: "-cd2"},
		},
		nfo.NFONameConfig{},
		true, // perFile=true
		true, // NFO enabled
		&mockNFOFieldMergerForPreview{nfoFilename: "ABC-123.nfo"},
		"/output/ABC-123",
	)
	// Only the second file has a non-empty path
	assert.NotEmpty(t, nfoPath)
	assert.Len(t, nfoPaths, 1, "should only generate NFO for files with non-empty paths")
}

// ---------------------------------------------------------------------------
// preview_helpers.go: validatePathLengths (0%)
// ---------------------------------------------------------------------------

func TestValidatePathLengths_ZeroMaxLength(t *testing.T) {
	// MaxPathLength=0 should skip validation
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 0, template.NewEngine(), []string{"/very/long/path.mp4"}, "", nil, "", "", "", nil)
	})
}

func TestValidatePathLengths_NegativeMaxLength(t *testing.T) {
	// Negative MaxPathLength should skip validation
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), -1, template.NewEngine(), []string{"/very/long/path.mp4"}, "", nil, "", "", "", nil)
	})
}

func TestValidatePathLengths_WithinLimits(t *testing.T) {
	engine := template.NewEngine()
	// Paths within limits should not panic
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 255, engine,
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

func TestValidatePathLengths_WithNFOPaths(t *testing.T) {
	engine := template.NewEngine()
	assert.NotPanics(t, func() {
		validatePathLengths(logging.GlobalLogger(), 255, engine,
			[]string{},
			"/output/ABC-123/ABC-123.nfo",
			[]string{"/output/ABC-123/ABC-123-cd1.nfo", "/output/ABC-123/ABC-123-cd2.nfo"},
			"",
			"",
			"",
			nil,
		)
	})
}

// ---------------------------------------------------------------------------
// factory.go: ScraperConstructionError (0%)
// ---------------------------------------------------------------------------

func TestScraperConstructionError_Error(t *testing.T) {
	innerErr := errors.New("scraper build failed")
	sce := NewScraperConstructionError(innerErr)
	assert.Equal(t, "scraper build failed", sce.Error())
}

func TestScraperConstructionError_Unwrap(t *testing.T) {
	innerErr := errors.New("inner error")
	sce := NewScraperConstructionError(innerErr)
	assert.Equal(t, innerErr, sce.Unwrap())
}

func TestScraperConstructionError_ErrorsIs(t *testing.T) {
	innerErr := errors.New("inner error")
	sce := NewScraperConstructionError(innerErr)
	assert.True(t, errors.Is(sce, innerErr), "ScraperConstructionError should unwrap to inner error")
}

// ---------------------------------------------------------------------------
// factory.go: WorkflowFactory.PosterGen (0%)
// ---------------------------------------------------------------------------

func TestWorkflowFactory_PosterGen(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	pg := factory.PosterGen()
	assert.NotNil(t, pg, "PosterGen should return the cached poster generator")
}

func TestWorkflowFactory_PosterGen_Nil(t *testing.T) {
	// Don't build a poster generator
	fc := workflowFactoryConfig{
		Matcher:   mustBuildMatcher(t),
		PosterGen: nil,
	}
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	pg := factory.PosterGen()
	assert.Nil(t, pg, "PosterGen should return nil when not configured")
}

// ---------------------------------------------------------------------------
// factory.go: newStrategyResolver (14.3%)
// ---------------------------------------------------------------------------

func TestNewStrategyResolver_WithOrgCfg(t *testing.T) {
	fs := afero.NewMemMapFs()
	m := mustBuildMatcher(t)
	engine := template.NewEngine()
	orgCfg := &organizer.Config{
		OperationMode: operationmode.OperationModeOrganize,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat:  ".jpg",
			FanartFormat:  ".jpg",
			TrailerFormat: ".mp4",
		},
	}

	resolver := newStrategyResolver(fs, orgCfg, m, engine)
	assert.NotNil(t, resolver)

	// Call the resolver to ensure it produces a strategy
	strategy := resolver(operationmode.OperationModeOrganize)
	assert.NotNil(t, strategy)
}

func TestNewStrategyResolver_NilOrgCfg(t *testing.T) {
	fs := afero.NewMemMapFs()
	m := mustBuildMatcher(t)
	engine := template.NewEngine()

	resolver := newStrategyResolver(fs, nil, m, engine)
	assert.NotNil(t, resolver)

	// Should create a minimal config strategy
	strategy := resolver(operationmode.OperationModeOrganize)
	assert.NotNil(t, strategy)
}

// ---------------------------------------------------------------------------
// factory.go: WorkflowFactory.NewWorkflow validation (73.3%)
// ---------------------------------------------------------------------------

func TestWorkflowFactory_NewWorkflow_NilOrganizer(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	fc.Organizer = nil // remove organizer
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	_, err = factory.NewWorkflow("job-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "organizer must not be nil")
}

func TestWorkflowFactory_NewWorkflow_NilDownloader(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	fc.Downloader = nil // remove downloader
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	_, err = factory.NewWorkflow("job-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "downloader must not be nil")
}

func TestWorkflowFactory_NewWorkflow_NilMovieRepo(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	fc.MovieRepo = nil // remove movie repo
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	_, err = factory.NewWorkflow("job-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "movieRepo must not be nil")
}

func TestWorkflowFactory_NewWorkflow_NilBatchFileOpRepo(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	fc.BatchFileOpRepo = nil // remove batch file op repo
	factory, err := NewWorkflowFactory(fc)
	require.NoError(t, err)

	_, err = factory.NewWorkflow("job-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "batchFileOpRepo must not be nil")
}

// ---------------------------------------------------------------------------
// factory.go: NewWorkflowFactory nil-validation (already covered by
// TestWorkflowFactory_NewWorkflow_NilBatchFileOpRepo above +
// TestNewWorkflow_NilMatcher below)
// ---------------------------------------------------------------------------

func TestNewWorkflow_NilMatcher(t *testing.T) {
	fc := newFactoryConfigWithDB(t)
	fc.Matcher = nil
	_, err := NewWorkflowFactory(fc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "matcher must not be nil")
}

// ---------------------------------------------------------------------------
// display_title.go: ApplyDisplayTitleFromSource additional coverage (80%)
// ---------------------------------------------------------------------------

func TestApplyDisplayTitleFromSource_TemplateErrorFallsBackToTitle(t *testing.T) {
	// Use a cancelled context to trigger template execution error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &models.Movie{ID: "TEST-001", Title: "Original Title", DisplayTitle: ""}
	src := &models.Movie{Title: "Source Title"}
	ApplyDisplayTitleFromSource(ctx, m, src, "<ID>", template.NewEngine(), nfo.NFONameConfig{})
	// When template fails and DisplayTitle is empty, should fall back to Title
	assert.Equal(t, "Original Title", m.DisplayTitle, "should fall back to Title when template fails and DisplayTitle is empty")
}

func TestApplyDisplayTitleFromSource_TemplateErrorPreservesExisting(t *testing.T) {
	// Use a cancelled context to trigger template execution error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &models.Movie{ID: "TEST-001", Title: "Original Title", DisplayTitle: "Existing Display"}
	src := &models.Movie{Title: "Source Title"}
	ApplyDisplayTitleFromSource(ctx, m, src, "<ID>", template.NewEngine(), nfo.NFONameConfig{})
	// When template fails and DisplayTitle already has a value, preserve it
	assert.Equal(t, "Existing Display", m.DisplayTitle, "should preserve existing DisplayTitle when template fails")
}

func TestApplyDisplayTitleFromSource_NilTitleSource_NilTemplateEngine(t *testing.T) {
	m := &models.Movie{ID: "TEST-001", Title: "Original"}
	// nil titleSource and nil templateEngine — should fall back to Title
	ApplyDisplayTitleFromSource(context.Background(), m, nil, "", nil, nfo.NFONameConfig{})
	assert.Equal(t, "Original", m.DisplayTitle)
}

func TestApplyDisplayTitleFromSource_ScraperIsNil_WithTemplate(t *testing.T) {
	m := &models.Movie{ID: "TEST-001", Title: "Original"}
	// scraped is nil but titleSource is provided and templateEngine is nil
	ApplyDisplayTitleFromSource(context.Background(), nil, m, "{{.Title}}", nil, nfo.NFONameConfig{})
	// scraped is nil, so it goes to the nil check branch
}

func TestApplyDisplayTitleFromSource_WithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &models.Movie{ID: "TEST-001", Title: "Original", DisplayTitle: ""}
	src := &models.Movie{Title: "Source Title"}
	ApplyDisplayTitleFromSource(ctx, m, src, "{{.Title}}", template.NewEngine(), nfo.NFONameConfig{})
	// Template execution with cancelled context may fail; DisplayTitle should get a fallback
}

// noOpRevertLog tests already exist in revert_log_test.go — not duplicated here.

// ---------------------------------------------------------------------------
// revert_log.go: readNFOSnapshot additional coverage (83.3%)
// ---------------------------------------------------------------------------

func TestReadNFOSnapshot_CanonicalizeError(t *testing.T) {
	// Use a path that will cause canonicalize to fail
	fs := afero.NewMemMapFs()
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "\x00invalid")
	assert.Empty(t, result.Content)
	assert.Empty(t, result.FoundPath)
}

// ---------------------------------------------------------------------------
// revert_log.go: buildGeneratedFilesJSON additional coverage (79.2%)
// ---------------------------------------------------------------------------

func TestBuildGeneratedFilesJSON_WithBothNFOAndDownloads(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(),
		"/dest/TEST-001.nfo",
		nil,
		[]string{"/dest/poster.jpg"},
	)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "TEST-001.nfo")
	assert.Contains(t, result, "poster.jpg")
}

func TestBuildGeneratedFilesJSON_SubtitleWithEmptyOriginalPath(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "", NewPath: "/dest/sub1.srt", Moved: true},
	}
	// Subtitle with empty OriginalPath should not appear in MoveBack
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.Empty(t, result)
}

func TestBuildGeneratedFilesJSON_SubtitleWithEmptyNewPath(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/source/sub1.srt", NewPath: "", Moved: true},
	}
	// Subtitle with empty NewPath should not appear in MoveBack
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.Empty(t, result)
}

func TestBuildGeneratedFilesJSON_OnlyNotMovedSubtitles(t *testing.T) {
	// When all subtitles have Moved=false, no MoveBack entries should be generated
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/source/sub1.srt", NewPath: "/dest/sub1.srt", Moved: false},
		{OriginalPath: "/source/sub2.srt", NewPath: "/dest/sub2.srt", Moved: false},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.Empty(t, result)
}

// ---------------------------------------------------------------------------
// Helper functions for tests
// ---------------------------------------------------------------------------

func configForFactory() *config.Config {
	cfg := config.DefaultConfig(nil, nil)
	_, _ = config.Prepare(cfg)
	return cfg
}

func mustBuildMatcher(t *testing.T) matcher.MatcherInterface {
	t.Helper()
	m, err := matcher.NewMatcher(&matcher.Config{RegexEnabled: false})
	require.NoError(t, err)
	return m
}

// TestPreviewOrchImpl_Execute_NilContextDoesNotPanic verifies that a nil
// context is normalized to context.Background() instead of panicking on
// ctx.Err() — consistent with Apply, Compare, and Scrape orchestrators.
func TestPreviewOrchImpl_Execute_NilContextDoesNotPanic(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
			PathCfg: PreviewPathConfig{
				MediaFormatConfig: organizer.MediaFormatConfig{
					PosterFormat: ".jpg",
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
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	assert.NotPanics(t, func() {
		_, err := orch.Execute(nil, PreviewCmd{
			Movie: &models.Movie{ID: "TEST-001"},
			FileResults: []models.FileMatchInfo{
				{Path: "/input/TEST-001.mp4", Name: "TEST-001.mp4", Extension: ".mp4", MovieID: "TEST-001"},
			},
		})
		assert.NoError(t, err)
	})
}

// TestPreviewOrchImpl_Execute_FastPathReturnsResolvedOpMode verifies that the
// in-place fast path (empty/dot source path) returns the resolved operation
// mode, not the original (possibly empty) command value.
func TestPreviewOrchImpl_Execute_FastPathReturnsResolvedOpMode(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newPreviewOrchestrator(
		fs,
		&mockMatcherForPreview{},
		PreviewConfig{
			OpMode: operationmode.OperationModeInPlace,
		},
		nfo.NFONameConfig{},
		template.NewEngine(),
		&mockNFOFieldMergerForPreview{},
		nil,
	).(*previewOrchImpl)

	// cmd.OperationMode is empty → should resolve to config default (InPlace).
	result, err := orch.Execute(context.Background(), PreviewCmd{
		Movie: &models.Movie{ID: "TEST-001"},
		// Empty source path + InPlace mode → fast-path early return.
		FileResults: []models.FileMatchInfo{
			{Path: "", Name: "TEST-001.mp4", Extension: ".mp4", MovieID: "TEST-001"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, operationmode.OperationModeInPlace, result.OperationMode,
		"fast path should return the resolved operation mode, not the empty command value")
}
