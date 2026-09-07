package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestPosterRecropRetryAndResolution(t *testing.T) {
	for _, action := range []string{"unchanged", "omitted", "omitted source change", "fresh", "fresh source change", "remove", "reset baseline url", "crop endpoint", "remove endpoint", "unmeasured"} {
		t.Run(action, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "movie.mp4")
			job := newBatchJob([]string{path})
			movie := &models.Movie{ID: "CROP-1", Poster: models.PosterState{PosterURL: "https://example.test/a.jpg", PosterCropSourceFull: true, PosterCropBounds: &models.CropBounds{Width: .4, Height: 1}}}
			job.results.UpdateFileResult(path, &resultstore.MovieResult{Movie: movie, ErrorCode: downloader.PosterRecropRequiredCode, Error: "rejected", Status: models.JobStatusFailed, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}})
			pe := NewPosterEditor(job.results, job.results, nil)
			fresh := &models.CropBounds{Width: .5, Height: 1, SourceFingerprint: assetidentity.FromBytes([]byte("fresh source")).Fingerprint}
			edit := movie.Clone()
			edit.Title = "edited"
			opts := FamilySaveOptions{}
			resolved := false
			switch action {
			case "omitted", "omitted source change":
				opts.CarryCropGeometry = true
				edit.Poster.PosterCropBounds = nil
				if action == "omitted source change" {
					edit.Poster.PosterURL = "https://example.test/b.jpg"
				}
			case "fresh", "fresh source change":
				edit.Poster.PosterCropBounds = fresh
				resolved = true
				if action == "fresh source change" {
					edit.Poster.PosterURL = "https://example.test/b.jpg"
				}
			case "remove":
				edit.Poster.PosterCropBounds = nil
				resolved = true
			case "reset baseline url":
				edit.Poster.PosterCropBounds = nil
				edit.Poster.CroppedPosterURL = "baseline-crop.jpg"
				resolved = true
			case "crop endpoint":
				require.NoError(t, pe.UpdatePosterCrop(movie.ID, "preview.jpg", fresh, true))
				resolved = true
			case "remove endpoint":
				require.NoError(t, pe.UpdatePosterCrop(movie.ID, "", nil, false))
				resolved = true
			case "unmeasured":
				require.NoError(t, pe.UpdatePosterCrop(movie.ID, "preview.jpg", fresh, false))
			}
			if action != "unchanged" && action != "crop endpoint" && action != "remove endpoint" && action != "unmeasured" {
				require.NoError(t, pe.UpdateMovieFamily(context.Background(), movie.ID, "", edit, opts))
			}
			current, err := job.results.GetMovieResult(path)
			require.NoError(t, err)
			if resolved {
				require.Empty(t, current.ErrorCode)
				require.Empty(t, current.Error)
				if action == "remove" || action == "reset baseline url" || action == "remove endpoint" {
					require.Nil(t, current.Movie.Poster.PosterCropBounds)
				} else {
					require.Equal(t, fresh, current.Movie.Poster.PosterCropBounds)
				}
			} else {
				require.Equal(t, downloader.PosterRecropRequiredCode, current.ErrorCode)
				require.NotNil(t, current.Movie.Poster.PosterCropBounds)
			}
			dbJob, err := jobpersist.Encode(jobpersist.Snapshot{Results: map[string]*resultstore.MovieResult{path: current}})
			require.NoError(t, err)
			snapshot, errs := jobpersist.Decode(dbJob)
			require.Empty(t, errs)
			restored := snapshot.Results[path]
			wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: restored.Movie}}
			inputs := minimalApplyInputs(t, job.results, true)
			inputs.WF = wf
			cfg := ApplyPhaseConfig{Download: true}
			cmd, afc, execute := buildApplyCmd(path, restored.Movie, restored, inputs, cfg, context.Background())
			require.True(t, execute)
			result := applyFile(context.Background(), wf, path, restored, restored.Movie, &preparedApplyFile{cmd: cmd, afc: afc, baseline: restored.Movie.Clone(), execute: execute}, inputs, cfg)
			require.Equal(t, !resolved, result.Failed)
			if resolved {
				require.Equal(t, 1, wf.getApplyCalled())
			} else {
				require.Zero(t, wf.getApplyCalled())
			}
		})
	}
}

