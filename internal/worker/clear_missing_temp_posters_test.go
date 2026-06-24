package worker

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearMissingTempPosters_ClearsStaleURL(t *testing.T) {
	fs := afero.NewMemMapFs()
	// No file created at /tmp/posters/job-1/ABP-731.jpg → URL must be cleared.
	results := map[string]*MovieResult{
		"/v/ABP-731.mp4": {
			Movie: &models.Movie{
				ID: "ABP-731",
				Poster: models.PosterState{
					PosterURL:        "https://example.com/poster.jpg",
					CroppedPosterURL: "/api/v1/temp/posters/job-1/ABP-731.jpg?v=1",
				},
			},
		},
	}

	ClearMissingTempPosters(fs, "/tmp", "job-1", results)

	r := results["/v/ABP-731.mp4"]
	require.NotNil(t, r.Movie)
	assert.Empty(t, r.Movie.Poster.CroppedPosterURL, "stale cropped URL should be cleared")
	assert.Equal(t, "https://example.com/poster.jpg", r.Movie.Poster.PosterURL,
		"remote poster_url must be preserved as the frontend fallback")
}

func TestClearMissingTempPosters_PreservesURLWhenFileExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	posterPath := filepath.Join("/tmp", "posters", "job-1", "ABP-731.jpg")
	require.NoError(t, fs.MkdirAll(filepath.Dir(posterPath), 0o755))
	require.NoError(t, afero.WriteFile(fs, posterPath, []byte("x"), 0o644))

	cropped := "/api/v1/temp/posters/job-1/ABP-731.jpg?v=1"
	results := map[string]*MovieResult{
		"/v/ABP-731.mp4": {
			Movie: &models.Movie{
				ID: "ABP-731",
				Poster: models.PosterState{
					CroppedPosterURL: cropped,
				},
			},
		},
	}

	ClearMissingTempPosters(fs, "/tmp", "job-1", results)

	assert.Equal(t, cropped, results["/v/ABP-731.mp4"].Movie.Poster.CroppedPosterURL,
		"existing temp poster URL must be preserved")
}

func TestClearMissingTempPosters_NoOpWhenTempDirEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()
	cropped := "/api/v1/temp/posters/job-1/ABP-731.jpg?v=1"
	results := map[string]*MovieResult{
		"/v/ABP-731.mp4": {
			Movie: &models.Movie{
				ID: "ABP-731",
				Poster: models.PosterState{
					CroppedPosterURL: cropped,
				},
			},
		},
	}

	ClearMissingTempPosters(fs, "", "job-1", results)

	assert.Equal(t, cropped, results["/v/ABP-731.mp4"].Movie.Poster.CroppedPosterURL,
		"empty temp dir is a no-op (e.g. legacy records without temp_dir)")
}

func TestClearMissingTempPosters_SkipsEmptyCroppedURL(t *testing.T) {
	fs := afero.NewMemMapFs()
	results := map[string]*MovieResult{
		"/v/ABP-731.mp4": {
			Movie: &models.Movie{
				ID:     "ABP-731",
				Poster: models.PosterState{CroppedPosterURL: ""},
			},
		},
		"/v/nil.mp4": {Movie: nil},
	}

	// Should not panic and should be a no-op.
	ClearMissingTempPosters(fs, "/tmp", "job-1", results)
	assert.Empty(t, results["/v/ABP-731.mp4"].Movie.Poster.CroppedPosterURL)
}

func TestClearMissingTempPosters_DirExistsButFileMissing(t *testing.T) {
	fs := afero.NewMemMapFs()
	posterDir := filepath.Join("/tmp", "posters", "job-1")
	require.NoError(t, fs.MkdirAll(posterDir, 0o755))
	// Only ABP-731's poster exists; ABP-980's is missing.
	require.NoError(t, afero.WriteFile(fs, filepath.Join(posterDir, "ABP-731.jpg"), []byte("x"), 0o644))

	results := map[string]*MovieResult{
		"/v/ABP-731.mp4": {Movie: &models.Movie{ID: "ABP-731", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/job-1/ABP-731.jpg?v=1",
		}}},
		"/v/ABP-980.mp4": {Movie: &models.Movie{ID: "ABP-980", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/job-1/ABP-980.jpg?v=2",
		}}},
	}

	ClearMissingTempPosters(fs, "/tmp", "job-1", results)

	assert.Equal(t, "/api/v1/temp/posters/job-1/ABP-731.jpg?v=1",
		results["/v/ABP-731.mp4"].Movie.Poster.CroppedPosterURL, "existing poster preserved")
	assert.Empty(t, results["/v/ABP-980.mp4"].Movie.Poster.CroppedPosterURL,
		"missing poster cleared via single ReadDir batch check")
}
