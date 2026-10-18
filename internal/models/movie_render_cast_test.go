package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMovieRenderCastIsTransientDetachedAndInvalidated(t *testing.T) {
	identity := Actress{ID: 7, FirstName: "Canonical", Verified: true, Translations: []ActressTranslation{{DisplayName: "Translation"}}}
	for _, change := range []string{"credit", "identity", "legacy", "generation"} {
		t.Run(change, func(t *testing.T) {
			movie := &Movie{RenderGeneration: 9, Actresses: []Actress{identity}, Credits: []MovieCredit{{ActressID: 7, Actress: &identity, CreditedName: "Reported"}}}
			projection := []MovieCredit{{Actress: &Actress{FirstName: "NFO", Translations: []ActressTranslation{{DisplayName: "NFO translation"}}}}}
			movie.SetRenderCredits(projection)
			projection[0].Actress.FirstName = "Mutated setter input"
			got, present := movie.RenderingCredits()
			require.True(t, present)
			require.Equal(t, "NFO", got[0].Actress.FirstName)
			got[0].Actress.FirstName = "Mutated getter result"
			clone := movie.Clone()
			clone.actorRenderState.credits[0].Actress.Translations[0].DisplayName = "Mutated clone"
			clone.actorRenderState.source.Credits[0].CreditedName = "Mutated clone source"
			require.Equal(t, "NFO translation", movie.actorRenderState.credits[0].Actress.Translations[0].DisplayName)
			require.Equal(t, "Reported", movie.actorRenderState.source.Credits[0].CreditedName)
			raw, err := json.Marshal(movie)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "NFO")
			var roundTrip Movie
			require.NoError(t, json.Unmarshal(raw, &roundTrip))
			rendered, _ := roundTrip.RenderingCredits()
			require.Equal(t, "Canonical", rendered[0].Actress.FirstName)
			switch change {
			case "credit":
				movie.Credits[0].Suppressed = true
			case "identity":
				movie.Credits[0].Actress = &Actress{FirstName: "Changed"}
			case "legacy":
				movie.Actresses = []Actress{{FirstName: "Changed"}}
			case "generation":
				movie.RenderGeneration++
			}
			rendered, present = movie.RenderingCredits()
			require.True(t, present)
			require.Equal(t, movie.Credits, rendered, "stale render projection must be discarded")
		})
	}
}

func TestMovieRenderCastEmptyPresenceAndNilFallback(t *testing.T) {
	var nilMovie *Movie
	credits, present := nilMovie.RenderingCredits()
	require.Nil(t, credits)
	require.False(t, present)
	movie := &Movie{Actresses: []Actress{{FirstName: "Legacy"}}}
	credits, present = movie.RenderingCredits()
	require.Nil(t, credits)
	require.False(t, present)
	movie.SetRenderCredits([]MovieCredit{})
	credits, present = movie.RenderingCredits()
	require.True(t, present)
	require.NotNil(t, credits)
	require.Empty(t, credits)
	movie.Actresses[0].FirstName = "Changed"
	_, present = movie.RenderingCredits()
	require.False(t, present)
}

func TestRenderProjectionCannotSurviveMovieRekey(t *testing.T) {
	for _, field := range []string{"ID", "ContentID"} {
		t.Run(field, func(t *testing.T) {
			movie := &Movie{ID: "CAST-001", ContentID: "cast001", Credits: []MovieCredit{{CreditedName: "Source"}}}
			movie.SetRenderCredits([]MovieCredit{{CreditedName: "Projected"}, {CreditedName: "NFO-only"}})
			if field == "ID" {
				movie.ID = "CAST-002"
			} else {
				movie.ContentID = "cast002"
			}
			got, present := movie.RenderingCredits()
			require.True(t, present)
			require.Equal(t, movie.Credits, got)
		})
	}
}
