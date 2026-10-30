package nfo

import (
	"context"
	"encoding/xml"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Configured text is rendered and then validated: aliases must survive both
// stages, not just produce a value through the template engine directly.
func TestActressAliasContractConfiguredNFO(t *testing.T) {
	for _, alias := range []string{"Actress", "actorname", "ACTRESSNAME"} {
		for _, tc := range []struct {
			name      string
			actresses []models.Actress
			ja        bool
			first     bool
			want      string
		}{
			{"last first", []models.Actress{{FirstName: "Momo", LastName: "Sakura", JapaneseName: "桜空もも"}}, false, false, "Sakura Momo"},
			{"first last", []models.Actress{{FirstName: "Momo", LastName: "Sakura", JapaneseName: "桜空もも"}}, false, true, "Momo Sakura"},
			{"Japanese", []models.Actress{{FirstName: "Momo", LastName: "Sakura", JapaneseName: "桜空もも"}}, true, false, "桜空もも"},
			{"empty", nil, false, false, ""},
		} {
			t.Run(alias+"/"+tc.name, func(t *testing.T) {
				fs := afero.NewMemMapFs()
				tmpl := "<IF:" + alias + ">name=<" + alias + "><ELSE>no actress</IF>"
				g := NewGenerator(fs, &Config{Tagline: tmpl, Tag: []string{tmpl, "static"}, ActressLanguageJA: tc.ja, FirstNameOrder: tc.first})
				movie := &models.Movie{ID: "TEST-001", Title: "Alias contract", Actresses: tc.actresses}
				require.NoError(t, g.GenerateAtPath(context.Background(), movie, "/alias.nfo", "", nil))
				data, err := afero.ReadFile(fs, "/alias.nfo")
				require.NoError(t, err)
				var got Movie
				require.NoError(t, xml.Unmarshal(data, &got))
				want := "no actress"
				if tc.want != "" {
					want = "name=" + tc.want
				}
				require.Equal(t, want, got.Tagline)
				require.ElementsMatch(t, []string{want, "static"}, got.Tags)
			})
		}
	}
}
