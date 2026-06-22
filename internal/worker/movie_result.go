package worker

import (
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ProvenanceData holds per-file provenance (field and actress source attribution)
// separated from MovieResult per ADR-0027. Provenance is an API-presentation concern;
// the apply phase never reads it.
type ProvenanceData struct {
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
}

// Clone returns a deep copy of the ProvenanceData.
func (p *ProvenanceData) Clone() *ProvenanceData {
	if p == nil {
		return nil
	}
	copied := *p
	if p.FieldSources != nil {
		copied.FieldSources = make(map[string]string, len(p.FieldSources))
		for k, v := range p.FieldSources {
			copied.FieldSources[k] = v
		}
	}
	if p.ActressSources != nil {
		copied.ActressSources = make(map[string]string, len(p.ActressSources))
		for k, v := range p.ActressSources {
			copied.ActressSources[k] = v
		}
	}
	return &copied
}

// The Clone method lives on models.OrchestrationState and is promoted through
// the embedded field — no local Clone method needed.

// MovieResult represents the result of processing a single file.
// It replaces the legacy FileResult, using a typed *models.Movie field
// instead of the untyped Data any field.
// Provenance (FieldSources/ActressSources) has been moved to ProvenanceData
// per ADR-0027 — use ResultTracker.Provenance to look up provenance by file path.
type MovieResult struct {
	ResultID      string               `json:"result_id"` // Stable UUID — survives movie_id changes from rescrape/edit
	FileMatchInfo models.FileMatchInfo `json:"file_match_info"`
	Movie         *models.Movie        `json:"movie,omitempty"` // typed, replaces Data any
	Revision      uint64               `json:"revision"`
	Status        models.JobStatus     `json:"status"`
	Error         string               `json:"error,omitempty"`
	StartedAt     time.Time            `json:"started_at"`
	EndedAt       *time.Time           `json:"ended_at,omitempty"`

	// Orchestration metadata propagated from ScrapeResult (ADR-0015).
	// Embedded with json:",inline" so the serialized shape is unchanged for
	// backward-compatible job persistence deserialization.
	models.OrchestrationState `json:",inline"`
}

// scrapeResultToMovieResult converts a scrape.ScrapeResult and its OrchestrationMeta
// to a MovieResult. This is the canonical conversion — all ScrapeResult→MovieResult
// paths should use this function to ensure consistent field mapping.
// Provenance is returned separately per ADR-0027.
func scrapeResultToMovieResult(fmi models.FileMatchInfo, result *scrape.ScrapeResult, meta *workflow.OrchestrationMeta) (*MovieResult, *ProvenanceData) {
	if result == nil {
		return nil, nil
	}
	now := time.Now()
	movieID := ""
	if result.Movie != nil {
		movieID = result.Movie.ID
	}
	fmi.MovieID = movieID
	mr := &MovieResult{
		ResultID:      uuid.New().String(),
		FileMatchInfo: fmi,
		Status:        models.JobStatusCompleted,
		Movie:         result.Movie,
		StartedAt:     result.StartedAt,
		EndedAt:       &now,
	}
	if meta != nil {
		// Struct copy from OrchestrationMeta's embedded OrchestrationState.
		// A single assignment replaces the five individual field copies.
		// NeedsPersistence is intentionally excluded — it's a transient
		// workflow-internal signal that is cleared during persistence.
		mr.OrchestrationState = meta.OrchestrationState
	}

	var prov *ProvenanceData
	if result.FieldSources != nil || result.ActressSources != nil {
		prov = &ProvenanceData{
			FieldSources:   result.FieldSources,
			ActressSources: result.ActressSources,
		}
	}
	return mr, prov
}

// Clone returns a deep copy of the MovieResult.
// It delegates Movie deep-copying to models.Movie.Clone() and
// OrchestrationState deep-copying to OrchestrationState.Clone().
func (mr *MovieResult) Clone() *MovieResult {
	if mr == nil {
		return nil
	}
	dst := *mr
	if mr.EndedAt != nil {
		t := *mr.EndedAt
		dst.EndedAt = &t
	}
	dst.OrchestrationState = mr.OrchestrationState.Clone()
	if mr.Movie != nil {
		dst.Movie = mr.Movie.Clone()
	}
	return &dst
}
