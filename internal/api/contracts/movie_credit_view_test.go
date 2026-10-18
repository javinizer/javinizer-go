package contracts

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovieCreditViewFromModel(t *testing.T) {
	assert.Nil(t, MovieCreditViewFromModel(nil))
	assert.Empty(t, MovieCreditViewSliceFromModels(nil))

	view := MovieCreditViewFromModel(&models.MovieCredit{
		ID: 3, ActressID: 4, CreditedName: "Credited", CreditedJapaneseName: "名",
		ReportedThumbURL: "https://thumb", OverrideName: "Override", UserOverride: true, Suppressed: true, OrderIndex: 2,
	})
	require.NotNil(t, view)
	assert.Equal(t, uint(3), view.ID)
	assert.Equal(t, uint(4), view.ActressID)
	assert.Equal(t, "Credited", view.CreditedName)
	assert.Equal(t, "名", view.CreditedJapaneseName)
	assert.Equal(t, "https://thumb", view.ReportedThumbURL)
	assert.Equal(t, "Override", view.OverrideName)
	assert.True(t, view.UserOverride)
	assert.True(t, view.Suppressed)
	assert.Equal(t, 2, view.OrderIndex)

	views := MovieCreditViewSliceFromModels([]models.MovieCredit{{ID: 1}, {ID: 2}})
	require.Len(t, views, 2)
	assert.Equal(t, uint(1), views[0].ID)
}

func TestMovieCreditViewFromModel_RenderVisible(t *testing.T) {
	testCases := []struct {
		name   string
		credit models.MovieCredit
		want   bool
	}{
		{"nil actress", models.MovieCredit{}, false},
		{"verified actress", models.MovieCredit{Actress: &models.Actress{Verified: true}}, true},
		{"unverified unquarantined", models.MovieCredit{Actress: &models.Actress{}}, true},
		{"unverified quarantined", models.MovieCredit{Actress: &models.Actress{AmbiguityQuarantined: true}}, false},
		{"suppressed", models.MovieCredit{Suppressed: true, Actress: &models.Actress{Verified: true}}, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			view := MovieCreditViewFromModel(&tc.credit)
			require.NotNil(t, view)
			assert.Equal(t, tc.want, view.RenderVisible)
		})
	}
}

// This fixture is also consumed by the frontend preview-signature test. Compare
// serialized credits from the real MovieView projection, not hand-built DTOs, so
// dropping a render-relevant field at the API boundary breaks the regression.
func TestMovieViewCreditSelectionPreviewFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/display_title_credit_selection.json")
	require.NoError(t, err)
	var cases []struct {
		Name           string          `json:"name"`
		ForceCanonical bool            `json:"force_canonical"`
		RenderedTitle  string          `json:"rendered_title"`
		Credits        json.RawMessage `json:"credits"`
	}
	require.NoError(t, json.Unmarshal(fixture, &cases))
	require.Len(t, cases, 2)
	actress := models.Actress{ID: 7, FirstName: "Canonical", LastName: "Name", Verified: true}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			movie := models.Movie{
				ID: "PREVIEW-001", ContentID: "preview001", Title: "Title",
				Actresses: []models.Actress{actress},
				Credits: []models.MovieCredit{{
					ID: 9, ActressID: 7, CreditedName: "Stage Alias", Actress: &actress,
					DisplayForceCanonical: tc.ForceCanonical,
				}},
			}
			ctx := template.NewContextFromMovieWithOptions(&movie, template.ContextOptions{
				RenderCredits: true, UseCreditedName: true,
			})
			rendered, err := template.NewEngine().Execute("<ACTRESSES>", ctx)
			require.NoError(t, err)
			assert.Equal(t, tc.RenderedTitle, rendered)
			wire, err := json.Marshal(MovieViewFromModel(&movie))
			require.NoError(t, err)
			var projected struct {
				Credits json.RawMessage `json:"credits"`
			}
			require.NoError(t, json.Unmarshal(wire, &projected))
			assert.JSONEq(t, string(tc.Credits), string(projected.Credits))
		})
	}
}
