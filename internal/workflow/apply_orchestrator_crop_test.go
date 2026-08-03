package workflow

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubNFOFileMerger emulates MergeWithExistingNFO returning a freshly merged
// movie (the real merger rebuilds a new Movie and never sees the runtime-only
// crop geometry).
type stubNFOFileMerger struct {
	result nfo.MergeWithExistingResult
}

func (s *stubNFOFileMerger) MergeWithExistingNFO(_ *models.Movie, _ nfo.MergeWithExistingOptions) nfo.MergeWithExistingResult {
	return s.result
}

// stubOrganizerToDest skips real file organization and points the pipeline
// at the requested destination dir, so the integrated crop test exercises
// merge boundary + download steps with real side effects and no moves.
type stubOrganizerToDest struct{}

func (stubOrganizerToDest) Organize(_ context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	return &organizer.OrganizeResult{FolderPath: cmd.DestDir}, nil
}

// Integrated reported-regression check: a movie carrying manual crop geometry
// through Execute comes out on disk cropped per the manual geometry, not the
// scraper default (scraper auto-crop of a landscape cover keeps the RIGHT
// half — white — while the manual geometry takes the LEFT half — black).
func TestApplyExecute_ManualCropGeometryReachesDisk(t *testing.T) {
	// Two-tone source: left half black, right half white.
	src := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			if x < 500 {
				src.Set(x, y, color.Black)
			} else {
				src.Set(x, y, color.White)
			}
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, src, &jpeg.Options{Quality: 95})
	}))
	t.Cleanup(srv.Close)

	fs := afero.NewMemMapFs()
	dl := downloader.NewDownloader(http.DefaultClient, fs, &downloader.Config{
		DownloadPoster:  true,
		MaxPosterHeight: 0,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}, nil)

	movie := &models.Movie{ID: "ABF-346", ContentID: "abf00346", Title: "Integrated Crop"}
	movie.Poster.PosterURL = srv.URL + "/cover.jpg"
	movie.Poster.ShouldCropPoster = true // scraper intent: auto-crop (white)
	movie.Poster.PosterCropBounds = &models.CropBounds{X: 0, Y: 0, Width: 0.4, Height: 1.0, SourceAspect: 1000.0 / 600.0}
	movie.Poster.PosterCropSourceFull = true

	orch := &applyOrchImpl{
		fs:         fs,
		organizer:  stubOrganizerToDest{},
		downloader: dl,
		// nfo nil: merge step is a pass-through for this scenario
		revertLog: noOpRevertLog{},
	}
	result, err := orch.Execute(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    models.FileMatchInfo{Path: "/src/ABF-346.mp4"},
		DestPath: "/dest",
		Download: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	f, err := fs.Open("/dest/ABF-346-poster.jpg")
	require.NoError(t, err, "poster must land in the destination dir")
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	require.NoError(t, err)
	assert.InDelta(t, 400, img.Bounds().Dx(), 2, "manual crop width (0.4 of 1000px)")
	b := img.Bounds()
	x := b.Min.X + b.Dx()/2
	y := b.Min.Y + b.Dy()/2
	r, g, bl, _ := img.At(x, y).RGBA()
	luma := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
	assert.Less(t, luma, 40.0, "on-disk poster must be the manual LEFT crop (black), not the scraper right crop (white)")
}

func cropBoundsFixture() *models.CropBounds {
	return &models.CropBounds{X: 0.1, Y: 0.05, Width: 0.4, Height: 0.9, SourceAspect: 1.5}
}

// A merge that leaves the effective poster source unchanged carries the
// manual crop geometry across — including onto a freshly built merged movie.
func TestApplyMergeBoundary_SourceUnchangedKeepsGeometry(t *testing.T) {
	pre := &models.Movie{ID: "IPX-535"}
	pre.Poster.PosterURL = "https://cdn/p.jpg"
	pre.Poster.PosterCropBounds = cropBoundsFixture()
	pre.Poster.PosterCropSourceFull = true

	freshMerged := &models.Movie{ID: "IPX-535"}
	freshMerged.Poster.PosterURL = "https://cdn/p.jpg"

	orch := &applyOrchImpl{nfo: &stubNFOFileMerger{result: nfo.MergeWithExistingResult{Movie: freshMerged, Merged: true}}}
	state := &applyPipelineState{movie: pre}
	steps := &stepCompletion{}

	require.NoError(t, orch.stepMerge(ApplyCmd{}, state, steps))
	require.True(t, steps.Merged)
	require.Same(t, freshMerged, state.movie)
	require.NotNil(t, state.movie.Poster.PosterCropBounds, "geometry must be carried across the merge")
	assert.Equal(t, *cropBoundsFixture(), *state.movie.Poster.PosterCropBounds)
	assert.True(t, state.movie.Poster.PosterCropSourceFull)
	assert.NotSame(t, pre.Poster.PosterCropBounds, state.movie.Poster.PosterCropBounds, "carry must copy, not alias")
}

