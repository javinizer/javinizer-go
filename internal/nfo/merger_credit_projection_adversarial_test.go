package nfo

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func challengeRenderCast(t *testing.T, m *models.Movie, useCredited bool) (Movie, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, &Config{FirstNameOrder: true, UseCreditedName: useCredited, IncludeActressImages: true, Tagline: "<ACTORS>"})
	require.NoError(t, g.GenerateAtPath(context.Background(), m, "/cast.nfo", "", nil))
	body, err := afero.ReadFile(fs, "/cast.nfo")
	require.NoError(t, err)
	var result Movie
	require.NoError(t, xml.Unmarshal(body, &result))
	ctx := template.NewContextFromMovieWithOptions(m, template.ContextOptions{RenderCredits: true, FirstNameOrder: true, UseCreditedName: useCredited})
	names, err := template.NewEngine().Execute("<ACTORS>", ctx)
	require.NoError(t, err)
	return result, names
}

func challengeSourceMovie() *models.Movie {
	a := models.Actress{ID: 31, FirstName: "Alpha", JapaneseName: "甲", Verified: true}
	b := models.Actress{ID: 32, FirstName: "Beta", LastName: "Person", JapaneseName: "乙", Verified: true, ThumbURL: "https://beta.invalid/face.jpg"}
	return &models.Movie{ID: "CHECK-001", ContentID: "check001", Title: "Synthetic", RenderGeneration: 7, RenderDirty: true,
		Actresses: []models.Actress{b},
		Credits: []models.MovieCredit{
			{ID: 41, ActressID: a.ID, Actress: &a, CreditedName: "Alpha Stage", OverrideName: "Alpha Override", UserOverride: true, Suppressed: true, OrderIndex: 0},
			{ID: 42, ActressID: b.ID, Actress: &b, CreditedName: "Beta Stage", OrderIndex: 1},
		},
	}
}

func TestChallengeNFOProjectionUnionAmbiguity(t *testing.T) {
	m := challengeSourceMovie()
	before := m.Clone()
	// Japanese and romanized keys point at different owners. Neither field
	// may win by lookup order, and the hidden owner must stay hidden.
	n := &models.Movie{ID: m.ID, Actresses: []models.Actress{{FirstName: "Beta", LastName: "Person", JapaneseName: "甲", ThumbURL: "https://wrong.invalid/face.jpg"}}}
	merged, err := MergeMovieMetadataWithOptions(m, n, PreferNFO, true)
	require.NoError(t, err)
	rendered, names := challengeRenderCast(t, merged.Merged, false)
	require.Len(t, rendered.Actors, 1)
	require.Equal(t, "Beta Person", rendered.Actors[0].Name)
	require.Equal(t, "https://beta.invalid/face.jpg", rendered.Actors[0].Thumb)
	require.Equal(t, "Beta Person", names)
	ja := NewGenerator(afero.NewMemMapFs(), &Config{ActressLanguageJA: true}).buildActorsForMovie(merged.Merged)
	require.Len(t, ja, 1)
	require.Equal(t, "乙", ja[0].Name, "ambiguous NFO fields cannot replace a source identity's presentation")
	require.Equal(t, before.Credits, merged.Merged.Credits, "render merge cannot rewrite identity authority")
	require.Equal(t, before, m, "merge cannot mutate its source")
}

func TestChallengeNFOProjectionOverrideCanonicalCollision(t *testing.T) {
	m := challengeSourceMovie()
	m.Credits[0].OverrideName = "Shared Alias"
	m.Credits[1].Actress.FirstName = "Shared"
	m.Credits[1].Actress.LastName = "Alias"
	m.Actresses = []models.Actress{*m.Credits[1].Actress}
	n := &models.Movie{ID: m.ID, Actresses: []models.Actress{{FirstName: "Ｓｈａｒｅｄ", LastName: " Alias  ", JapaneseName: "別名"}}}
	merged, err := MergeMovieMetadataWithOptions(m, n, PreferNFO, true)
	require.NoError(t, err)
	rendered, names := challengeRenderCast(t, merged.Merged, false)
	require.Len(t, rendered.Actors, 1)
	require.Equal(t, "Shared Alias", rendered.Actors[0].Name)
	require.Equal(t, "Shared Alias", names)
	ja := NewGenerator(afero.NewMemMapFs(), &Config{ActressLanguageJA: true}).buildActorsForMovie(merged.Merged)
	require.Len(t, ja, 1)
	require.Equal(t, "乙", ja[0].Name)
}

func TestChallengeNFOProjectionRepeatedSuppressedAndNewNames(t *testing.T) {
	m := challengeSourceMovie()
	n := &models.Movie{ID: m.ID, Actresses: []models.Actress{
		{FirstName: "Alpha", LastName: "Override"}, {FirstName: "Alpha", LastName: "Stage"}, {JapaneseName: "甲"},
		{FirstName: "New", LastName: "Face"}, {FirstName: "New", LastName: "Face"},
	}}
	merged, err := MergeMovieMetadataWithOptions(m, n, PreferNFO, true)
	require.NoError(t, err)
	rendered, names := challengeRenderCast(t, merged.Merged, true)
	require.Len(t, rendered.Actors, 2)
	require.Equal(t, "Beta Stage", rendered.Actors[0].Name)
	require.Equal(t, "New Face", rendered.Actors[1].Name)
	require.Contains(t, names, "Beta Stage")
	require.Contains(t, names, "New Face")
	require.NotContains(t, names, "Alpha")
	require.Equal(t, m.Credits, merged.Merged.Credits)
	wire, err := json.Marshal(merged.Merged)
	require.NoError(t, err)
	var restored models.Movie
	require.NoError(t, json.Unmarshal(wire, &restored))
	require.Equal(t, m.Credits, restored.Credits, "render-only NFO cast must not enter serialized credits")
}

func TestChallengeNFOProjectionInvalidatesAfterUserMutation(t *testing.T) {
	m := challengeSourceMovie()
	n := &models.Movie{ID: m.ID, Actresses: []models.Actress{{FirstName: "New", LastName: "Face"}}}
	merged, err := MergeMovieMetadataWithOptions(m, n, PreferNFO, true)
	require.NoError(t, err)
	require.Len(t, merged.Merged.Credits, 2)
	merged.Merged.Credits[1].Suppressed = true
	rendered, names := challengeRenderCast(t, merged.Merged, false)
	require.Empty(t, rendered.Actors, "stale presentation cannot outlive changed authoritative credits")
	require.Empty(t, names)
}

func TestChallengeNFOProjectionHiddenAliasBridgeIsOrderIndependent(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "alias first", true: "linked row first"}[reverse], func(t *testing.T) {
			m := challengeSourceMovie()
			actors := []models.Actress{{FirstName: "Temporary", LastName: "Face"}, {FirstName: "Temporary", LastName: "Face", JapaneseName: "甲"}}
			if reverse {
				actors[0], actors[1] = actors[1], actors[0]
			}
			merged, err := MergeMovieMetadataWithOptions(m, &models.Movie{ID: m.ID, Actresses: actors}, PreferNFO, true)
			require.NoError(t, err)
			rendered, names := challengeRenderCast(t, merged.Merged, false)
			require.Len(t, rendered.Actors, 1, "all connected NFO rows must respect a suppressed source regardless of row order")
			require.Equal(t, "Beta Person", rendered.Actors[0].Name)
			require.NotContains(t, names, "Temporary")
		})
	}
}
