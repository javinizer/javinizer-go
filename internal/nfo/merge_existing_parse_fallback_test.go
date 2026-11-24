package nfo

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestMergeWithExistingNFOKeepsScraperResultWhenSidecarCannotParse(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := filepath.Join(t.TempDir(), "media")
	require.NoError(t, fs.MkdirAll(dir, 0700))
	sidecar := filepath.Join(dir, "TEST-001.nfo")
	body := []byte(`<movie><title>Incomplete NFO</movie>`)
	require.NoError(t, afero.WriteFile(fs, sidecar, body, 0600))

	impl := NewNFOImplementor(fs, &Config{FilenameTemplate: "<ID>.nfo"}, template.NewEngine())
	movie := &models.Movie{ID: "TEST-001", Title: "Scraped title"}
	result := impl.MergeWithExistingNFO(movie, MergeWithExistingOptions{
		Match:          models.FileMatchInfo{Path: filepath.Join(dir, "TEST-001.mp4")},
		ScalarStrategy: PreferNFO,
	})
	require.NoError(t, result.Err, "an unreadable sidecar is not a cast merge ambiguity")
	require.Same(t, movie, result.Movie)
	require.False(t, result.Merged)
	require.Nil(t, result.MergeStats)
	require.Equal(t, sidecar, result.FoundNFOPath)
	after, err := afero.ReadFile(fs, sidecar)
	require.NoError(t, err)
	require.Equal(t, body, after)
}
