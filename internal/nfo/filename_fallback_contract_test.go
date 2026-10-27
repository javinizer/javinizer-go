package nfo

import (
	"context"
	"encoding/xml"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Filename resolution is also used to capture the NFO for Undo. A fallback
// must therefore name exactly the safe sidecar that Generate actually writes.
func TestNFOFilenameFallbackMatchesGeneratedSidecar(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template string
		movieID  string
		wantBase string
	}{
		{"empty sanitized template uses sanitized ID", " ?* .. ", " SAFE/001?* ", "SAFE-001"},
		{"empty sanitized template and ID use default", " ?* .. ", " ?* .. ", "metadata"},
		{"extension only uses sanitized ID", ".nfo", " SAFE\\001?* ", "SAFE-001"},
		{"extension only and empty sanitized ID use default", ".NFO", " ?* .. ", "metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, perFile := range []bool{false, true} {
				name := "shared sidecar"
				if perFile {
					name = "multipart sidecar"
				}
				t.Run(name, func(t *testing.T) {
					fs := afero.NewMemMapFs()
					cfg := &Config{FilenameTemplate: tc.template, PerFile: perFile}
					movie := &models.Movie{ID: tc.movieID, DisplayTitle: "A & B <original>"}
					const suffix = "-pt2"
					want := tc.wantBase + ".nfo"
					if perFile {
						want = tc.wantBase + suffix + ".nfo"
					}
					resolved := ResolveNFOFilename(template.NewEngine(), movie, cfg.ToNFONameConfig(true, suffix, 2))
					require.Equal(t, want, resolved)
					require.Equal(t, resolved, filepath.Base(resolved))
					require.NotContains(t, resolved, "/")
					require.NotContains(t, resolved, "\\")

					root := t.TempDir()
					outputDir := filepath.Join(root, "sidecars")
					require.NoError(t, NewGenerator(fs, cfg).Generate(context.Background(), movie, outputDir, suffix, "", []string{"preserved tag"}))
					entries, err := afero.ReadDir(fs, outputDir)
					require.NoError(t, err)
					require.Len(t, entries, 1)
					require.Equal(t, resolved, entries[0].Name())
					require.False(t, entries[0].IsDir())
					parentEntries, err := afero.ReadDir(fs, root)
					require.NoError(t, err)
					require.Len(t, parentEntries, 1, "generation must not escape the sidecar directory")
					require.Equal(t, "sidecars", parentEntries[0].Name())

					data, err := afero.ReadFile(fs, filepath.Join(outputDir, resolved))
					require.NoError(t, err)
					var decoded Movie
					require.NoError(t, xml.Unmarshal(data, &decoded), "filename fallback must still write valid NFO XML")
					require.Equal(t, "movie", decoded.XMLName.Local)
					require.Equal(t, movie.ID, decoded.ID)
					require.Equal(t, movie.DisplayTitle, decoded.Title)
					require.Equal(t, []string{"preserved tag"}, decoded.Tags)
				})
			}
		})
	}
}
