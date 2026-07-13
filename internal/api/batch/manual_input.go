package batch

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// inputKey identifies a (matcher MovieID, manual input) pair so we can count
// how many submitted files share each manual input within a matcher group.
type inputKey struct {
	movieID string
	input   string
}

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

	// Map each submitter's manual input to the matcher MovieID of its file and
	// track distinct manual inputs + per-input counts per matcher MovieID.
	// A matcher group is ambiguous (being split) when it has more than one
	// distinct manual input. Within an ambiguous group, only files whose input
	// is unique (no sibling shares it) lose multipart metadata; files sharing
	// an input keep their part metadata so organize/NFO templates still group
	// them as a genuine multi-part under the shared ID.
	movieInput := make(map[string]string)
	movieInputs := make(map[string]map[string]bool)
	inputCounts := make(map[inputKey]int)
	for path, input := range manualInputs {
		if !submitted[path] {
			continue
		}
		fmi, ok := fileMatchInfo[path]
		if !ok || fmi.MovieID == "" {
			continue
		}
		trimmed := strings.TrimSpace(input)
		if movieInputs[fmi.MovieID] == nil {
			movieInputs[fmi.MovieID] = make(map[string]bool)
		}
		// Use the raw (unredacted) input for ambiguity detection so URLs
		// whose movie ID lives in the query string (?v=IPX-111 vs
		// ?v=IPX-222) are correctly seen as distinct inputs. The redacted
		// value is still used for the grouping key (FileMatchInfo.MovieID)
		// in the override loop below.
		movieInputs[fmi.MovieID][trimmed] = true
		if existing, ok := movieInput[fmi.MovieID]; !ok || input < existing {
			movieInput[fmi.MovieID] = input
		}
		inputCounts[inputKey{fmi.MovieID, trimmed}]++
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
		// Skip propagation for ambiguous groups (more than one distinct input) —
		// we can't know which input to propagate.
		if inputs := movieInputs[fmi.MovieID]; len(inputs) > 1 {
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
		// Use the raw input for split detection (matches inputCounts keying)
		// so query-based URLs (?v=IPX-111 vs ?v=IPX-222) count as distinct.
		count := inputCounts[inputKey{fmi.MovieID, trimmed}]
		isSplit := count <= 1 && len(movieInputs[fmi.MovieID]) > 1
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
