package batch

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// resolveManualInputOverride produces the final per-file RawInputOverride map by
// propagating each submitted file's manual input to newly-discovered sibling
// files that share the same matcher MovieID. Co-submitted files (present in
// submittedFiles) keep their own input and are never overwritten by
// propagation: propagation targets only files that were discovered by sibling
// discovery, not files the caller submitted (backend F1). Two submitted files
// sharing a matcher MovieID with conflicting manual inputs are NOT rejected
// (backend F2): the user is explicitly splitting matcher-grouped files into
// separate movies. Instead, the conflicting MovieID is marked ambiguous so
// sibling propagation is skipped (we can't know which input to propagate).
//
// fileMatchInfo is the metadata returned by discoverSiblingPartsWithMetadata;
// allFiles is its expanded file list (submitted + discovered). The function
// also overrides fileMatchInfo entries so files with explicit manual inputs
// are grouped by the user's ID, not the matcher's.
func resolveManualInputOverride(
	submittedFiles []string,
	manualInputs map[string]string,
	fileMatchInfo map[string]models.FileMatchInfo,
	allFiles []string,
) map[string]string {
	if len(manualInputs) == 0 {
		return manualInputs
	}

	submitted := make(map[string]bool, len(submittedFiles))
	for _, f := range submittedFiles {
		submitted[f] = true
	}

	// Map each submitter's manual input to the matcher MovieID of its file.
	// Conflicting inputs for the same matcher MovieID are not rejected — the
	// user is explicitly splitting matcher-grouped files — but the MovieID is
	// marked ambiguous so sibling propagation is skipped.
	movieInput := make(map[string]string)
	ambiguousMovies := make(map[string]bool)
	for path, input := range manualInputs {
		if !submitted[path] {
			continue
		}
		fmi, ok := fileMatchInfo[path]
		if !ok || fmi.MovieID == "" {
			continue
		}
		if existing, dup := movieInput[fmi.MovieID]; dup && existing != input {
			ambiguousMovies[fmi.MovieID] = true
			continue
		}
		movieInput[fmi.MovieID] = input
	}

	// Seed the result with every explicit input (submitters keep their own).
	result := make(map[string]string, len(manualInputs))
	for k, v := range manualInputs {
		result[k] = v
	}

	// Propagate to discovered siblings (!submitted) sharing a MovieID. Co-submitted
	// files are skipped so a part the caller submitted is never clobbered.
	for _, path := range allFiles {
		if submitted[path] {
			continue
		}
		if _, has := result[path]; has {
			continue
		}
		fmi, ok := fileMatchInfo[path]
		if !ok || fmi.MovieID == "" {
			continue
		}
		if ambiguousMovies[fmi.MovieID] {
			continue
		}
		if input, ok := movieInput[fmi.MovieID]; ok {
			result[path] = input
		}
	}

	// Override matcher-derived metadata so files with explicit manual inputs
	// are grouped by the user's ID, not the matcher's. Files with different
	// manual inputs naturally split into separate movies; files with the same
	// manual input stay grouped (correct for genuine multi-part).
	for path, input := range result {
		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			continue
		}
		fmi, ok := fileMatchInfo[path]
		if !ok {
			continue
		}
		// Redact URL query params before using as the grouping key —
		// buildScrapeCmd does the same for cmd.MovieID. RawInputOverride
		// (the result map) stays raw so the scraper sees the real URL.
		redacted := scrape.RedactURLQuery(trimmed)
		// Only clear multipart metadata when the matcher group was actually
		// split by conflicting manual inputs (ambiguousMovies). A genuine
		// multi-part corrected to a new shared ID preserves part metadata
		// so organizer/NFO templates still get <PART> suffixes.
		isSplit := ambiguousMovies[fmi.MovieID]
		fmi.MovieID = redacted
		if isSplit {
			fmi.IsMultiPart = false
			fmi.PartNumber = 0
			fmi.PartSuffix = ""
		}
		fileMatchInfo[path] = fmi
	}

	return result
}