// Edge branches of the boundary carry/clear: nil merged movie is a no-op;
// absent, non-full-source, or source-less geometry never applies.
func TestApplyMergeBoundary_EdgeCases(t *testing.T) {
	t.Parallel()

	carryPosterCropAcrossMerge(nil, "https://cdn/p.jpg", cropBoundsFixture(), true) // no panic

	// No pre-merge geometry: merged movie gains nothing.
	merged := &models.Movie{}
	merged.Poster.PosterURL = "https://cdn/p.jpg"
	carryPosterCropAcrossMerge(merged, "", nil, false)
	assert.Nil(t, merged.Poster.PosterCropBounds)
	assert.False(t, merged.Poster.PosterCropSourceFull)

	// Geometry without a poster source is meaningless: never carried.
	merged2 := &models.Movie{}
	carryPosterCropAcrossMerge(merged2, "", cropBoundsFixture(), true)
	assert.Nil(t, merged2.Poster.PosterCropBounds)

	assert.Equal(t, "", effectivePosterSource(nil))
	withCover := &models.Movie{}
	withCover.Poster.CoverURL = "https://cdn/c.jpg"
	assert.Equal(t, "https://cdn/c.jpg", effectivePosterSource(withCover))
}

// A merge that changes the effective poster source clears the geometry so a
// crop measured against the old image is never applied to the new one.
func TestApplyMergeBoundary_SourceChangedClearsGeometry(t *testing.T) {
	pre := &models.Movie{ID: "IPX-535"}
	pre.Poster.PosterURL = "https://cdn/old.jpg"
	pre.Poster.PosterCropBounds = cropBoundsFixture()
	pre.Poster.PosterCropSourceFull = true

	freshMerged := &models.Movie{ID: "IPX-535"}
	freshMerged.Poster.PosterURL = "https://cdn/new.jpg"

	orch := &applyOrchImpl{nfo: &stubNFOFileMerger{result: nfo.MergeWithExistingResult{Movie: freshMerged, Merged: true}}}
	state := &applyPipelineState{movie: pre}

	require.NoError(t, orch.stepMerge(ApplyCmd{}, state, &stepCompletion{}))
	assert.Nil(t, state.movie.Poster.PosterCropBounds)
	assert.False(t, state.movie.Poster.PosterCropSourceFull)
}

// CoverURL is the fallback poster source: changing it while PosterURL is
// empty is also a source change.
func TestApplyMergeBoundary_CoverChangedClearsGeometry(t *testing.T) {
	pre := &models.Movie{ID: "IPX-535"}
	pre.Poster.CoverURL = "https://cdn/old-cover.jpg"
	pre.Poster.PosterCropBounds = cropBoundsFixture()
	pre.Poster.PosterCropSourceFull = true

	freshMerged := &models.Movie{ID: "IPX-535"}
	freshMerged.Poster.CoverURL = "https://cdn/new-cover.jpg"

	orch := &applyOrchImpl{nfo: &stubNFOFileMerger{result: nfo.MergeWithExistingResult{Movie: freshMerged, Merged: true}}}
	state := &applyPipelineState{movie: pre}

	require.NoError(t, orch.stepMerge(ApplyCmd{}, state, &stepCompletion{}))
	assert.Nil(t, state.movie.Poster.PosterCropBounds, "cover source change must clear geometry")
}

// Legacy (non-full-source) geometry is never applied — it does not survive
// the boundary either.
func TestApplyMergeBoundary_NonFullSourceGeometryDropped(t *testing.T) {
	pre := &models.Movie{ID: "IPX-535"}
	pre.Poster.PosterURL = "https://cdn/p.jpg"
	pre.Poster.PosterCropBounds = cropBoundsFixture()
	pre.Poster.PosterCropSourceFull = false

	freshMerged := &models.Movie{ID: "IPX-535"}
	freshMerged.Poster.PosterURL = "https://cdn/p.jpg"

	orch := &applyOrchImpl{nfo: &stubNFOFileMerger{result: nfo.MergeWithExistingResult{Movie: freshMerged, Merged: true}}}
	state := &applyPipelineState{movie: pre}

	require.NoError(t, orch.stepMerge(ApplyCmd{}, state, &stepCompletion{}))
	assert.Nil(t, state.movie.Poster.PosterCropBounds)
}

// Without an NFO merger enabled the merge step is a pass-through: the movie
// (and any geometry) is untouched.
func TestApplyMergeBoundary_NoMergerPassesThrough(t *testing.T) {
	pre := &models.Movie{ID: "IPX-535"}
	pre.Poster.PosterURL = "https://cdn/p.jpg"
	pre.Poster.PosterCropBounds = cropBoundsFixture()
	pre.Poster.PosterCropSourceFull = true

	orch := &applyOrchImpl{}
	state := &applyPipelineState{movie: pre}

	require.NoError(t, orch.stepMerge(ApplyCmd{}, state, &stepCompletion{}))
	require.Same(t, pre, state.movie)
	assert.NotNil(t, state.movie.Poster.PosterCropBounds)
}
