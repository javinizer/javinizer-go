package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func challengeRenderStateFixture() (*Movie, []MovieCredit) {
	a := Actress{ID: 3, FirstName: "Durable", Verified: true, Translations: []ActressTranslation{{Language: "en", FirstName: "Original"}}}
	m := &Movie{ContentID: "render-state", RenderGeneration: 9, Actresses: []Actress{a}, Credits: []MovieCredit{{ID: 7, ActressID: 3, Actress: &a, CreditedName: "Durable", Scraped: a}}}
	projection := m.Clone().Credits
	projection[0].OverrideName = "Presentation"
	projection[0].UserOverride = true
	projection = append(projection, MovieCredit{Actress: &Actress{FirstName: "NFO-only"}})
	return m, projection
}

func TestChallengeRenderStateIsolation(t *testing.T) {
	m, projection := challengeRenderStateFixture()
	m.SetRenderCredits(projection)
	projection[0].Actress.Translations[0].FirstName = "Caller mutated"
	projection[1].Actress.FirstName = "Caller mutated"
	got, ok := m.RenderingCredits()
	require.True(t, ok)
	require.Equal(t, "Original", got[0].Actress.Translations[0].FirstName)
	require.Equal(t, "NFO-only", got[1].Actress.FirstName)
	got[0].Actress.Translations[0].FirstName = "Getter mutated"
	got[1].Actress.FirstName = "Getter mutated"
	again, ok := m.RenderingCredits()
	require.True(t, ok)
	require.Equal(t, "Original", again[0].Actress.Translations[0].FirstName)
	require.Equal(t, "NFO-only", again[1].Actress.FirstName)
	clone := m.Clone()
	m.Credits[0].Actress.FirstName = "Original changed"
	cloned, ok := clone.RenderingCredits()
	require.True(t, ok)
	require.Len(t, cloned, 2)
	require.Equal(t, "Durable", cloned[0].Actress.FirstName)
	encoded, err := json.Marshal(clone)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "NFO-only")
	require.NotContains(t, string(encoded), "Presentation")
	var decoded Movie
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	roundtrip, ok := decoded.RenderingCredits()
	require.True(t, ok)
	require.Len(t, roundtrip, 1)
	want := clone.Clone().Credits
	want[0].Scraped = Actress{} // Existing field is intentionally excluded from JSON.
	require.Equal(t, want, roundtrip)
}

func TestChallengeRenderStateInvalidation(t *testing.T) {
	for name, mutate := range map[string]func(*Movie){
		"generation":  func(m *Movie) { m.RenderGeneration++ },
		"suppression": func(m *Movie) { m.Credits[0].Suppressed = true },
		"override":    func(m *Movie) { m.Credits[0].UserOverride = true; m.Credits[0].OverrideName = "Later" },
		"canonical":   func(m *Movie) { m.Credits[0].Actress.FirstName = "Later" },
		"legacy cast": func(m *Movie) { m.Actresses[0].ThumbURL = "Later" },
	} {
		t.Run(name, func(t *testing.T) {
			m, projection := challengeRenderStateFixture()
			m.SetRenderCredits(projection)
			mutate(m)
			got, ok := m.RenderingCredits()
			require.True(t, ok)
			require.Equal(t, m.Credits, got, "changed authority must discard stale NFO-only rows")
		})
	}
}

func TestChallengeRenderStateExplicitEmpty(t *testing.T) {
	m := &Movie{Actresses: []Actress{{FirstName: "Stale"}}}
	m.SetRenderCredits(nil)
	got, ok := m.RenderingCredits()
	require.True(t, ok, "explicit empty projection must not fall back to legacy cast")
	require.Empty(t, got)
	clone := m.Clone()
	got, ok = clone.RenderingCredits()
	require.True(t, ok)
	require.Empty(t, got)
}
