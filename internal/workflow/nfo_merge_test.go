package workflow

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeWithExistingNFO_ForceOverwrite_SkipsMerge(t *testing.T) {
	fs := afero.NewMemMapFs()
	nfoConfig := &nfo.Config{
		FilenameTemplate: "<ID>.nfo",
		GroupActress:     false,
	}

	movie := &models.Movie{ID: "ABC-123", Title: "Scraped Title"}
	match := models.FileMatchInfo{Path: "/source/ABC-123.mp4", MovieID: "ABC-123"}

	// Per ADR-0033: NFOInterface carries its own infrastructure deps.
	nfoIface := nfo.NewNFOImplementor(fs, nfoConfig, template.NewEngine())
	result := nfoIface.MergeWithExistingNFO(movie, nfo.MergeWithExistingOptions{
		Match:          match,
		ForceOverwrite: true,
	})

	assert.False(t, result.Merged, "ForceOverwrite should skip merge")
	assert.Equal(t, movie, result.Movie, "Movie should be unchanged")
	assert.Equal(t, "", result.FoundNFOPath, "No NFO path should be found when skipping")
}

func TestMergeWithExistingNFO_NoExistingNFO(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))

	nfoConfig := &nfo.Config{
		FilenameTemplate: "<ID>.nfo",
		GroupActress:     false,
	}

	movie := &models.Movie{ID: "ABC-123", Title: "Scraped Title"}
	match := models.FileMatchInfo{Path: "/source/ABC-123.mp4", MovieID: "ABC-123"}

	nfoIface := nfo.NewNFOImplementor(fs, nfoConfig, template.NewEngine())
	result := nfoIface.MergeWithExistingNFO(movie, nfo.MergeWithExistingOptions{
		Match: match,
	})

	assert.False(t, result.Merged, "Should not merge when no NFO exists")
	assert.Equal(t, "", result.FoundNFOPath, "FoundNFOPath should be empty")
	assert.Equal(t, "Scraped Title", result.Movie.Title, "Title should remain from scraped data")
}

func TestMergeWithExistingNFO_PreserveNFO_PreservesExistingFields(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))

	nfoContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>Existing Title</title>
  <id>ABC-123</id>
</movie>`
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.nfo", []byte(nfoContent), 0644))

	nfoConfig := &nfo.Config{
		FilenameTemplate: "<ID>.nfo",
		GroupActress:     false,
	}

	movie := &models.Movie{ID: "ABC-123", Title: "Scraped Title"}
	match := models.FileMatchInfo{Path: "/source/ABC-123.mp4", MovieID: "ABC-123"}

	nfoIface := nfo.NewNFOImplementor(fs, nfoConfig, template.NewEngine())
	result := nfoIface.MergeWithExistingNFO(movie, nfo.MergeWithExistingOptions{
		Match:       match,
		PreserveNFO: true,
	})

	assert.True(t, result.Merged, "Should merge when existing NFO found")
	assert.Equal(t, filepath.FromSlash("/source/ABC-123.nfo"), result.FoundNFOPath, "FoundNFOPath should be set")
	assert.Equal(t, "Existing Title", result.Movie.Title, "Existing title should be preserved with PreserveNFO")
}

func TestMergeWithExistingNFO_PreferScraper_UsesScrapedValues(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))

	nfoContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>Existing Title</title>
  <id>ABC-123</id>
</movie>`
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.nfo", []byte(nfoContent), 0644))

	nfoConfig := &nfo.Config{
		FilenameTemplate: "<ID>.nfo",
		GroupActress:     false,
	}

	movie := &models.Movie{ID: "ABC-123", Title: "Scraped Title"}
	match := models.FileMatchInfo{Path: "/source/ABC-123.mp4", MovieID: "ABC-123"}

	nfoIface := nfo.NewNFOImplementor(fs, nfoConfig, template.NewEngine())
	result := nfoIface.MergeWithExistingNFO(movie, nfo.MergeWithExistingOptions{
		Match:          match,
		ScalarStrategy: nfo.PreferScraper,
	})
	assert.True(t, result.Merged, "Should merge when existing NFO found")
	assert.Equal(t, "Scraped Title", result.Movie.Title, "Scraped title should win with PreferScraper strategy")
}

func TestMergeWithExistingNFO_MalformedNFO_ReturnsUnmerged(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))

	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.nfo", []byte("this is not valid XML <<>>"), 0644))

	nfoConfig := &nfo.Config{
		FilenameTemplate: "<ID>.nfo",
		GroupActress:     false,
	}

	movie := &models.Movie{ID: "ABC-123", Title: "Scraped Title"}
	match := models.FileMatchInfo{Path: "/source/ABC-123.mp4", MovieID: "ABC-123"}

	nfoIface := nfo.NewNFOImplementor(fs, nfoConfig, template.NewEngine())
	result := nfoIface.MergeWithExistingNFO(movie, nfo.MergeWithExistingOptions{
		Match: match,
	})

	assert.False(t, result.Merged, "Should not merge when NFO is malformed")
	assert.Equal(t, "Scraped Title", result.Movie.Title, "Title should remain from scraped data")
	assert.Equal(t, filepath.FromSlash("/source/ABC-123.nfo"), result.FoundNFOPath, "FoundNFOPath should still be set even for malformed NFO")
}
