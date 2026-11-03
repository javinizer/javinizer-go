package nfo

import (
	"testing"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// This extends the existing ASCII fallback fixture through real NFO discovery,
// XML parsing, title remapping and metadata merging, using an owned memory FS.
func TestMergeWithExistingNFOEmptyScrapedTitlePreservesUnicodeFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0o755))
	const path = "/source/MKMP-094.nfo"
	const content = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>[MKMP-094] 題名</title>
  <id>MKMP-094</id>
</movie>`
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))
	impl := nfoImplementor{
		fs:             fs,
		nfoConfig:      &Config{FilenameTemplate: "<ID>.nfo"},
		templateEngine: template.NewEngine(),
	}
	result := impl.MergeWithExistingNFO(&models.Movie{ID: "MKMP-094"}, MergeWithExistingOptions{
		Match:          models.FileMatchInfo{Path: "/source/MKMP-094.mp4", MovieID: "MKMP-094"},
		ScalarStrategy: PreferNFO,
	})
	require.NoError(t, result.Err)
	require.True(t, result.Merged)
	require.Equal(t, path, result.FoundNFOPath)
	require.NotNil(t, result.Movie)
	require.Equal(t, "題名", result.Movie.Title)
	require.True(t, utf8.ValidString(result.Movie.Title))
	remaining, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	require.Equal(t, content, string(remaining), "reading the fallback must not rewrite the source NFO")
}
