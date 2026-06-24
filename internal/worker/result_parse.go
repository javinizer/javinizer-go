package worker

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ParsedJobResults holds the parsed results and provenance from a job's
// persisted Results JSON column. Returned by ParseJobResultsJSON.
type ParsedJobResults struct {
	Results    map[string]*MovieResult
	Provenance map[string]*ProvenanceData
}

// ParseJobResultsJSON parses the Results JSON column from the database,
// handling all three persistence formats:
//
//  1. New envelope format {"domain": ..., "provenance": ...} — ADR-0027
//  2. Legacy FileResult format with "data_type" key and "data" field
//  3. Old MovieResult format with nested "file_match_info"
//
// This function centralizes the format-detection logic that was previously
// duplicated between reconstructBatchJob and parseAndConvertJobResults.
// Both callers should use this function and then convert the output to
// their target type.
func ParseJobResultsJSON(raw []byte) (*ParsedJobResults, error) {
	if len(raw) == 0 {
		return &ParsedJobResults{
			Results:    make(map[string]*MovieResult),
			Provenance: make(map[string]*ProvenanceData),
		}, nil
	}

	// Probe top-level keys instead of substring matching. Substring matching
	// with bytes.Contains can false-positive when "domain" or "data_type"
	// appear as values inside nested fields (e.g. a movie title), causing
	// silent data loss via incorrect format routing.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err == nil {
		// Format 1: envelope format has a top-level "domain" key.
		if _, ok := probe["domain"]; ok {
			return parseEnvelopeFormat(raw)
		}
		// Formats 2 & 3 are both maps of file-path → object. Distinguish by
		// checking whether ANY value object has a "data_type" key (legacy
		// FileResult format) versus "file_match_info"/"movie" (old MovieResult).
		// Scan past nil/non-data_type entries rather than breaking on the first
		// parsed value: Go map iteration order is randomized, and a null entry
		// unmarshals to a nil map (no error, no data_type key), which would
		// prematurely break and misroute a legacy payload to
		// parseOldMovieResultFormat, losing legacy "data" decoding.
		hasLegacyDataType := false
		for _, v := range probe {
			var valueProbe map[string]json.RawMessage
			if json.Unmarshal(v, &valueProbe) == nil {
				if _, ok := valueProbe["data_type"]; ok {
					hasLegacyDataType = true
					break
				}
			}
		}
		if hasLegacyDataType {
			return parseLegacyFileResultFormat(raw)
		}
	}

	// Format 3: Old MovieResult format with nested "file_match_info"
	return parseOldMovieResultFormat(raw)
}

// parseEnvelopeFormat parses the new envelope format {"domain": ..., "provenance": ...} (ADR-0027).
func parseEnvelopeFormat(raw []byte) (*ParsedJobResults, error) {
	var envelope JobResultsEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("envelope format: %w", err)
	}
	if envelope.Domain == nil {
		envelope.Domain = make(map[string]*MovieResult)
	}
	if envelope.Provenance == nil {
		envelope.Provenance = make(map[string]*ProvenanceData)
	}
	return &ParsedJobResults{Results: envelope.Domain, Provenance: envelope.Provenance}, nil
}

