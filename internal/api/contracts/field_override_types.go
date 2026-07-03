package contracts

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

// FieldOverrideRequest is the body for POST
// /api/v1/batch/{id}/results/{resultId}/field-override. The user cherry-picks a
// single field's value from the named source's raw scraper results, overwriting
// the aggregated value (mirrors the original Javinizer "Replace" button).
type FieldOverrideRequest struct {
	// Field is the canonical field-source key to override (e.g. "maker",
	// "title", "actresses", "genres"). Must be one of the keys surfaced by the
	// source viewer / field_sources map.
	Field string `json:"field" binding:"required" example:"maker"`
	// Source is the scraper source name whose raw value should win the field
	// (e.g. "dmm", "r18dev"). Must be one of the sources that contributed to
	// this movie.
	Source string `json:"source" binding:"required" example:"dmm"`
}

// FieldOverrideResponse is the updated movie + provenance after a field override
// is applied and persisted.
type FieldOverrideResponse struct {
	Movie          *MovieView        `json:"movie"`
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
}

// SourceResultsResponse is the raw per-scraper results for a movie, used by the
// review-page source viewer to render each source's fields and offer per-field
// "Use this" overrides. ScraperResults are persisted in the job envelope and
// survive server restarts. A synthesized single-source fallback is returned
// only for legacy envelopes persisted before this feature or when provenance
// was never set.
type SourceResultsResponse struct {
	Results []*models.ScraperResult `json:"results"`
}
