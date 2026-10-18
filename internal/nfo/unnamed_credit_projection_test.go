package nfo

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCreditPresentationCompoundOptions(t *testing.T) {
	for _, kind := range []string{"bilingual", "last-only", "japanese-only", "unnamed"} {
		for _, ja := range []bool{false, true} {
			for _, first := range []bool{false, true} {
				for _, mode := range []models.UnknownActressMode{models.UnknownActressModeSkip, models.UnknownActressModeFallback} {
					for _, choice := range []string{"canonical", "credited", "override", "force", "force-override", "canonical-credit-fallback"} {
						t.Run(fmt.Sprintf("%s/ja=%v/first=%v/%s/%s", kind, ja, first, mode, choice), func(t *testing.T) {
							a := models.Actress{ID: 1, FirstName: "Given", LastName: "Family", JapaneseName: "日本名", Verified: true}
							switch kind {
							case "last-only":
								a.FirstName = ""
								a.JapaneseName = ""
							case "japanese-only":
								a.FirstName = ""
								a.LastName = ""
							case "unnamed":
								a.FirstName = ""
								a.LastName = ""
								a.JapaneseName = ""
							}
							c := models.MovieCredit{ID: 2, MovieContentID: "presentation", ActressID: 1, Actress: &a}
							if choice != "canonical" {
								c.CreditedName = "  Reported Alias  "
								c.CreditedJapaneseName = "報告名"
							}
							if choice == "override" || choice == "force-override" {
								c.UserOverride = true
								c.OverrideName = " Selected Alias "
							}
							if choice == "force" || choice == "force-override" {
								c.DisplayForceCanonical = true
							}
							useCredited := choice != "canonical" && choice != "canonical-credit-fallback"
							m := &models.Movie{ID: "PRESENTATION", ContentID: "presentation", Credits: []models.MovieCredit{c}, Actresses: []models.Actress{a}}
							before := m.Clone()
							g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<ACTORS>", ActressLanguageJA: ja, FirstNameOrder: first, UseCreditedName: useCredited, UnknownActressMode: mode, UnknownActressText: "Unidentified", IncludeActressImages: false})
							out, err := g.movieToNFO(context.Background(), m, "", "", 0, false, nil)
							require.NoError(t, err)
							want := models.FormatActressName(a, models.FormatActressNameOptions{JapaneseNames: ja, FirstNameOrder: first, UnknownActressMode: models.UnknownActressModeSkip})
							selection := "canonical"
							if c.UserOverride {
								want = "Selected Alias"
								selection = "override"
							} else if (useCredited && !c.DisplayForceCanonical) || (want == "" && c.CreditedName != "") {
								want = "Reported Alias"
								selection = "credited"
							}
							wantTagline := want
							if want == "" && mode == models.UnknownActressModeFallback {
								want = "Unidentified"
							}
							if want == "" {
								require.Empty(t, out.Actors)
							} else {
								require.Len(t, out.Actors, 1)
								require.Equal(t, want, out.Actors[0].Name)
								require.Len(t, out.Actors[0].PresentationLinks, 1)
								require.Equal(t, selection, out.Actors[0].PresentationLinks[0].Selection)
							}
							require.Equal(t, wantTagline, out.Tagline)
							require.Equal(t, before, m)
						})
					}
				}
			}
		}
	}
}