type blockedRecropWorkflow struct {
	stubApplyWorkflow
	started chan struct{}
	release chan struct{}
}

func (w *blockedRecropWorkflow) Apply(_ context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	close(w.started)
	<-w.release
	return nil, &downloader.PosterRecropRequiredError{Reason: downloader.SourceFingerprintMissing, Bounds: *cmd.Movie.Poster.PosterCropBounds}
}

func TestPosterRecropConcurrentNewCrop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	store := resultstore.New(1, []string{path})
	movie := &models.Movie{ID: "CROP-1", Poster: models.PosterState{PosterCropSourceFull: true, PosterCropBounds: &models.CropBounds{Width: .4, Height: 1}}}
	store.UpdateFileResult(path, &resultstore.MovieResult{Movie: movie, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}})
	before, err := store.GetMovieResult(path)
	require.NoError(t, err)
	pe := NewPosterEditor(store, store, nil)
	wf := &blockedRecropWorkflow{started: make(chan struct{}), release: make(chan struct{})}
	inputs := minimalApplyInputs(t, store, true)
	inputs.EditLockFn = func(ids ...string) func() { return pe.lockRegistry().AcquireMany(ids) }
	cfg := ApplyPhaseConfig{}
	cmd, afc, execute := buildApplyCmd(path, movie, before, inputs, cfg, context.Background())
	require.True(t, execute)
	done := make(chan applyFileOutcome, 1)
	go func() {
		done <- applyFile(context.Background(), wf, path, before, movie, &preparedApplyFile{cmd: cmd, afc: afc, baseline: movie.Clone(), execute: execute}, inputs, cfg)
	}()
	<-wf.started
	fresh := &models.CropBounds{Width: .5, Height: 1, SourceFingerprint: assetidentity.FromBytes([]byte("new source")).Fingerprint}
	editErr := pe.UpdatePosterCrop(movie.ID, "new-preview.jpg", fresh, true)
	close(wf.release)
	outcome := <-done
	require.NoError(t, editErr)
	require.True(t, outcome.Failed)
	after, err := store.GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, fresh, after.Movie.Poster.PosterCropBounds)
	require.Empty(t, after.ErrorCode)
}

func TestPosterRecropMissingBoundsStillBlocksRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	store := resultstore.New(1, []string{path})
	movie := &models.Movie{ID: "CROP-1"}
	stored := &resultstore.MovieResult{Movie: movie, ErrorCode: downloader.PosterRecropRequiredCode, Status: models.JobStatusFailed, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}}
	store.UpdateFileResult(path, stored)
	inputs := minimalApplyInputs(t, store, true)
	wf := &stubApplyWorkflow{}
	cfg := ApplyPhaseConfig{Download: true}
	cmd, afc, execute := buildApplyCmd(path, movie, stored, inputs, cfg, context.Background())
	require.True(t, execute)
	outcome := applyFile(context.Background(), wf, path, stored, movie, &preparedApplyFile{cmd: cmd, afc: afc, baseline: movie.Clone(), execute: execute}, inputs, cfg)
	require.True(t, outcome.Failed)
	require.Zero(t, wf.getApplyCalled())
	after, err := store.GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, downloader.PosterRecropRequiredCode, after.ErrorCode)
}

func TestPosterRecropIntentComparison(t *testing.T) {
	empty := &models.Movie{}
	crop := &models.Movie{Poster: models.PosterState{PosterCropBounds: &models.CropBounds{Width: .4, Height: 1}}}
	require.False(t, samePosterCropIntent(nil, empty))
	require.False(t, samePosterCropIntent(empty, nil))
	require.False(t, samePosterCropIntent(empty, crop))
	require.True(t, samePosterCropIntent(empty, empty))
	require.True(t, samePosterCropIntent(crop, crop.Clone()))
}

