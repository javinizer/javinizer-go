package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdatePosterFromURL_CropIntentDerivation pins the intent-line fix
// (poster_editor.go): poster-from-URL no longer flattens ShouldCropPoster to
// false. The temp preview DownloadFromURL serves is always auto-cropped, so
// the recorded intent must match how the downloaded image is meant to
// conclude; otherwise the review preview (cropped) and Organize (whole image
// under a false intent) disagree, and resetPoster — which routes the
// poster-restore through this endpoint — drifts away from the scraped
// baseline.
func TestUpdatePosterFromURL_CropIntentDerivation(t *testing.T) {
	const (
		filePath = "/test/intent.mp4"
		movieID  = "INT-001"
		newURL   = "https://example.com/new-poster.jpg"
	)

	setup := func(t *testing.T, prior models.PosterState, prov *resultstore.ProvenanceData) *BatchJob {
		t.Helper()
		job := newBatchJob([]string{filePath})
		job.results.UpdateFileResult(filePath, &resultstore.MovieResult{
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Poster: prior},
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		})
		if prov != nil {
			job.results.SetProvenance(filePath, prov)
		}
		return job
	}

	applyAndRead := func(t *testing.T, job *BatchJob, url string) models.PosterState {
		t.Helper()
		require.NoError(t, job.posterEditor.UpdatePosterFromURL(context.Background(), movieID, url, "https://example.com/new-cropped.jpg"))
		res, err := job.results.GetMovieResult(filePath)
		require.NoError(t, err)
		require.NotNil(t, res.Movie)
		assert.Nil(t, res.Movie.Poster.CropBounds, "poster-from-URL always invalidates measured crop bounds")
		return res.Movie.Poster
	}

	t.Run("prior poster-grade source stays whole-image intent", func(t *testing.T) {
		job := setup(t, models.PosterState{PosterURL: "https://example.com/old-poster.jpg", ShouldCropPoster: false}, nil)
		p := applyAndRead(t, job, newURL)
		assert.False(t, p.ShouldCropPoster,
			"a poster-grade replacement assumes another poster-grade image: Organize writes it whole")
	})

	t.Run("prior cover-backed source keeps cover-crop intent", func(t *testing.T) {
		job := setup(t, models.PosterState{CoverURL: "https://example.com/cover.jpg", ShouldCropPoster: true}, nil)
		p := applyAndRead(t, job, newURL)
		assert.True(t, p.ShouldCropPoster,
			"the auto-cropped preview and Organize's default cover-crop must agree")
	})

	t.Run("prior cover-class explicit poster keeps cover-crop intent", func(t *testing.T) {
		// javdb/mgstage shape: PosterURL points at the landscape cover with
		// ShouldCropPoster=true.
		job := setup(t, models.PosterState{PosterURL: "https://example.com/jacket.jpg", ShouldCropPoster: true}, nil)
		p := applyAndRead(t, job, newURL)
		assert.True(t, p.ShouldCropPoster)
	})

	t.Run("no prior source defaults to cover-crop intent", func(t *testing.T) {
		job := setup(t, models.PosterState{}, nil)
		p := applyAndRead(t, job, newURL)
		assert.True(t, p.ShouldCropPoster,
			"defaulting to the cover-crop keeps the auto-cropped preview and Organize aligned")
	})

	t.Run("known provenance source intent wins over prior-class fallback", func(t *testing.T) {
		// A poster-grade prior would fall back to false, but the selected URL is
		// javdb's landscape cover — that source's crop decision travels with the
		// image (parity with SyncCropIntentWithSource).
		job := setup(t,
			models.PosterState{PosterURL: "https://example.com/old-poster.jpg", ShouldCropPoster: false},
			&resultstore.ProvenanceData{ScraperResults: []*models.ScraperResult{
				{Source: "dmm", PosterURL: "https://example.com/pl.jpg", ShouldCropPoster: false},
				{Source: "javdb", PosterURL: newURL, CoverURL: newURL, ShouldCropPoster: true},
			}},
		)
		p := applyAndRead(t, job, newURL)
		assert.True(t, p.ShouldCropPoster,
			"the recorded source crops this very image; Organize must not write the landscape bytes whole")
	})

	t.Run("known poster-grade provenance source overrides cover-backed prior", func(t *testing.T) {
		job := setup(t,
			models.PosterState{CoverURL: "https://example.com/cover.jpg", ShouldCropPoster: true},
			&resultstore.ProvenanceData{ScraperResults: []*models.ScraperResult{
				{Source: "r18dev", PosterURL: newURL, ShouldCropPoster: false},
			}},
		)
		p := applyAndRead(t, job, newURL)
		assert.False(t, p.ShouldCropPoster,
			"the selected source ships this image as a poster: Organize writes it whole")
	})

	t.Run("unmatched provenance falls back to prior class", func(t *testing.T) {
		job := setup(t,
			models.PosterState{PosterURL: "https://example.com/old-poster.jpg", ShouldCropPoster: false},
			&resultstore.ProvenanceData{ScraperResults: []*models.ScraperResult{
				{Source: "javdb", PosterURL: "https://example.com/other.jpg", ShouldCropPoster: true},
			}},
		)
		p := applyAndRead(t, job, newURL)
		assert.False(t, p.ShouldCropPoster,
			"an intent recorded against a DIFFERENT image is never inherited")
	})

	t.Run("nil provenance entries are skipped before a matching source", func(t *testing.T) {
		// A nil entry must not short-circuit the scan: the intent of the
		// matching source AFTER it still wins (prior is poster-grade, so the
		// fallback alone would answer false).
		job := setup(t,
			models.PosterState{PosterURL: "https://example.com/old-poster.jpg", ShouldCropPoster: false},
			&resultstore.ProvenanceData{ScraperResults: []*models.ScraperResult{
				nil,
				{Source: "javdb", PosterURL: newURL, CoverURL: newURL, ShouldCropPoster: true},
			}},
		)
		p := applyAndRead(t, job, newURL)
		assert.True(t, p.ShouldCropPoster,
			"the scan must continue past nil entries and adopt the matching source's intent")
	})

	t.Run("provenance source without PosterURL falls back to CoverURL for matching", func(t *testing.T) {
		// Sources that only carry a cover URL still participate: the new URL
		// equals the source's CoverURL, and that source ships the image whole
		// (ShouldCropPoster=false). An absent prior would otherwise default to
		// the cover-crop intent.
		job := setup(t,
			models.PosterState{},
			&resultstore.ProvenanceData{ScraperResults: []*models.ScraperResult{
				{Source: "dmm", PosterURL: "", CoverURL: "https://example.com/cover.jpg", ShouldCropPoster: false},
			}},
		)
		p := applyAndRead(t, job, "https://example.com/cover.jpg")
		assert.False(t, p.ShouldCropPoster,
			"the matched source's intact-image intent travels with its CoverURL")
		assert.Equal(t, "https://example.com/cover.jpg", p.PosterURL)
	})

	t.Run("reset-to-baseline is a fixed point for cover-class sources", func(t *testing.T) {
		// resetPoster routes the restore through poster-from-URL with the
		// baseline URL: a cover-class (javdb-style) baseline must come back
		// with its recorded intent or the Reset button never settles.
		job := setup(t,
			models.PosterState{PosterURL: "https://example.com/user-picked.jpg", ShouldCropPoster: true},
			&resultstore.ProvenanceData{ScraperResults: []*models.ScraperResult{
				{Source: "javdb", PosterURL: "https://example.com/baseline.jpg", CoverURL: "https://example.com/baseline.jpg", ShouldCropPoster: true},
			}},
		)
		p := applyAndRead(t, job, "https://example.com/baseline.jpg")
		assert.True(t, p.ShouldCropPoster)
		assert.Equal(t, "https://example.com/baseline.jpg", p.PosterURL)
	})
}
