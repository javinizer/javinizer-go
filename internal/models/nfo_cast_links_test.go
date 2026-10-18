package models

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNFOActorLinksArePrivateDetachedAndInvalidated(t *testing.T) {
	m := &Movie{Actresses: []Actress{{FirstName: "Old", Translations: []ActressTranslation{{}}}}}
	_, present := m.NFOActorLinks()
	require.False(t, present)
	links := [][]ActorPresentationLink{{{Version: "1", Selection: "override", MovieContentID: "movie", CreditID: "1"}}}
	m.SetNFOActorLinks(links)
	links[0][0].CreditID = "bad"
	got, present := m.NFOActorLinks()
	require.True(t, present)
	require.Equal(t, "1", got[0][0].CreditID)
	got[0][0].CreditID = "bad"
	clone := m.Clone()
	clone.nfoActorLinks.links[0][0].CreditID = "2"
	got, _ = m.NFOActorLinks()
	require.Equal(t, "1", got[0][0].CreditID)
	clone.nfoActorLinks.actors[0].Translations = nil
	require.Len(t, m.nfoActorLinks.actors[0].Translations, 1)
	wire, err := json.Marshal(m)
	require.NoError(t, err)
	var restored Movie
	require.NoError(t, json.Unmarshal(wire, &restored))
	_, present = restored.NFOActorLinks()
	require.False(t, present)
	m.Actresses[0].FirstName = "Changed"
	got, present = m.NFOActorLinks()
	require.True(t, present)
	require.Nil(t, got)
}
