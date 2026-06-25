package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test doubles for applyOrchImpl.Execute coverage
// ---------------------------------------------------------------------------

// stubOrganizer implements organizer.OrganizerInterface
type stubOrganizer struct {
	result *organizer.OrganizeResult
	err    error
}

func (s *stubOrganizer) Organize(_ context.Context, _ organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	return s.result, s.err
}

// stubDownloader implements downloader.DownloaderInterface
type stubDownloader struct {
	outcome *downloader.DownloadOutcome
	err     error
}

func (s *stubDownloader) Download(_ context.Context, _ downloader.DownloadCmd) (*downloader.DownloadOutcome, error) {
	return s.outcome, s.err
}

// stubNFOGen implements nfo.GeneratorInterface
type stubNFOGen struct {
	resolvedPath  string
	err           error
	lastVideoPath string
}

func (s *stubNFOGen) Generate(_ context.Context, _ *models.Movie, _, _, _ string, _ []string) error {
	return s.err
}

func (s *stubNFOGen) GenerateAtPath(_ context.Context, _ *models.Movie, _, _ string, _ []string) error {
	return s.err
}

func (s *stubNFOGen) ResolveAndGenerate(_ context.Context, _ *models.Movie, _ string, _ nfo.NFONameConfig, videoPath string, _ []string) (string, error) {
	s.lastVideoPath = videoPath
	return s.resolvedPath, s.err
}

// applyStubNFO implements nfo.NFOInterface for apply orchestrator tests.
// Separate from mockNFOInterface in compare_orchestrator_test.go to avoid conflicts.
type applyStubNFO struct {
	mergeResult nfo.MergeWithExistingResult
}

func (s *applyStubNFO) ParseNFO(_ afero.Fs, _ string) (*nfo.ParseResult, error) {
	return nil, nil
}

func (s *applyStubNFO) MergeMovieMetadataWithOptions(_ *models.Movie, _ *models.Movie, _ nfo.MergeStrategy, _ bool) (*nfo.MergeResult, error) {
	return nil, nil
}

func (s *applyStubNFO) MergeWithExistingNFO(movie *models.Movie, _ nfo.MergeWithExistingOptions) nfo.MergeWithExistingResult {
	if s.mergeResult.Movie != nil {
		return s.mergeResult
	}
	// Default: pass movie through unchanged
	return nfo.MergeWithExistingResult{Movie: movie}
}

func (s *applyStubNFO) ResolveNFOFilename(_ *models.Movie, _ nfo.NFONameConfig) string {
	return ""
}

func (s *applyStubNFO) ResolveNFOPath(_ string, _ *models.Movie, _ nfo.NFONameConfig, _ string) (string, []string) {
	return "", nil
}

// stubTagRepo implements database.MovieTagRepositoryInterface
type stubTagRepo struct {
	tags []string
	err  error
}

