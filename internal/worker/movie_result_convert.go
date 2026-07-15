package worker

import (
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// scrapeResultToMovieResult converts a scrape.ScrapeResult and its OrchestrationMeta
// to a MovieResult. This is the canonical conversion — all ScrapeResult→MovieResult
// paths should use this function to ensure consistent field mapping.
// Provenance is returned separately.
//
// preserveMovieID controls whether fmi.MovieID (set by the matcher/scanner)
// is kept as the grouping key. When true, the matcher-derived ID is preserved
// even if the scraper returns a different content ID — preventing files with
// different input IDs that resolve to the same scraped ID from being grouped
// as multi-part siblings. When false (rescrape path, filename fallback),
// the scraped ID overwrites fmi.MovieID.
func scrapeResultToMovieResult(fmi models.FileMatchInfo, result *scrape.ScrapeResult, meta *workflow.OrchestrationMeta, preserveMovieID bool) (*resultstore.MovieResult, *resultstore.ProvenanceData) {
	if result == nil {
		return nil, nil
	}
	now := time.Now()
	movieID := ""
	if result.Movie != nil {
		movieID = result.Movie.ID
	}
	if !preserveMovieID || fmi.MovieID == "" {
		fmi.MovieID = movieID
	}
	mr := &resultstore.MovieResult{
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

	var prov *resultstore.ProvenanceData
	if result.FieldSources != nil || result.ActressSources != nil || result.ScraperResults != nil {
		prov = &resultstore.ProvenanceData{
			FieldSources:   result.FieldSources,
			ActressSources: result.ActressSources,
			ScraperResults: result.ScraperResults,
		}
	}
	return mr, prov
}
