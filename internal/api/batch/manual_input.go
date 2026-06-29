package batch

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
)

// resolveManualInputOverride produces the final per-file RawInputOverride map by
// propagating each submitted file's manual input to newly-discovered sibling
// files that share the same matcher MovieID. Co-submitted files (present in
// submittedFiles) keep their own input and are never overwritten by
// propagation: propagation targets only files that were discovered by sibling
// discovery, not files the caller submitted (backend F1). Two submitted files
// sharing a matcher MovieID with conflicting manual inputs are rejected
// (backend F2): otherwise map-iteration order would decide which input a
// shared sibling receives, making the scrape non-deterministic.
//
// fileMatchInfo is the metadata returned by discoverSiblingPartsWithMetadata;
// allFiles is its expanded file list (submitted + discovered).
func resolveManualInputOverride(
	submittedFiles []string,
	manualInputs map[string]string,
	fileMatchInfo map[string]models.FileMatchInfo,
	allFiles []string,
) (map[string]string, error) {
	if len(manualInputs) == 0 {
		return manualInputs, nil
	}

	submitted := make(map[string]bool, len(submittedFiles))
	for _, f := range submittedFiles {
		submitted[f] = true
	}

	// Map each submitter's manual input to the matcher MovieID of its file.
	movieInput := make(map[string]string)
	for path, input := range manualInputs {
		if !submitted[path] {
			continue
		}
		fmi, ok := fileMatchInfo[path]
		if !ok || fmi.MovieID == "" {
			continue
		}
		if existing, dup := movieInput[fmi.MovieID]; dup && existing != input {
			return nil, fmt.Errorf("conflicting manual inputs for movie %q: %q vs %q", fmi.MovieID, scrape.RedactURLQuery(existing), scrape.RedactURLQuery(input))
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
		if input, ok := movieInput[fmi.MovieID]; ok {
			result[path] = input
		}
	}

	return result, nil
}
