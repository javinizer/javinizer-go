package models

import "reflect"

// actorRenderState adapts presentation-only actors to the common credit
// renderers without putting them in Movie.Credits or an identity repository.
type actorRenderState struct {
	source     *Movie
	credits    []MovieCredit
	generation int64
}

func cloneRenderCredits(credits []MovieCredit) []MovieCredit {
	return (&Movie{Credits: credits}).Clone().Credits
}

// SetRenderCredits installs a detached, transient cast projection. Imported
// rows must carry no identity IDs or verification status. An empty projection
// intentionally renders no cast; it is distinct from having no projection.
func (m *Movie) SetRenderCredits(credits []MovieCredit) {
	m.actorRenderState = &actorRenderState{
		source:     (&Movie{ID: m.ID, ContentID: m.ContentID, Credits: m.Credits, Actresses: m.Actresses}).Clone(),
		credits:    cloneRenderCredits(credits),
		generation: m.RenderGeneration,
	}
}

// RenderingCredits returns a detached render cast and whether it is
// authoritative for presentation. Credit, identity, legacy-cast, or generation
// changes invalidate a previous NFO projection automatically. Reloading or
// serializing a Movie also discards the private projection.
func (m *Movie) RenderingCredits() ([]MovieCredit, bool) {
	if m == nil {
		return nil, false
	}
	state := m.actorRenderState
	if state != nil && state.generation == m.RenderGeneration && state.source.ID == m.ID && state.source.ContentID == m.ContentID &&
		reflect.DeepEqual(state.source.Credits, m.Credits) &&
		reflect.DeepEqual(state.source.Actresses, m.Actresses) {
		return cloneRenderCredits(state.credits), true
	}
	return cloneRenderCredits(m.Credits), len(m.Credits) > 0
}
