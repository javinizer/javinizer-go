package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemapParsedNFOTitleForMerge_MovesTitleToDisplayTitle(t *testing.T) {
	movie := &models.Movie{ID: "MKMP-094", Title: "[MKMP-094] Ayaka Tomoda"}
	RemapParsedNFOTitleForMerge(movie, "Ayaka Tomoda")
	assert.Equal(t, "", movie.Title, "Title is cleared so the merge treats the NFO <title> as a display title")
	assert.Equal(t, "[MKMP-094] Ayaka Tomoda", movie.DisplayTitle, "NFO <title> is carried as the display title")
}

func TestRemapParsedNFOTitleForMerge_KeepsExistingDisplayTitle(t *testing.T) {
	movie := &models.Movie{ID: "MKMP-094", Title: "ignored", DisplayTitle: "already set"}
	RemapParsedNFOTitleForMerge(movie, "anything")
	assert.Equal(t, "ignored", movie.Title)
	assert.Equal(t, "already set", movie.DisplayTitle)
}

func TestMergePreferNFO_CodePrefixedNFOTitleDoesNotPolluteTitle(t *testing.T) {
	scraped := &models.Movie{ID: "MKMP-094", ContentID: "mkmp094", Title: "Ayaka Tomoda"}
	nfoMovie := &models.Movie{ID: "MKMP-094", ContentID: "mkmp094", Title: "[MKMP-094] Ayaka Tomoda"}

	RemapParsedNFOTitleForMerge(nfoMovie, scraped.Title)
	require.Equal(t, "", nfoMovie.Title)
	require.Equal(t, "[MKMP-094] Ayaka Tomoda", nfoMovie.DisplayTitle)

	result, err := MergeMovieMetadataWithOptions(scraped, nfoMovie, PreferNFO, false)
	require.NoError(t, err)
	assert.Equal(t, "Ayaka Tomoda", result.Merged.Title, "PreferNFO must not pollute base Title with the code-prefixed NFO display title")
	assert.Equal(t, "[MKMP-094] Ayaka Tomoda", result.Merged.DisplayTitle, "NFO display title is preserved in DisplayTitle")
}

func TestMergeWithExistingNFO_PreferNFO_CodePrefixedTitleDoesNotPolluteTitle(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))

	nfoContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>[MKMP-094] Ayaka Tomoda</title>
  <id>MKMP-094</id>
</movie>`
	require.NoError(t, afero.WriteFile(fs, "/source/MKMP-094.nfo", []byte(nfoContent), 0644))

	nfoImpl := nfoImplementor{
		fs:             fs,
		nfoConfig:      &Config{FilenameTemplate: "<ID>.nfo"},
		templateEngine: template.NewEngine(),
	}

	scraped := &models.Movie{ID: "MKMP-094", Title: "Ayaka Tomoda"}
	result := nfoImpl.MergeWithExistingNFO(scraped, MergeWithExistingOptions{
		Match:          models.FileMatchInfo{Path: "/source/MKMP-094.mp4", MovieID: "MKMP-094"},
		ScalarStrategy: PreferNFO,
	})

	require.True(t, result.Merged, "should merge with existing NFO")
	assert.Equal(t, "Ayaka Tomoda", result.Movie.Title, "PreferNFO must keep the clean scraped base Title")
	assert.Equal(t, "[MKMP-094] Ayaka Tomoda", result.Movie.DisplayTitle, "code-prefixed NFO <title> lands in DisplayTitle")
}

func TestRemapParsedNFOTitleForMerge_NilMovieDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		RemapParsedNFOTitleForMerge(nil, "")
	})
}

// TestRemapParsedNFOTitleForMerge_PreservesStrippedFallbackWhenScrapedEmpty
// guards codex P2 finding 2: when the scraper returned no base title (sparse
// result or temporary title-extraction failure), moving the NFO <title> to
// DisplayTitle would empty both merge inputs and the critical-field safety
// valve would fall back to "[Unknown Title]". Instead the code prefix is
// stripped and the title is kept in Title as the base-title fallback.
func TestRemapParsedNFOTitleForMerge_PreservesStrippedFallbackWhenScrapedEmpty(t *testing.T) {
	cases := []struct {
		name  string
		title string
		id    string
		want  string
	}{
		{"bracketed code prefix", "[MKMP-094] Ayaka Tomoda", "MKMP-094", "Ayaka Tomoda"},
		{"unbracketed code prefix", "MKMP-094 Ayaka Tomoda", "MKMP-094", "Ayaka Tomoda"},
		{"no code prefix stays verbatim", "Ayaka Tomoda", "MKMP-094", "Ayaka Tomoda"},
		{"empty movie id leaves title verbatim", "[MKMP-094] Ayaka Tomoda", "", "[MKMP-094] Ayaka Tomoda"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			movie := &models.Movie{ID: tc.id, Title: tc.title}
			RemapParsedNFOTitleForMerge(movie, "")
			assert.Equal(t, tc.want, movie.Title, "Title keeps a stripped base-title fallback")
			assert.Equal(t, "", movie.DisplayTitle, "DisplayTitle is not populated when serving as fallback")
		})
	}
}

// TestMergeWithExistingNFO_EmptyScrapedTitlePreservesNFOFallback asserts the
// end-to-end merge path: a scraper that returned no title still produces a
// usable merged Title (the stripped NFO <title>), never "[Unknown Title]".
func TestMergeWithExistingNFO_EmptyScrapedTitlePreservesNFOFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))

	nfoContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>[MKMP-094] Ayaka Tomoda</title>
  <id>MKMP-094</id>
</movie>`
	require.NoError(t, afero.WriteFile(fs, "/source/MKMP-094.nfo", []byte(nfoContent), 0644))

	nfoImpl := nfoImplementor{
		fs:             fs,
		nfoConfig:      &Config{FilenameTemplate: "<ID>.nfo"},
		templateEngine: template.NewEngine(),
	}

	scraped := &models.Movie{ID: "MKMP-094", Title: ""}
	result := nfoImpl.MergeWithExistingNFO(scraped, MergeWithExistingOptions{
		Match:          models.FileMatchInfo{Path: "/source/MKMP-094.mp4", MovieID: "MKMP-094"},
		ScalarStrategy: PreferNFO,
	})

	require.True(t, result.Merged, "should merge with existing NFO")
	assert.Equal(t, "Ayaka Tomoda", result.Movie.Title, "stripped NFO <title> serves as base-title fallback")
	assert.NotEqual(t, "[Unknown Title]", result.Movie.Title)
}