func TestPosterRecropLateWriteback(t *testing.T) {
	for _, newerCrop := range []bool{false, true} {
		t.Run(fmt.Sprint(newerCrop), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "movie.mp4")
			store := resultstore.New(1, []string{path})
			movie := &models.Movie{ID: "CROP-1", Poster: models.PosterState{PosterCropSourceFull: true, PosterCropBounds: &models.CropBounds{Width: .4, Height: 1}}}
			store.UpdateFileResult(path, &resultstore.MovieResult{Movie: movie, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}})
			before, err := store.GetMovieResult(path)
			require.NoError(t, err)
			pe := NewPosterEditor(store, store, nil)
			edited := movie.Clone()
			edited.Title = "concurrent edit"
			if newerCrop {
				edited.Poster.PosterCropBounds = &models.CropBounds{Width: .5, Height: 1, SourceFingerprint: assetidentity.FromBytes([]byte("new source")).Fingerprint}
			}
			editedDone := make(chan error, 1)
			go func() {
				editedDone <- pe.UpdateMovieFamily(context.Background(), movie.ID, "", edited, FamilySaveOptions{CarryCropGeometry: !newerCrop})
			}()
			require.NoError(t, <-editedDone)
			inputs := minimalApplyInputs(t, store, true)
			refusal := &downloader.PosterRecropRequiredError{Reason: downloader.SourceFingerprintMissing, Bounds: *movie.Poster.PosterCropBounds}
			outcome := interpretApplyResult(path, movie, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), &ApplyFileContext{FilePath: path, Match: before.FileMatchInfo, MovieResult: before}, nil, refusal)
			require.True(t, outcome.Failed)
			after, err := store.GetMovieResult(path)
			require.NoError(t, err)
			require.Equal(t, "concurrent edit", after.Movie.Title)
			require.Equal(t, edited.Poster.PosterCropBounds, after.Movie.Poster.PosterCropBounds)
			if newerCrop {
				require.Empty(t, after.ErrorCode)
			} else {
				require.Equal(t, downloader.PosterRecropRequiredCode, after.ErrorCode)
			}
		})
	}
}

func TestPosterRecropWritebackPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	movie := &models.Movie{ID: "CROP-1", Poster: models.PosterState{PosterCropSourceFull: true, PosterCropBounds: &models.CropBounds{Width: .4, Height: 1}}}
	store := resultstore.New(1, []string{path})
	store.UpdateFileResult(path, &resultstore.MovieResult{Movie: movie, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}})
	before, err := store.GetMovieResult(path)
	require.NoError(t, err)
	refusal := &downloader.PosterRecropRequiredError{Reason: downloader.SourceFingerprintMissing, Bounds: *movie.Poster.PosterCropBounds}
	wrapped := fmt.Errorf("apply media: %w", refusal)
	require.ErrorIs(t, wrapped, downloader.ErrPosterRecropRequired)
	var typed *downloader.PosterRecropRequiredError
	require.ErrorAs(t, wrapped, &typed)
	outcome := interpretApplyResult(path, movie, time.Now(), time.Minute, minimalApplyInputs(t, store, true), ApplyPhaseConfig{}, context.Background(), &ApplyFileContext{FilePath: path, Match: before.FileMatchInfo, MovieResult: before}, nil, wrapped)
	require.True(t, outcome.Failed)
	result, err := store.GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, downloader.PosterRecropRequiredCode, result.ErrorCode)
	require.NotEmpty(t, result.Error)
	dbJob, err := jobpersist.Encode(jobpersist.Snapshot{ID: "recrop", Files: []string{path}, Results: map[string]*resultstore.MovieResult{path: result}, Status: models.JobStatusCompleted, TempDir: t.TempDir()})
	require.NoError(t, err)
	snapshot, errs := jobpersist.Decode(dbJob)
	require.Empty(t, errs)
	require.Equal(t, result.ErrorCode, snapshot.Results[path].ErrorCode)
	require.Equal(t, result.Movie.Poster, snapshot.Results[path].Movie.Poster)
	queue := NewJobStore(nil, nil, nil, t.TempDir(), nil, nil)
	restored := queue.reconstructBatchJob(dbJob)
	require.NotNil(t, restored)
	restoredResult, err := restored.results.GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, result.ErrorCode, restoredResult.ErrorCode)
	require.Equal(t, result.Movie.Poster, restoredResult.Movie.Poster)
}