// parseLegacyFileResultFormat parses the legacy FileResult format with "data_type" key.
func parseLegacyFileResultFormat(raw []byte) (*ParsedJobResults, error) {
	type legacyFileResult struct {
		FilePath           string            `json:"file_path"`
		MovieID            string            `json:"movie_id"`
		Revision           uint64            `json:"revision"`
		Status             models.JobStatus  `json:"status"`
		Error              string            `json:"error,omitempty"`
		PosterError        *string           `json:"poster_error,omitempty"`
		TranslationWarning *string           `json:"translation_warning,omitempty"`
		FieldSources       map[string]string `json:"field_sources,omitempty"`
		ActressSources     map[string]string `json:"actress_sources,omitempty"`
		ResultID           string            `json:"result_id"`
		Data               json.RawMessage   `json:"data,omitempty"`
		StartedAt          time.Time         `json:"started_at"`
		EndedAt            *time.Time        `json:"ended_at,omitempty"`
		IsMultiPart        bool              `json:"is_multi_part,omitempty"`
		PartNumber         int               `json:"part_number,omitempty"`
		PartSuffix         string            `json:"part_suffix,omitempty"`
	}

	var legacyResults map[string]*legacyFileResult
	if err := json.Unmarshal(raw, &legacyResults); err != nil {
		return nil, fmt.Errorf("legacy format: %w", err)
	}

	results := make(map[string]*MovieResult, len(legacyResults))
	provenance := make(map[string]*ProvenanceData, len(legacyResults))

	for filePath, lfr := range legacyResults {
		if lfr == nil {
			continue
		}
		mr := &MovieResult{
			FileMatchInfo: models.FileMatchInfo{
				Path:        lfr.FilePath,
				MovieID:     lfr.MovieID,
				IsMultiPart: lfr.IsMultiPart,
				PartNumber:  lfr.PartNumber,
				PartSuffix:  lfr.PartSuffix,
			},
			ResultID:  lfr.ResultID,
			Revision:  lfr.Revision,
			Status:    lfr.Status,
			Error:     lfr.Error,
			StartedAt: lfr.StartedAt,
			EndedAt:   lfr.EndedAt,
			OrchestrationState: models.OrchestrationState{
				PosterError:        lfr.PosterError,
				TranslationWarning: lfr.TranslationWarning,
			},
		}
		// Map legacy "data" field to Movie
		if lfr.Data != nil {
			var m models.Movie
			if err := json.Unmarshal(lfr.Data, &m); err == nil {
				mr.Movie = &m
			}
		}
		results[filePath] = mr
		if lfr.FieldSources != nil || lfr.ActressSources != nil {
			provenance[filePath] = &ProvenanceData{
				FieldSources:   lfr.FieldSources,
				ActressSources: lfr.ActressSources,
			}
		}
	}
	return &ParsedJobResults{Results: results, Provenance: provenance}, nil
}

// parseOldMovieResultFormat parses the old MovieResult format with nested "file_match_info".
func parseOldMovieResultFormat(raw []byte) (*ParsedJobResults, error) {
	type oldMovieResult struct {
		FileMatchInfo  models.FileMatchInfo `json:"file_match_info"`
		Movie          *models.Movie        `json:"movie,omitempty"`
		Revision       uint64               `json:"revision"`
		Status         models.JobStatus     `json:"status"`
		Error          string               `json:"error,omitempty"`
		FieldSources   map[string]string    `json:"field_sources,omitempty"`
		ActressSources map[string]string    `json:"actress_sources,omitempty"`
		ResultID       string               `json:"result_id"`
		StartedAt      time.Time            `json:"started_at"`
		EndedAt        *time.Time           `json:"ended_at,omitempty"`
		// Orchestration fields — same shape as OrchestrationState but flat in old format.
		DisplayTitleApplied bool    `json:"display_title_applied,omitempty"`
		PosterGenerated     bool    `json:"poster_generated,omitempty"`
		Persisted           bool    `json:"persisted,omitempty"`
		PosterError         *string `json:"poster_error,omitempty"`
		TranslationWarning  *string `json:"translation_warning,omitempty"`
	}

	var oldResults map[string]*oldMovieResult
	if err := json.Unmarshal(raw, &oldResults); err != nil {
		return nil, fmt.Errorf("old movie result format: %w", err)
	}

	results := make(map[string]*MovieResult, len(oldResults))
	provenance := make(map[string]*ProvenanceData, len(oldResults))

	for k, v := range oldResults {
		if v == nil {
			continue
		}
		mr := &MovieResult{
			ResultID:      v.ResultID,
			FileMatchInfo: v.FileMatchInfo,
			Movie:         v.Movie,
			Revision:      v.Revision,
			Status:        v.Status,
			Error:         v.Error,
			StartedAt:     v.StartedAt,
			EndedAt:       v.EndedAt,
			OrchestrationState: models.OrchestrationState{
				DisplayTitleApplied: v.DisplayTitleApplied,
				PosterGenerated:     v.PosterGenerated,
				Persisted:           v.Persisted,
				PosterError:         v.PosterError,
				TranslationWarning:  v.TranslationWarning,
			},
		}
		results[k] = mr
		if v.FieldSources != nil || v.ActressSources != nil {
			provenance[k] = &ProvenanceData{
				FieldSources:   v.FieldSources,
				ActressSources: v.ActressSources,
			}
		}
	}
	return &ParsedJobResults{Results: results, Provenance: provenance}, nil
}
