package nfo

import (
	"fmt"
	"strings"
)

// MergeStrategy defines how to merge metadata from different sources.
type MergeStrategy string

const (
	// PreferScraper uses scraper data when available, falls back to NFO (default)
	PreferScraper MergeStrategy = "prefer-scraper"
	// PreferNFO uses NFO data when available, falls back to scraper (conservative)
	PreferNFO MergeStrategy = "prefer-nfo"
	// MergeArrays combines arrays from both sources and deduplicates
	MergeArrays MergeStrategy = "merge-arrays"
	// PreserveExisting never overwrites non-empty fields (strictest preservation)
	PreserveExisting MergeStrategy = "preserve-existing"
	// FillMissingOnly only populates completely empty fields (safe gap filling)
	FillMissingOnly MergeStrategy = "fill-missing-only"
)

// criticalFields defines fields that must never be empty, regardless of merge strategy
// These fields will always fall back to NFO if scraper returns empty
var criticalFields = map[string]bool{
	"ID":        true,
	"ContentID": true,
	"Title":     true,
}

func (s MergeStrategy) String() string {
	return string(s)
}

// ParseScalarStrategy converts scalar strategy string to MergeStrategy.
// Returns an error for unknown inputs. Empty string returns PreferNFO (default).
func ParseScalarStrategy(strategy string) (MergeStrategy, error) {
	switch strings.ToLower(strategy) {
	case "":
		return PreferNFO, nil
	case "prefer-scraper":
		return PreferScraper, nil
	case "prefer-nfo":
		return PreferNFO, nil
	case "merge-arrays":
		return MergeArrays, nil
	case "preserve-existing":
		return PreserveExisting, nil
	case "fill-missing-only":
		return FillMissingOnly, nil
	default:
		return MergeStrategy(""), fmt.Errorf("unknown scalar strategy %q", strategy)
	}
}

// ArrayStrategyMerge is the string value for the "merge" array strategy.
const ArrayStrategyMerge = "merge"

// ArrayStrategyReplace is the string value for the "replace" array strategy.
const ArrayStrategyReplace = "replace"

// ParseArrayStrategy converts array strategy string to a boolean.
// Valid values: ArrayStrategyMerge/"merge" (returns true), ArrayStrategyReplace/"replace" (returns false) (case-insensitive).
// Empty string returns true (merge, the default).
// Returns an error for unknown inputs.
func ParseArrayStrategy(strategy string) (bool, error) {
	switch strings.ToLower(strategy) {
	case ArrayStrategyMerge, "":
		return true, nil
	case ArrayStrategyReplace:
		return false, nil
	default:
		return false, fmt.Errorf("unknown array strategy %q", strategy)
	}
}

// ApplyPreset applies a preset configuration to scalar and array strategy strings.
// Presets:
//   - "conservative": preserve-existing + merge (strictest preservation)
//   - "gap-fill": fill-missing-only + merge (safe gap filling)
//   - "aggressive": prefer-scraper + replace (trust scrapers completely)
//
// Returns the resolved scalar and array strategy strings, or an error if preset is invalid.
// If preset is empty, returns the original strategy strings unchanged.
func ApplyPreset(preset string, scalarStrategy string, arrayStrategy string) (string, string, error) {
	if preset == "" {
		return scalarStrategy, arrayStrategy, nil
	}

	switch strings.ToLower(preset) {
	case "conservative":
		return "preserve-existing", ArrayStrategyMerge, nil
	case "gap-fill":
		return "fill-missing-only", ArrayStrategyMerge, nil
	case "aggressive":
		return "prefer-scraper", ArrayStrategyReplace, nil
	default:
		return scalarStrategy, arrayStrategy, fmt.Errorf("invalid preset: %s (valid options: conservative, gap-fill, aggressive)", preset)
	}
}

// ApplyPresetTyped applies a preset and returns typed strategy values, eliminating
// the string round-trip that ApplyPreset requires. Use this when callers already
// have resolved MergeStrategy and bool values.
func ApplyPresetTyped(preset string, scalar MergeStrategy, array bool) (MergeStrategy, bool, error) {
	if preset == "" {
		return scalar, array, nil
	}

	switch strings.ToLower(preset) {
	case "conservative":
		return PreserveExisting, true, nil
	case "gap-fill":
		return FillMissingOnly, true, nil
	case "aggressive":
		return PreferScraper, false, nil
	default:
		return scalar, array, fmt.Errorf("invalid preset: %s (valid options: conservative, gap-fill, aggressive)", preset)
	}
}
