package models

import (
	"reflect"
	"slices"
)

// ActorPresentationLink is untrusted NFO presentation provenance. It is never
// an identity, credit, or database association. Strings preserve malformed XML
// references for a blocking merge diagnostic instead of a parse fallback.
type ActorPresentationLink struct {
	Version        string
	Selection      string
	MovieContentID string
	CreditID       string
}

type nfoActorLinkState struct {
	actors []Actress
	links  [][]ActorPresentationLink
}

func cloneActorLinks(links [][]ActorPresentationLink) [][]ActorPresentationLink {
	result := make([][]ActorPresentationLink, len(links))
	for i := range links {
		result[i] = slices.Clone(links[i])
	}
	return result
}

// SetNFOActorLinks records parsed, presentation-only actor references privately.
func (m *Movie) SetNFOActorLinks(links [][]ActorPresentationLink) {
	m.nfoActorLinks = &nfoActorLinkState{actors: (&Movie{Actresses: m.Actresses}).Clone().Actresses, links: cloneActorLinks(links)}
}

// NFOActorLinks returns detached references only while their actor rows remain
// unchanged. Presence survives invalidation so an edited parsed NFO still gets
// legacy ambiguity checks rather than silently acquiring trusted provenance.
func (m *Movie) NFOActorLinks() ([][]ActorPresentationLink, bool) {
	if m.nfoActorLinks == nil {
		return nil, false
	}
	if !reflect.DeepEqual(m.Actresses, m.nfoActorLinks.actors) {
		return nil, true
	}
	return cloneActorLinks(m.nfoActorLinks.links), true
}
