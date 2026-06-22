package models

// OrchestrationState groups the five orchestration-metadata fields shared across
// MovieResult consumers. These fields are NEVER set by the scraper itself —
// only by orchestration steps: DisplayTitle and Persist by the workflow's
// scrapeOrchestrator; Poster generation by the worker's scrape phase;
// Translation by the scraper. Extracting them into one canonical type in the models
// package means adding a new orchestration signal requires editing only this type,
// not every package's local copy and every conversion function.
//
// Both workflow.OrchestrationMeta (which adds NeedsPersistence) and
// worker.OrchestrationState (used for JSON persistence) are defined in terms of
// this type, eliminating the fan-out where a single new field required edits in
// four locations.
type OrchestrationState struct {
	DisplayTitleApplied bool    `json:"display_title_applied,omitempty"` // true if DisplayTitle was applied from the workflow's template
	PosterGenerated     bool    `json:"poster_generated,omitempty"`      // true if poster generation was attempted (check PosterError for outcome)
	Persisted           bool    `json:"persisted,omitempty"`             // true if the scraped movie was persisted to the database
	PosterError         *string `json:"poster_error,omitempty"`          // non-nil if poster generation failed
	TranslationWarning  *string `json:"translation_warning,omitempty"`   // non-nil if translation produced a partial result
}

// Clone returns a deep copy of the OrchestrationState.
func (o *OrchestrationState) Clone() OrchestrationState {
	if o == nil {
		return OrchestrationState{}
	}
	cpy := *o
	if o.PosterError != nil {
		s := *o.PosterError
		cpy.PosterError = &s
	}
	if o.TranslationWarning != nil {
		s := *o.TranslationWarning
		cpy.TranslationWarning = &s
	}
	return cpy
}
