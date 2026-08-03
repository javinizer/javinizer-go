package nfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// TestMergeMovieMetadataWithOptions_DivergentPosterSourceDropsCropBounds pins
// I8: under a prefer-NFO scalar strategy the on-disk NFO's PosterURL/CoverURL
// can win over the source the job state's CropBounds were measured against.
// Bounds measured against one image must not survive onto a different
// effective source — they are dropped and the divergence is provenance-noted.
func TestMergeMovieMetadataWithOptions_DivergentPosterSourceDropsCropBounds(t *testing.T) {
	bounds := &models.CropBounds{X: 11, Y: 22, Width: 300, Height: 400, ImageWidth: 1000, ImageHeight: 1500}
	scraped := &models.Movie{
		ID: "I8A-001",
		Poster: models.PosterState{
			PosterURL:  "https://scraper.example/job-poster.jpg",
			CropBounds: bounds,
		},
	}
	nfo := &models.Movie{
		ID: "I8A-001",
		Poster: models.PosterState{
			PosterURL: "https://nfo.example/different-poster.jpg",
			CoverURL:  "https://nfo.example/different-cover.jpg",
		},
	}

	res, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Merged)

	require.Equal(t, "https://nfo.example/different-poster.jpg", res.Merged.Poster.PosterURL,
		"prefer-nfo must win the source (precondition of this test)")
	assert.Nil(t, res.Merged.Poster.CropBounds,
		"bounds measured against the scraper source must not ride a divergent NFO source (I8)")
	ds, ok := res.Provenance["CropBounds"]
	require.True(t, ok, "the drop must be provenance-noted")
	assert.Equal(t, "dropped-source-divergence", ds.Source)
}

// TestMergeMovieMetadataWithOptions_SamePosterSourceKeepsCropBounds pins the
// other I8 arm: when the merge keeps the SAME effective source the bounds
// were measured against, they stay valid and must be preserved.
func TestMergeMovieMetadataWithOptions_SamePosterSourceKeepsCropBounds(t *testing.T) {
	bounds := &models.CropBounds{X: 11, Y: 22, Width: 300, Height: 400, ImageWidth: 1000, ImageHeight: 1500}
	scraped := &models.Movie{
		ID: "I8B-001",
		Poster: models.PosterState{
			PosterURL:  "https://scraper.example/poster.jpg",
			CropBounds: bounds,
		},
	}
	nfo := &models.Movie{
		ID: "I8B-001",
		Poster: models.PosterState{
			PosterURL: "https://scraper.example/poster.jpg", // identical source
			CoverURL:  "https://nfo.example/cover.jpg",
		},
	}

	res, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, res.Merged)
	require.NotNil(t, res.Merged.Poster.CropBounds, "unchanged effective source keeps the crop bounds")
	assert.Equal(t, *bounds, *res.Merged.Poster.CropBounds)
}

// TestMergeMovieMetadataWithOptions_DivergentPosterSourceResetsCropIntent pins
// r10 P1-2: the I8 divergence guard dropped CropBounds but left the
// ShouldCropPoster intent merged from the SCRAPER side — a bool, so the NFO's
// deliberate false reads as "empty" and loses the merge — pairing a stale
// cover-crop intent with the divergent NFO source. Organize would then
// auto-crop the NFO's poster with a decision measured against the scraper's
// image. On divergence the intent is re-derived from the WINNING (NFO) side.
func TestMergeMovieMetadataWithOptions_DivergentPosterSourceResetsCropIntent(t *testing.T) {
	bounds := &models.CropBounds{X: 11, Y: 22, Width: 300, Height: 400, ImageWidth: 1000, ImageHeight: 1500}
	scraped := &models.Movie{
		ID: "R10-001",
		Poster: models.PosterState{
			CoverURL:         "https://scraper.example/landscape-cover.jpg",
			ShouldCropPoster: true, // cover-crop intent, measured against the scraper cover
			CropBounds:       bounds,
		},
	}
	nfo := &models.Movie{
		ID: "R10-001",
		Poster: models.PosterState{
			PosterURL:        "https://nfo.example/poster-grade.jpg", // wins under prefer-nfo
			ShouldCropPoster: false,                                  // deliberate: no crop for this poster-grade image
		},
	}

	res, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, res.Merged)
	require.Equal(t, "https://nfo.example/poster-grade.jpg", res.Merged.Poster.PosterURL,
		"prefer-nfo must win the source (precondition of this test)")
	assert.Nil(t, res.Merged.Poster.CropBounds, "bounds must still be dropped (I8)")
	assert.False(t, res.Merged.Poster.ShouldCropPoster,
		"the scraper-side crop intent must not ride onto the divergent NFO source (r10 P1-2)")
	ds, ok := res.Provenance["ShouldCropPoster"]
	require.True(t, ok, "the intent reset must be provenance-noted")
	assert.Equal(t, "dropped-source-divergence", ds.Source)
}

// TestMergeMovieMetadataWithOptions_DivergentPosterSourceTakesNFOCropIntent
// pins the mirror arm: on divergence the NFO's own TRUE intent is kept (the
// intent belonging to the winning source, not blindly cleared). Under
// prefer-nfo the NFO's empty PosterURL wins empty (only critical fields
// fall back to the scraper), so the NFO cover becomes the merged effective
// source — divergent from the scraper's poster — and the NFO cover-crop
// intent must ride with it.
func TestMergeMovieMetadataWithOptions_DivergentPosterSourceTakesNFOCropIntent(t *testing.T) {
	scraped := &models.Movie{
		ID: "R10-002",
		Poster: models.PosterState{
			PosterURL:        "https://scraper.example/poster.jpg",
			ShouldCropPoster: false,
		},
	}
	nfo := &models.Movie{
		ID: "R10-002",
		Poster: models.PosterState{
			CoverURL:         "https://nfo.example/landscape-cover.jpg",
			ShouldCropPoster: true, // cover-crop intent measured against the winning NFO cover
		},
	}

	res, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, res.Merged)
	require.Equal(t, "https://nfo.example/landscape-cover.jpg", res.Merged.Poster.CoverURL,
		"the NFO cover must be the merged effective source (precondition of this test)")
	assert.True(t, res.Merged.Poster.ShouldCropPoster,
		"on divergence the intent is re-derived from the winning (NFO) side, true stays true (r10 P1-2)")
	ds, ok := res.Provenance["ShouldCropPoster"]
	require.True(t, ok)
	assert.Equal(t, "nfo", ds.Source)
}

// TestMergeMovieMetadataWithOptions_SamePosterSourceKeepsCropIntent pins that
// an unchanged effective source preserves the scraper-side intent exactly
// (no-divergence arm of the r10 fix).
func TestMergeMovieMetadataWithOptions_SamePosterSourceKeepsCropIntent(t *testing.T) {
	scraped := &models.Movie{
		ID: "R10-003",
		Poster: models.PosterState{
			CoverURL:         "https://cdn.example/cover.jpg",
			ShouldCropPoster: true,
		},
	}
	nfo := &models.Movie{
		ID: "R10-003",
		Poster: models.PosterState{
			CoverURL:         "https://cdn.example/cover.jpg", // identical source
			ShouldCropPoster: false,
		},
	}

	res, err := MergeMovieMetadataWithOptions(scraped, nfo, PreferNFO, false)
	require.NoError(t, err)
	require.NotNil(t, res.Merged)
	assert.True(t, res.Merged.Poster.ShouldCropPoster,
		"unchanged effective source: the scraper/job crop intent stays (no divergence occurred)")
}