func (s *stubTagRepo) AddTag(_ context.Context, _, _ string) error     { return nil }
func (s *stubTagRepo) RemoveTag(_ context.Context, _, _ string) error  { return nil }
func (s *stubTagRepo) RemoveAllTags(_ context.Context, _ string) error { return nil }
func (s *stubTagRepo) GetTagsForMovie(_ context.Context, _ string) ([]string, error) {
	return s.tags, s.err
}
func (s *stubTagRepo) GetMoviesWithTag(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (s *stubTagRepo) ListTagsPaginated(_ context.Context, _, _ int) ([]models.MovieTag, error) {
	return nil, nil
}
func (s *stubTagRepo) ListAll(_ context.Context) (map[string][]string, error) { return nil, nil }
func (s *stubTagRepo) ListAllChunked(_ context.Context, _ int) (map[string][]string, error) {
	return nil, nil
}
func (s *stubTagRepo) GetUniqueTagsList(_ context.Context) ([]string, error) { return nil, nil }

// recordingRevertLog records Begin/Complete calls for assertion
type recordingRevertLog struct {
	beginCalls    int
	completeCalls int
	lastResult    *ApplyResult
	beginOpID     string
	beginErr      error
}

func (r *recordingRevertLog) Begin(_ context.Context, _ ApplyCmd) (OperationID, error) {
	r.beginCalls++
	if r.beginErr != nil {
		return "", r.beginErr
	}
	r.beginOpID = "op-rec-1"
	return r.beginOpID, nil
}

func (r *recordingRevertLog) CaptureSnapshot(_ context.Context, _ OperationID, _ ApplyCmd) {}

func (r *recordingRevertLog) Complete(_ context.Context, _ OperationID, result *ApplyResult) error {
	r.completeCalls++
	r.lastResult = result
	return nil
}

func (r *recordingRevertLog) CompleteFailed(_ context.Context, _ OperationID, result *ApplyResult) error {
	r.completeCalls++
	r.lastResult = result
	return nil
}

// completeErrorRevertLog returns an error from Complete
type completeErrorRevertLog struct {
	noOpRevertLog
	completeErr error
}

func (c completeErrorRevertLog) Complete(_ context.Context, _ OperationID, _ *ApplyResult) error {
	return c.completeErr
}

func (c completeErrorRevertLog) CompleteFailed(_ context.Context, _ OperationID, _ *ApplyResult) error {
	return c.completeErr
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — nil movie
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NilMovie(t *testing.T) {
	impl := &applyOrchImpl{fs: afero.NewMemMapFs()}
	result, err := impl.Execute(context.Background(), ApplyCmd{Movie: nil}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "movie is nil")
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — nil context replaced with Background
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NilContext(t *testing.T) {
	impl := &applyOrchImpl{
		fs:  afero.NewMemMapFs(),
		nfo: &applyStubNFO{},
	}
	result, err := impl.Execute(nil, ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — skip organize (success path with no organizer)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_SkipOrganize(t *testing.T) {
	impl := &applyOrchImpl{
		fs:  afero.NewMemMapFs(),
		nfo: &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Steps.Organized)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — organize success
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_OrganizeSuccess(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		organizer: &stubOrganizer{
			result: &organizer.OrganizeResult{
				NewPath:    "/dest/TEST-001.mp4",
				FolderPath: "/dest/TEST-001",
			},
		},
		nfo: &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.Organized)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — organize error
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_OrganizeError(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		organizer: &stubOrganizer{
			err: errors.New("organize failed"),
		},
		nfo:       &applyStubNFO{},
		revertLog: noOpRevertLog{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "organization failed")
	require.NotNil(t, result)
	assert.Equal(t, "organize", result.FailedStep)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — download error (regular)
// Download failures are non-fatal: the pipeline logs the error and continues to
// NFO generation, mirroring main's ProcessFileTask.Execute (the project
// guarantee is that a correct NFO is produced regardless of artwork availability).
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_DownloadError(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		downloader: &stubDownloader{
			err: errors.New("download failed"),
		},
		nfo:       &applyStubNFO{},
		revertLog: noOpRevertLog{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err, "download failure must not abort the apply pipeline")
	require.NotNil(t, result)
	assert.False(t, result.Steps.Downloaded, "download step should not be marked complete on failure")
	assert.Empty(t, result.FailedStep, "no step should fail when download error is tolerated")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — download partial error
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_DownloadPartialError(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		downloader: &stubDownloader{
			err: &downloader.DownloadPartialError{Attempted: 2, Succeeded: 0},
		},
		nfo:       &applyStubNFO{},
		revertLog: noOpRevertLog{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err, "download partial failure must not abort the apply pipeline")
	require.NotNil(t, result)
	assert.False(t, result.Steps.Downloaded, "download step should not be marked complete on partial failure")
	assert.Empty(t, result.FailedStep, "no step should fail when download partial error is tolerated")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — download partial error preserves non-critical paths
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_DownloadPartialErrorPreservesPaths(t *testing.T) {
	// A DownloadPartialError means all CRITICAL media (cover/poster) failed, but
	// non-critical media (extrafanart/actress) may have succeeded first. The
	// downloader returns those partial paths alongside the error; stepDownload
	// must preserve them on state.downloadPaths so Complete/CompleteFailed can
	// record the artifacts for revert cleanup (instead of dropping them).
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		downloader: &stubDownloader{
			outcome: &downloader.DownloadOutcome{
				DownloadedPaths: []string{"/dest/extrafanart/s1.jpg"},
			},
			err: &downloader.DownloadPartialError{Attempted: 2, Succeeded: 0},
		},
		nfo:       &applyStubNFO{},
		revertLog: noOpRevertLog{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err, "download partial failure must not abort the apply pipeline")
	require.NotNil(t, result)
	assert.False(t, result.Steps.Downloaded, "download step should not be marked complete on partial failure")
	assert.Contains(t, result.DownloadPaths, "/dest/extrafanart/s1.jpg",
		"non-critical artifacts produced before the partial error must be preserved for revert cleanup")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — download success
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_DownloadSuccess(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		downloader: &stubDownloader{
			outcome: &downloader.DownloadOutcome{
				DownloadedPaths: []string{"/dest/poster.jpg"},
			},
		},
		nfo: &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.Downloaded)
	assert.Contains(t, result.DownloadPaths, "/dest/poster.jpg")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO uses the post-organize video path when moved (WF-4)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NFOUsesPostOrganizePathWhenMoved(t *testing.T) {
	nfoGen := &stubNFOGen{resolvedPath: "/dest/TEST-001.nfo"}
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		organizer: &stubOrganizer{
			result: &organizer.OrganizeResult{
				NewPath:    "/dest/TEST-001.mp4",
				FolderPath: "/dest",
			},
		},
		nfo:       &applyStubNFO{},
		nfoGen:    nfoGen,
		revertLog: noOpRevertLog{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(), // cmd.Match.Path is the original source path
		DestPath:    "/dest",
		Organize:    OrganizeOptions{MoveFiles: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.NFOGenerated, "NFO should be generated")
	// stepNFO must pass the post-organize NewPath, not the original cmd.Match.Path,
	// so MediaInfo stream details can be extracted from the relocated file.
	assert.Equal(t, "/dest/TEST-001.mp4", nfoGen.lastVideoPath, "stepNFO must use organizeResult.NewPath when the file was moved")
}

// TestApplyOrchImpl_Execute_NFOUsesOriginalPathWhenOrganizeSkipped verifies the
// WF-4 fallback: when organize is skipped, stepNFO uses cmd.Match.Path.
func TestApplyOrchImpl_Execute_NFOUsesOriginalPathWhenOrganizeSkipped(t *testing.T) {
	nfoGen := &stubNFOGen{resolvedPath: "/dest/TEST-001.nfo"}
	impl := &applyOrchImpl{
		fs:        afero.NewMemMapFs(),
		nfo:       &applyStubNFO{},
		nfoGen:    nfoGen,
		revertLog: noOpRevertLog{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.NFOGenerated)
	assert.Equal(t, defaultMatch().Path, nfoGen.lastVideoPath, "stepNFO must fall back to cmd.Match.Path when organize is skipped")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — download skipped when DryRun=true
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_DownloadSkippedDryRun(t *testing.T) {
	impl := &applyOrchImpl{
		fs:         afero.NewMemMapFs(),
		downloader: &stubDownloader{}, // would panic if called
		nfo:        &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
		DryRun:   true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Steps.Downloaded, "Download should be skipped during dry run")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO generation error
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NFOGenerationError(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		nfoGen: &stubNFOGen{
			err: errors.New("nfo generation failed"),
		},
		nfo:       &applyStubNFO{},
		revertLog: noOpRevertLog{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NFO generation failed")
	require.NotNil(t, result)
	assert.Equal(t, "nfo_generation", result.FailedStep)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO generation success
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NFOGenerationSuccess(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		nfoGen: &stubNFOGen{
			resolvedPath: "/dest/TEST-001.nfo",
		},
		nfo: &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.NFOGenerated)
	assert.Equal(t, "/dest/TEST-001.nfo", result.NFOPath)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO generation skipped when DryRun=true
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NFOSkippedDryRun(t *testing.T) {
	impl := &applyOrchImpl{
		fs:     afero.NewMemMapFs(),
		nfoGen: &stubNFOGen{}, // would return empty path if called
		nfo:    &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
		DryRun:      true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Steps.NFOGenerated, "NFO generation should be skipped during dry run")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO generation returns empty resolvedPath (skipped)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NFOSkippedEmptyResolvedPath(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		nfoGen: &stubNFOGen{
			resolvedPath: "", // empty means generation was skipped
		},
		nfo: &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Steps.NFOGenerated, "Empty resolvedPath means NFO generation was skipped")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — display title applied
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_DisplayTitleApplied(t *testing.T) {
	impl := &applyOrchImpl{
		fs:             afero.NewMemMapFs(),
		nfo:            &applyStubNFO{},
		applyCfg:       ApplyConfig{DisplayTitle: "[<ID>] <TITLE>"},
		templateEngine: template.NewEngine(),
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test Movie"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.DisplayTitle)
	assert.Equal(t, "[TEST-001] Test Movie", result.Movie.DisplayTitle)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — display title falls back to movie title
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_DisplayTitleFallbackToTitle(t *testing.T) {
	impl := &applyOrchImpl{
		fs:       afero.NewMemMapFs(),
		nfo:      &applyStubNFO{},
		applyCfg: ApplyConfig{DisplayTitle: ""}, // no display title template
	}
	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.DisplayTitle)
	assert.Equal(t, "Test Movie", result.Movie.DisplayTitle)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — display title with custom source
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_DisplayTitleWithCustomSource(t *testing.T) {
	impl := &applyOrchImpl{
		fs:             afero.NewMemMapFs(),
		nfo:            &applyStubNFO{},
		applyCfg:       ApplyConfig{DisplayTitle: "[<ID>] <TITLE>"},
		templateEngine: template.NewEngine(),
	}
	sourceMovie := &models.Movie{ID: "SRC-001", Title: "Source Title"}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:           &models.Movie{ID: "TEST-001", Title: "Test Movie"},
		Match:           defaultMatch(),
		Organize:        OrganizeOptions{Skip: true},
		DisplayTitleSrc: sourceMovie,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	// DisplayTitleSrc provides Title/OriginalTitle for template rendering, but ID comes from cmd.Movie
	assert.Equal(t, "[TEST-001] Source Title", result.Movie.DisplayTitle)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO merge step
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_MergeStep(t *testing.T) {
	mergedMovie := &models.Movie{ID: "TEST-001", Title: "Merged Title"}
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		nfo: &applyStubNFO{
			mergeResult: nfo.MergeWithExistingResult{
				Movie:        mergedMovie,
				Merged:       true,
				MergeStats:   &nfo.MergeStats{},
				FoundNFOPath: "/found/TEST-001.nfo",
			},
		},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.Merged)
	assert.True(t, result.Merged)
	assert.Equal(t, "Merged Title", result.Movie.Title)
	assert.Equal(t, "/found/TEST-001.nfo", result.FoundNFOPath)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — progress callback
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_ProgressCallback(t *testing.T) {
	var progressSteps []scrape.ProgressStep
	progress := func(step scrape.ProgressStep, pct float64, msg string) {
		progressSteps = append(progressSteps, step)
	}

	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		organizer: &stubOrganizer{
			result: &organizer.OrganizeResult{
				NewPath:    "/dest/TEST-001.mp4",
				FolderPath: "/dest/TEST-001",
			},
		},
		nfo: &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, progress)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, progressSteps, scrape.ProgressStepOrganize)
	assert.Contains(t, progressSteps, scrape.ProgressStepApply)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — with recordingRevertLog (success path)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_WithRevertLog_Success(t *testing.T) {
	rl := &recordingRevertLog{}
	impl := &applyOrchImpl{
		fs:        afero.NewMemMapFs(),
		nfo:       &applyStubNFO{},
		revertLog: rl,
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, rl.beginCalls, "Begin should be called once")
	assert.Equal(t, 1, rl.completeCalls, "Complete should be called once on success")
	assert.NotNil(t, rl.lastResult)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — with recordingRevertLog (failure path)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_WithRevertLog_Failure(t *testing.T) {
	rl := &recordingRevertLog{}
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		organizer: &stubOrganizer{
			err: errors.New("organize failed"),
		},
		nfo:       &applyStubNFO{},
		revertLog: rl,
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/dest",
		Organize: OrganizeOptions{MoveFiles: true},
	}, nil)
	assert.Error(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, rl.beginCalls, "Begin should be called once")
	// Complete should be called with nil result (failure path)
	assert.Equal(t, 1, rl.completeCalls, "Complete should be called once on failure")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — multipart download
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_MultipartDownload(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		downloader: &stubDownloader{
			outcome: &downloader.DownloadOutcome{
				DownloadedPaths: []string{"/dest/poster.jpg"},
			},
		},
		nfo: &applyStubNFO{},
	}
	match := models.FileMatchInfo{
		Path:        "/src/ABC-001-cd1.mp4",
		MovieID:     "ABC-001",
		IsMultiPart: true,
		PartNumber:  1,
		PartSuffix:  "-cd1",
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "ABC-001", Title: "Test"},
		Match:    match,
		DestPath: "/dest",
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.Downloaded)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO generation with tag repo
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NFOGenerationWithTagRepo(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		nfoGen: &stubNFOGen{
			resolvedPath: "/dest/TEST-001.nfo",
		},
		nfo:     &applyStubNFO{},
		tagRepo: &stubTagRepo{tags: []string{"tag1", "tag2"}},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.NFOGenerated)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO generation with tag repo error
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NFOGenerationWithTagRepoError(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		nfoGen: &stubNFOGen{
			resolvedPath: "/dest/TEST-001.nfo",
		},
		nfo:     &applyStubNFO{},
		tagRepo: &stubTagRepo{err: errors.New("tag repo error")},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.NFOGenerated, "NFO generation should succeed even if tag repo fails")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — NFO generation with nil tag repo
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NFOGenerationNilTagRepo(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		nfoGen: &stubNFOGen{
			resolvedPath: "/dest/TEST-001.nfo",
		},
		nfo:     &applyStubNFO{},
		tagRepo: nil,
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.NFOGenerated)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — full success path (all steps)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_FullSuccessPath(t *testing.T) {
	rl := &recordingRevertLog{}
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		organizer: &stubOrganizer{
			result: &organizer.OrganizeResult{
				NewPath:    "/dest/TEST-001/TEST-001.mp4",
				FolderPath: "/dest/TEST-001",
			},
		},
		downloader: &stubDownloader{
			outcome: &downloader.DownloadOutcome{
				DownloadedPaths: []string{"/dest/TEST-001/poster.jpg"},
			},
		},
		nfoGen: &stubNFOGen{
			resolvedPath: "/dest/TEST-001/TEST-001.nfo",
		},
		nfo:            &applyStubNFO{},
		applyCfg:       ApplyConfig{DisplayTitle: "[<ID>] <TITLE>"},
		templateEngine: template.NewEngine(),
		revertLog:      rl,
		tagRepo:        &stubTagRepo{tags: []string{"tag1"}},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test Movie"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{MoveFiles: true},
		Download:    true,
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.Organized)
	assert.True(t, result.Steps.Merged)
	assert.True(t, result.Steps.DisplayTitle)
	assert.True(t, result.Steps.Downloaded)
	assert.True(t, result.Steps.NFOGenerated)
	assert.Equal(t, "op-rec-1", result.OperationID)
	assert.Equal(t, 1, rl.completeCalls)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — organize result with folder path changes targetDir
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_OrganizeResultChangesTargetDir(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		organizer: &stubOrganizer{
			result: &organizer.OrganizeResult{
				NewPath:    "/actual-dest/TEST-001.mp4",
				FolderPath: "/actual-dest",
			},
		},
		downloader: &stubDownloader{
			outcome: &downloader.DownloadOutcome{
				DownloadedPaths: []string{"/actual-dest/poster.jpg"},
			},
		},
		nfo: &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		DestPath: "/original-dest",
		Organize: OrganizeOptions{MoveFiles: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.Downloaded)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.withRevertLog — removed (factory creates fresh instances per workflow)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — revertLog begin error (non-dry-run)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_RevertLogBeginError(t *testing.T) {
	rl := &recordingRevertLog{beginErr: errors.New("begin failed")}
	impl := &applyOrchImpl{
		fs:        afero.NewMemMapFs(),
		nfo:       &applyStubNFO{},
		revertLog: rl,
	}
	// Should not panic when Begin fails
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — revertLog begin error (dry run)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_RevertLogBeginError_DryRun(t *testing.T) {
	rl := &recordingRevertLog{beginErr: errors.New("begin failed")}
	impl := &applyOrchImpl{
		fs:        afero.NewMemMapFs(),
		nfo:       &applyStubNFO{},
		revertLog: rl,
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
		DryRun:   true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — revertLog Complete error on success
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_RevertLogCompleteErrorOnSuccess(t *testing.T) {
	impl := &applyOrchImpl{
		fs:        afero.NewMemMapFs(),
		nfo:       &applyStubNFO{},
		revertLog: completeErrorRevertLog{completeErr: errors.New("complete failed")},
	}
	// Should not return error when Complete fails on success path
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — nil organizer (skipped organize)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NilOrganizer_SkipFalse(t *testing.T) {
	impl := &applyOrchImpl{
		fs:        afero.NewMemMapFs(),
		organizer: nil,
		nfo:       &applyStubNFO{},
	}
	// When organizer is nil and Skip is false, organize step is still skipped
	// because of the `!cmd.Organize.Skip && o.organizer != nil` check
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: false},
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Steps.Organized, "Organize should be skipped when organizer is nil")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — nil downloader (download skipped)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NilDownloader(t *testing.T) {
	impl := &applyOrchImpl{
		fs:         afero.NewMemMapFs(),
		downloader: nil,
		nfo:        &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:    defaultMatch(),
		Organize: OrganizeOptions{Skip: true},
		Download: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Steps.Downloaded, "Download should be skipped when downloader is nil")
}

// ---------------------------------------------------------------------------
// applyOrchImpl.Execute — nil nfoGen (NFO generation skipped)
// ---------------------------------------------------------------------------

func TestApplyOrchImpl_Execute_NilNFOGenerator(t *testing.T) {
	impl := &applyOrchImpl{
		fs:     afero.NewMemMapFs(),
		nfoGen: nil,
		nfo:    &applyStubNFO{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		Organize:    OrganizeOptions{Skip: true},
		GenerateNFO: true,
	}, nil)
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Steps.NFOGenerated, "NFO generation should be skipped when nfoGen is nil")
}

// ---------------------------------------------------------------------------
// noOpApplyOrchestrator
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// newApplyOrchestrator — constructor
// ---------------------------------------------------------------------------

func TestNewApplyOrchestrator(t *testing.T) {
	orch := newApplyOrchestrator(
		afero.NewMemMapFs(),
		&stubOrganizer{},
		&stubDownloader{},
		&stubNFOGen{},
		&applyStubNFO{},
		ApplyConfig{NFONameCfg: nfo.NFONameConfig{}, DisplayTitle: "test-template"},
		template.NewEngine(),
		noOpRevertLog{},
		&stubTagRepo{},
		nil, // logger
	)
	assert.NotNil(t, orch)

	assert.NotNil(t, orch.fs)
	assert.NotNil(t, orch.organizer)
	assert.NotNil(t, orch.downloader)
	assert.NotNil(t, orch.nfoGen)
	assert.NotNil(t, orch.nfo)
}

// TestApplyOrchImpl_Execute_PartialFailureReportsCompletedSteps verifies that
// when a late step fails after organize+merge+displayTitle+download succeed,
// the partial ApplyResult.Steps reflects the actual completed steps — not
// all-false. This is the regression for the executeSteps bug where onStepFail
// received a zero-value stepsSoFar instead of the real outer steps variable.
// (Download failure is non-fatal, so NFO generation is the failing step here.)
func TestApplyOrchImpl_Execute_PartialFailureReportsCompletedSteps(t *testing.T) {
	impl := &applyOrchImpl{
		fs: afero.NewMemMapFs(),
		downloader: &stubDownloader{
			outcome: &downloader.DownloadOutcome{
				DownloadedPaths: []string{"/dest/poster.jpg"},
			},
		},
		nfo:       &applyStubNFO{},
		nfoGen:    &stubNFOGen{err: errors.New("nfo generation failed")},
		revertLog: noOpRevertLog{},
	}
	result, err := impl.Execute(context.Background(), ApplyCmd{
		Movie:       &models.Movie{ID: "TEST-001", Title: "Test"},
		Match:       defaultMatch(),
		DestPath:    "/dest",
		Organize:    OrganizeOptions{Skip: true},
		Download:    true,
		GenerateNFO: true,
	}, nil)
	assert.Error(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "nfo_generation", result.FailedStep)
	// Merge, DisplayTitle, and Download steps ran before NFO failed. With the
	// old bug, these would all be false because onStepFail got a zero-value copy.
	assert.True(t, result.Steps.Merged, "Merged should reflect the completed merge step")
	assert.True(t, result.Steps.DisplayTitle, "DisplayTitle should reflect the completed step")
	assert.True(t, result.Steps.Downloaded, "Downloaded should be true — download succeeded before NFO failed")
	assert.False(t, result.Steps.NFOGenerated, "NFOGenerated should be false — NFO generation failed")
}
