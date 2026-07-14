package worker

import (
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ProvenanceData holds per-file provenance (field and actress source attribution)
// separated from MovieResult. Provenance is an API-presentation concern;
// the apply phase never reads it.
type ProvenanceData struct {
	FieldSources   map[string]string `json:"field_sources,omitempty"`
	ActressSources map[string]string `json:"actress_sources,omitempty"`
	// ScraperResults holds the raw per-scraper results that produced the
	// aggregated Movie, retained so the review page can offer per-field source
	// overrides without a re-scrape. Persisted in the job envelope (unlike the
	// original in-memory-only design) so the multi-scraper source view survives
	// a backend restart — the review window routinely spans restarts. Served
	// via the dedicated /sources endpoint, not the main job response.
	ScraperResults []*models.ScraperResult `json:"scraper_results,omitempty"`
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
	if p.ScraperResults != nil {
		copied.ScraperResults = make([]*models.ScraperResult, len(p.ScraperResults))
		for i, sr := range p.ScraperResults {
			copied.ScraperResults[i] = sr.Clone()
		}
	}
	return &copied
}

// The Clone method lives on models.OrchestrationState and is promoted through
// the embedded field — no local Clone method needed.

// MovieResult represents the result of processing a single file.
// It replaces the legacy FileResult, using a typed *models.Movie field
// instead of the untyped Data any field.
// Provenance (FieldSources/ActressSources) has been moved to ProvenanceData —
// use ResultTracker.Provenance to look up provenance by file path.
type MovieResult struct {
	ResultID      string               `json:"result_id"` // Stable UUID — survives movie_id changes from rescrape/edit
	FileMatchInfo models.FileMatchInfo `json:"file_match_info"`
	Movie         *models.Movie        `json:"movie,omitempty"` // typed, replaces Data any
	Revision      uint64               `json:"revision"`
	Status        models.JobStatus     `json:"status"`
	Error         string               `json:"error,omitempty"`
	StartedAt     time.Time            `json:"started_at"`
	EndedAt       *time.Time           `json:"ended_at,omitempty"`

	// Orchestration metadata propagated from ScrapeResult.
	// Embedded with json:",inline" so the serialized shape is unchanged for
	// backward-compatible job persistence deserialization.
	models.OrchestrationState `json:",inline"`
}

// scrapeResultToMovieResult converts a scrape.ScrapeResult and its OrchestrationMeta
// to a MovieResult. This is the canonical conversion — all ScrapeResult→MovieResult
// paths should use this function to ensure consistent field mapping.
// Provenance is returned separately.
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
	// Populate OriginalFileName from the source file so templates (e.g. NFO
	// <FILENAME>) resolve; the scrape workflow builds the movie from the ID only.
	if result.Movie != nil && result.Movie.OriginalFileName == "" && fmi.Name != "" {
		result.Movie.OriginalFileName = fmi.Name
	}
	if meta != nil {
		// Struct copy from OrchestrationMeta's embedded OrchestrationState.
		// A single assignment replaces the five individual field copies.
		// NeedsPersistence is intentionally excluded — it's a transient
		// workflow-internal signal that is cleared during persistence.
		mr.OrchestrationState = meta.OrchestrationState
	}

	var prov *ProvenanceData
	if result.FieldSources != nil || result.ActressSources != nil || result.ScraperResults != nil {
		prov = &ProvenanceData{
			FieldSources:   result.FieldSources,
			ActressSources: result.ActressSources,
			ScraperResults: result.ScraperResults,
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
