package worker

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// A retry that clears the credit gate can encounter an independent poster
// safety failure. Use the real result store so the frozen input is distinct
// from live writeback, as it is in production.
func TestReviewCreditGateRetryPreservesNewPosterRecropMarker(t *testing.T) {
	for _, priorCode := range []string{"credit_conflict", "credit_status_unavailable"} {
		t.Run(priorCode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "synthetic.mp4")
			store := resultstore.New(1, []string{path})
			movie := &models.Movie{
				ID: "SYNTHETIC-001", ContentID: "synthetic-001",
				Poster: models.PosterState{
					PosterURL:            "https://example.invalid/never-requested.jpg",
					PosterCropSourceFull: true,
					PosterCropBounds:     &models.CropBounds{Width: .4, Height: 1},
				},
			}
			store.UpdateFileResult(path, &resultstore.MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID},
				Movie:         movie, Status: models.JobStatusFailed,
				ErrorCode: priorCode, Error: "prior credit gate failure",
			})
			before, err := store.GetMovieResult(path)
			require.NoError(t, err)
			wf := &stubApplyWorkflow{applyErr: &downloader.PosterRecropRequiredError{
				Reason: downloader.SourceFingerprintMissing,
				Bounds: *movie.Poster.PosterCropBounds,
			}}
			inputs := makeApplyInputs(wf)
			inputs.Updater = store
			inputs.Results = map[string]*resultstore.MovieResult{path: before}
			NewApplyPhase().Run(t.Context(), inputs, ApplyPhaseConfig{
				Destination: t.TempDir(), RetryFilePaths: []string{path},
			})
			after, err := store.GetMovieResult(path)
			require.NoError(t, err)
			require.Equal(t, 1, wf.getApplyCalled())
			require.Equal(t, models.JobStatusFailed, after.Status)
			require.Contains(t, after.Error, "poster")
			require.Equal(t, downloader.PosterRecropRequiredCode, after.ErrorCode,
				"discharging a credit diagnostic must not erase the new poster safety marker")
		})
	}
}
