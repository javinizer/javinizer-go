package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeRescrapeMovie_CropBoundsEffectiveSource pins the crop-state
// invariant for merge-enabled rescrapes: CropBounds and ShouldCropPoster are
// measured against the effective poster source (PosterURL ?? CoverURL), so
// when reconciliation keeps the existing effective source the stored manual
// crop and its intent must survive — the merge engine takes CropBounds from
// the scraped movie (never populated), which would otherwise silently drop a
// user-approved crop measured against the very image being retained, and
// Organize would then save that image uncropped (the crop's
// ShouldCropPoster=false reset means nothing re-derives crop intent).
func TestMergeRescrapeMovie_CropBoundsEffectiveSource(t *testing.T) {
	croppedExisting := func() *models.Movie {
		m := existingMovie()
		// A completed manual crop: bounds measured against the current image,
		// auto-crop intent deliberately reset.
		m.Poster.CropBounds = &models.CropBounds{X: 10, Y: 20, Width: 300, Height: 450}
		m.Poster.ShouldCropPoster = false
		return m
	}

	tests := []struct {
		name           string
		existing       func() *models.Movie
		scraped        func() *models.Movie
		wantBounds     *models.CropBounds
		wantShouldCrop bool
	}{
		{
			name:     "scraper returns no images: existing manual crop and intent preserved",
			existing: croppedExisting,
			scraped: func() *models.Movie {
				// No poster/cover; ShouldCropPoster=true is adversarial — a
				// source-retaining merge must still keep the stored intent.
				return &models.Movie{ID: "OLD-001", Title: "Scraped", Poster: models.PosterState{ShouldCropPoster: true}}
			},
			wantBounds:     &models.CropBounds{X: 10, Y: 20, Width: 300, Height: 450},
			wantShouldCrop: false,
		},
		{
			name:     "scraper re-finds the identical poster URL: effective source unchanged, crop still preserved",
			existing: croppedExisting,
			scraped: func() *models.Movie {
				return &models.Movie{ID: "OLD-001", Title: "Scraped", Poster: models.PosterState{
					PosterURL:        "https://old.invalid/poster.jpg",
					ShouldCropPoster: true,
				}}
			},
			wantBounds:     &models.CropBounds{X: 10, Y: 20, Width: 300, Height: 450},
			wantShouldCrop: false,
		},
		{
			name:     "new poster URL: source changed, crop cleared, scraper intent carried",
			existing: croppedExisting,
			scraped: func() *models.Movie {
				return &models.Movie{ID: "OLD-001", Title: "Scraped", Poster: models.PosterState{
					PosterURL:        "https://new.invalid/poster.jpg",
					ShouldCropPoster: true,
				}}
			},
			wantBounds:     nil,
			wantShouldCrop: true,
		},
		{
			name: "cover-only change with PosterURL empty: effective source changed, crop cleared",
			existing: func() *models.Movie {
				m := existingMovie()
				m.Poster.PosterURL = "" // cover-backed: CoverURL feeds the pipeline
				m.Poster.CropBounds = &models.CropBounds{X: 10, Y: 20, Width: 300, Height: 450}
				m.Poster.ShouldCropPoster = false
				return m
			},
			scraped: func() *models.Movie {
				return &models.Movie{ID: "OLD-001", Title: "Scraped", Poster: models.PosterState{
					CoverURL:         "https://new.invalid/cover.jpg",
					ShouldCropPoster: true,
				}}
			},
			wantBounds:     nil,
			wantShouldCrop: true,
		},
		{
			name: "cover-only rescrape that keeps the same cover: poster-backed source unchanged, crop preserved",
			existing: func() *models.Movie {
				m := existingMovie()
				m.Poster.PosterURL = ""
				m.Poster.CropBounds = &models.CropBounds{X: 1, Y: 2, Width: 100, Height: 150}
				m.Poster.ShouldCropPoster = false
				return m
			},
			scraped: func() *models.Movie {
				return &models.Movie{ID: "OLD-001", Title: "Scraped", Poster: models.PosterState{
					CoverURL:         "https://old.invalid/cover.jpg",
					ShouldCropPoster: true,
				}}
			},
			wantBounds:     &models.CropBounds{X: 1, Y: 2, Width: 100, Height: 150},
			wantShouldCrop: false,
		},
		{
			name: "existing movie has no crop: stays nil, no churn",
			existing: func() *models.Movie {
				m := existingMovie()
				m.Poster.ShouldCropPoster = true
				return m
			},
			scraped: func() *models.Movie {
				return &models.Movie{ID: "OLD-001", Title: "Scraped"}
			},
			wantBounds:     nil,
			wantShouldCrop: true, // stored intent kept with the retained source
		},
		{
			name:     "content-id change with fresh images: crop measured against old content is cleared",
			existing: croppedExisting,
			scraped: func() *models.Movie {
				return scrapedNewID()
			},
			wantBounds:     nil,
			wantShouldCrop: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			merged := mergeRescrapeMovie(tc.existing(), tc.scraped(), workflow.MergeOptions{
				ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
			}, "file.mp4")
			if tc.wantBounds == nil {
				assert.Nil(t, merged.Poster.CropBounds)
			} else {
				require.NotNil(t, merged.Poster.CropBounds)
				assert.Equal(t, *tc.wantBounds, *merged.Poster.CropBounds)
			}
			assert.Equal(t, tc.wantShouldCrop, merged.Poster.ShouldCropPoster)
		})
	}
}

// The preserved bounds must be a copy, not an alias of the existing movie's
// pointer — later edits through the merged result must not mutate the
// stored existing movie.
func TestMergeRescrapeMovie_PreservedCropBoundsIsDeepCopy(t *testing.T) {
	existing := existingMovie()
	existing.Poster.CropBounds = &models.CropBounds{X: 10, Y: 20, Width: 300, Height: 450}
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped"}

	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	require.NotNil(t, merged.Poster.CropBounds)

	merged.Poster.CropBounds.X = 999
	assert.Equal(t, 10, existing.Poster.CropBounds.X, "merged CropBounds must not alias existing")
}
