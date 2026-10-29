package nfo_test

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Legacy/imported movies can contain a name-only cast entry alongside the
// same performer with a provider ID. Default generation must not duplicate
// that actor or discard a different actor following the duplicate.
func TestDefaultNFORoundtripMixedCastIdentity(t *testing.T) {
	for _, tc := range []struct {
		name              string
		firstID, secondID int
	}{
		{"name only before provider identity", 0, 101},
		{"provider identity before name only", 101, 0},
		{"different provider identities with the same name", 101, 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			movie := &models.Movie{
				ID: "TEST-001", ContentID: "test00001", Title: "Fixture & title",
				Actresses: []models.Actress{
					{DMMID: tc.firstID, FirstName: "Alice", LastName: "Example", ThumbURL: "https://example.test/first.jpg"},
					{DMMID: tc.secondID, FirstName: "ALICE", LastName: "EXAMPLE", ThumbURL: "https://example.test/duplicate.jpg"},
					{DMMID: 303, FirstName: "Betty", LastName: "Example", ThumbURL: "https://example.test/distinct.jpg"},
				},
			}
			before := *movie
			before.Actresses = append([]models.Actress(nil), movie.Actresses...)
			root := t.TempDir()
			nameCfg := nfo.NFONameConfigFromAppConfig(nil)
			require.Equal(t, nfo.NFONameConfig{}, nameCfg)
			generator := nfo.NewGenerator(afero.NewOsFs(), nil)
			var firstXML []byte
			for attempt := 0; attempt < 2; attempt++ {
				path, err := generator.ResolveAndGenerate(context.Background(), movie, root, nameCfg, "", []string{"retained tag"})
				require.NoError(t, err)
				require.Equal(t, filepath.ToSlash(filepath.Join(root, "TEST-001.nfo")), path)
				data, err := os.ReadFile(filepath.FromSlash(path))
				require.NoError(t, err)
				var decoded nfo.Movie
				require.NoError(t, xml.Unmarshal(data, &decoded))
				require.Equal(t, "movie", decoded.XMLName.Local)
				require.Equal(t, movie.ID, decoded.ID)
				require.Equal(t, movie.Title, decoded.Title)
				require.Equal(t, []string{"retained tag"}, decoded.Tags)
				require.Len(t, decoded.Actors, 2)
				require.Equal(t, "Alice Example", decoded.Actors[0].Name)
				require.Equal(t, "https://example.test/first.jpg", decoded.Actors[0].Thumb)
				require.Equal(t, 0, decoded.Actors[0].Order)
				require.Equal(t, "Betty Example", decoded.Actors[1].Name)
				require.Equal(t, "https://example.test/distinct.jpg", decoded.Actors[1].Thumb)
				require.Equal(t, 1, decoded.Actors[1].Order)
				require.Equal(t, before, *movie, "generation must not rewrite caller-owned cast metadata")
				if attempt == 0 {
					firstXML = data
				} else {
					require.Equal(t, firstXML, data, "reusing the generator must produce the same cast and XML")
				}
			}
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			require.Len(t, entries, 1, "only the requested sidecar should remain after repeated generation")
			require.Equal(t, "TEST-001.nfo", entries[0].Name())
		})
	}
}
