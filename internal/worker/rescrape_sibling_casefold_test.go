package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRescrapePhase_SiblingPosterMirrorHandlesCaseVariant pins the folded
// identity guard in the I7 sibling poster fan-out (Codex P2): multipart
// siblings whose movie IDs differ only in CASE (ABC-1 vs abc-1) share ONE
// folded result-index family and ONE poster-source lock key, so
// FindFilePathsForMovieID returns BOTH — but a raw-string guard in the
// mirror closure skipped the case-variant sibling while the rescrape
// replaced the shared {movieID}-full.jpg cache, leaving that sibling's
// poster source/intent/bounds measured against the OLD image. The sibling's
// own identity (Movie.ID, per-part fields) must survive the mirror; only the
// Poster group is cloned across.
func TestRescrapePhase_SiblingPosterMirrorHandlesCaseVariant(t *testing.T) {
	const movieID = "CASE-001"
	const variantID = "case-001" // same folded family as movieID
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + variantID + "-cd2.mp4"
	jobID := models.NewJobID()

	tracker := resultstore.New(2, []string{fileCD1, fileCD2})
	tracker.UpdateFileResult(fileCD1, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileCD1, MovieID: movieID},
		Movie: &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{
			PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
		}},
		Status: models.JobStatusCompleted,
	})
	tracker.UpdateFileResult(fileCD2, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileCD2, MovieID: variantID},
		Movie: &models.Movie{ID: variantID, Title: "Old Sibling", OriginalTitle: "variant-original", Poster: models.PosterState{
			PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
		}},
		Status: models.JobStatusCompleted,
	})

	// Sanity: the folded index already treats the variant as one family —
	// the fan-out loop therefore REACHES the sibling and only the guard can
	// skip it.
	require.ElementsMatch(t, []string{fileCD1, fileCD2}, tracker.FindFilePathsForMovieID(movieID))

	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: &stubOverridePosterGen{stampCroppedURL: "/api/v1/temp/posters/" + jobID.String() + "/" + movieID + ".jpg?v=7"},
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: movieID, FilePath: fileCD1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, models.RescrapeStatusSuccess, res.Status, "res: %+v", res)

	sibling, err := tracker.GetMovieResult(fileCD2)
	require.NoError(t, err)
	require.NotNil(t, sibling.Movie)
	assert.Equal(t, variantID, sibling.Movie.ID,
		"the mirror clones ONLY the poster group — the sibling keeps its own case-variant identity")
	assert.Equal(t, "variant-original", sibling.Movie.OriginalTitle,
		"per-part fields survive the poster mirror")
	assert.Equal(t, "https://new.example/poster.jpg", sibling.Movie.Poster.PosterURL,
		"a case-variant sibling shares the folded cache the rescrape replaced — its poster state must converge too")

	assertPosterSourceLockFree(t, jobID.String(), movieID)
	assertPosterSourceLockFree(t, jobID.String(), variantID)
}

// TestRescrapePhase_CaseVariantSiblingRekeyStillGuarded pins the RETAINED
// rekey guard beside the fold: a sibling whose Live identity raced to a
// DIFFERENT (non-folded-equal) key mid-write is still skipped — folded
// equality relaxes only case, never a genuine re-key.
func TestRescrapePhase_CaseVariantSiblingRekeyStillGuarded(t *testing.T) {
	const movieID = "GRD-001"
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + movieID + "-cd2.mp4"
	jobID := models.NewJobID()

	tracker := resultstore.New(2, []string{fileCD1, fileCD2})
	for _, fp := range []string{fileCD1, fileCD2} {
		tracker.UpdateFileResult(fp, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: movieID},
			Movie: &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{
				PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
			}},
			Status: models.JobStatusCompleted,
		})
	}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	store := &siblingRacedRekeyStore{Store: tracker, sibPath: fileCD2, armed: true}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: &stubOverridePosterGen{stampCroppedURL: "/api/v1/temp/posters/" + jobID.String() + "/" + movieID + ".jpg?v=9"},
		ResultMap: store,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: movieID, FilePath: fileCD1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, models.RescrapeStatusSuccess, res.Status, "res: %+v", res)

	sibling, err := tracker.GetMovieResult(fileCD2)
	require.NoError(t, err)
	assert.Equal(t, "RACED-999", sibling.Movie.ID)
	assert.Equal(t, "https://old.example/poster.jpg", sibling.Movie.Poster.PosterURL,
		"a genuinely re-keyed sibling must still be left unmirrorred")

	assertPosterSourceLockFree(t, jobID.String(), movieID)
	assertPosterSourceLockFree(t, jobID.String(), "RACED-999")
}