func TestPosterRecropUnmeasuredPreviewCropKeepsBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	job := newBatchJob([]string{path})
	movie := &models.Movie{ID: "CROP-1", Poster: models.PosterState{PosterURL: "https://example.test/a.jpg", PosterCropSourceFull: true, PosterCropBounds: &models.CropBounds{Width: .4, Height: 1}}}
	job.results.UpdateFileResult(path, &resultstore.MovieResult{Movie: movie, ErrorCode: downloader.PosterRecropRequiredCode, Error: "rejected", Status: models.JobStatusFailed, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}})
	pe := NewPosterEditor(job.results, job.results, nil)
	require.NoError(t, pe.UpdatePosterCrop(movie.ID, "preview-cropped.jpg", nil, false))
	current, err := job.results.GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, downloader.PosterRecropRequiredCode, current.ErrorCode, "unmeasured preview-only crop must not discharge the recrop block")
	require.Equal(t, "preview-cropped.jpg", current.Movie.Poster.CroppedPosterURL)
	require.Nil(t, current.Movie.Poster.PosterCropBounds, "endpoint clears geometry; the block rides on the ErrorCode marker")
}

func TestPosterRecropSourceReplacementClearsBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	job := newBatchJob([]string{path})
	movie := &models.Movie{ID: "CROP-1", Poster: models.PosterState{PosterURL: "https://example.test/a.jpg", PosterCropSourceFull: true, PosterCropBounds: &models.CropBounds{Width: .4, Height: 1}}}
	job.results.UpdateFileResult(path, &resultstore.MovieResult{Movie: movie, ErrorCode: downloader.PosterRecropRequiredCode, Error: "rejected", Status: models.JobStatusFailed, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}})
	pe := NewPosterEditor(job.results, job.results, nil)
	require.NoError(t, pe.UpdatePosterFromURL(context.Background(), movie.ID, "https://example.test/b.jpg", ""))
	current, err := job.results.GetMovieResult(path)
	require.NoError(t, err)
	require.Empty(t, current.ErrorCode, "replacing the poster source must clear the recrop block")
	require.Nil(t, current.Movie.Poster.PosterCropBounds, "new source invalidates stored geometry")
}

func TestPosterRecropSkipDownloadRetryProceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	store := resultstore.New(1, []string{path})
	movie := &models.Movie{ID: "CROP-1"}
	stored := &resultstore.MovieResult{Movie: movie, ErrorCode: downloader.PosterRecropRequiredCode, Status: models.JobStatusFailed, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}}
	store.UpdateFileResult(path, stored)
	inputs := minimalApplyInputs(t, store, true)
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: movie}}
	cfg := ApplyPhaseConfig{} // Download: false — NFO/organize-only retry
	cmd, afc, execute := buildApplyCmd(path, movie, stored, inputs, cfg, context.Background())
	require.True(t, execute)
	outcome := applyFile(context.Background(), wf, path, stored, movie, &preparedApplyFile{cmd: cmd, afc: afc, baseline: movie.Clone(), execute: execute}, inputs, cfg)
	require.False(t, outcome.Failed, "skip-download retry must not be blocked by the recrop marker")
	require.Equal(t, 1, wf.getApplyCalled())
	after, err := store.GetMovieResult(path)
	require.NoError(t, err)
	require.Equal(t, downloader.PosterRecropRequiredCode, after.ErrorCode, "marker persists — stale crop intent remains until a fresh measured crop or removal")
}

func TestPosterRecropDryRunRetryProceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	store := resultstore.New(1, []string{path})
	movie := &models.Movie{ID: "CROP-1"}
	stored := &resultstore.MovieResult{Movie: movie, ErrorCode: downloader.PosterRecropRequiredCode, Status: models.JobStatusFailed, FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID}}
	store.UpdateFileResult(path, stored)
	inputs := minimalApplyInputs(t, store, true)
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: movie}}
	cfg := ApplyPhaseConfig{Download: true, DryRun: true}
	cmd, afc, execute := buildApplyCmd(path, movie, stored, inputs, cfg, context.Background())
	require.True(t, execute)
	outcome := applyFile(context.Background(), wf, path, stored, movie, &preparedApplyFile{cmd: cmd, afc: afc, baseline: movie.Clone(), execute: execute}, inputs, cfg)
	require.False(t, outcome.Failed, "dry-run retry must not be blocked by the recrop marker")
	require.Equal(t, 1, wf.getApplyCalled())
}
