package aggregator

import (
	"unicode"
)

// compileGenreRegexes moved to GenreProcessor.compileRegexes in genre_processor.go

// isRegexPattern checks if a string contains regex metacharacters
// Only returns true for patterns with clear regex intent
// Avoids false positives on literal dots in names like "S1.No1Style"
func isRegexPattern(s string) bool {
	if s == "" {
		return false
	}
	// Check for anchor characters (highest confidence indicators)
	if s[0] == '^' || s[len(s)-1] == '$' {
		return true
	}
	// Check for quantifier patterns (*, +, ?) that follow other characters
	// This catches patterns like "test*", "test+", "test?" which are clearly regex
	if len(s) >= 2 {
		for i := 0; i < len(s)-1; i++ {
			// Check if next character is a quantifier
			if s[i+1] == '*' || s[i+1] == '+' || s[i+1] == '?' {
				return true
			}
		}
	}
	// Check for other unambiguous regex metacharacters
	// Note: we explicitly exclude lone dots (.) as they're common in genre names
	meta := []rune{'\\', '[', ']', '(', ')', '|', '{', '}'}
	for _, r := range s {
		for _, m := range meta {
			if r == m {
				return true
			}
		}
	}
	return false
}

// resolvePriorities resolves all field priorities at initialization time
// Supports per-field overrides from config; fields without overrides use global priority.
func (a *Aggregator) resolvePriorities() {
	a.resolvedPriorities = make(map[string][]string)

	globalPriority := getFieldPriorityFromConfig(a.cfg, "")

	// List of all metadata fields that need priority resolution
	fields := []string{
		"ID", "ContentID", "Title", "OriginalTitle", "Description",
		"Director", "Maker", "Label", "Series", "PosterURL", "CoverURL",
		"TrailerURL", "Runtime", "ReleaseDate", "Rating", "Actress",
		"Genre", "ScreenshotURL",
	}

	for _, field := range fields {
		fieldPriority := copySlice(globalPriority)

		if a.cfg != nil {
			if fp := a.cfg.Metadata.Priority.GetFieldPriority(toSnakeCase(field)); len(fp) > 0 {
				fieldPriority = mergePriorityLists(fp, globalPriority)
			}
		}

		a.resolvedPriorities[field] = fieldPriority
	}
}

// GetResolvedPriorities returns the cached field-level priority map (for debugging)
//
//nolint:unused // used by same-package tests
func (a *Aggregator) getResolvedPriorities() map[string][]string {
	return a.resolvedPriorities
}

// mergePriorityLists appends globalFallback to perFieldOverride, skipping duplicates.
// Per-field entries keep their relative order; global entries fill in after.
func mergePriorityLists(perFieldOverride, globalFallback []string) []string {
	seen := make(map[string]struct{}, len(perFieldOverride)+len(globalFallback))
	merged := make([]string, 0, len(perFieldOverride)+len(globalFallback))
	for _, s := range perFieldOverride {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			merged = append(merged, s)
		}
	}
	for _, s := range globalFallback {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			merged = append(merged, s)
		}
	}
	return merged
}

// getFieldPriorityFromConfig returns the scraper priority list.
// Checks per-field override first, then global metadata priority, then scrapers priority.
// Returns nil when no config is available.
func getFieldPriorityFromConfig(cfg *Config, fieldKey string) []string {
	if cfg == nil {
		return nil
	}

	if cfg.Metadata != nil {
		if fp := cfg.Metadata.Priority.GetFieldPriority(fieldKey); len(fp) > 0 {
			return fp
		}
	}

	if len(cfg.ScrapersPriority) > 0 {
		return cfg.ScrapersPriority
	}

	return nil
}

// copySlice creates a copy of a string slice
func copySlice(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// toSnakeCase converts CamelCase field names to snake_case for config lookup.
// e.g. "OriginalTitle" → "original_title", "ID" → "id", "PosterURL" → "poster_url"
func toSnakeCase(s string) string {
	var result []byte
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			// Add underscore before this uppercase letter if:
			// - it's not the first character AND
			// - the previous character is lowercase OR
			// - the next character is lowercase (end of acronym like "URL" → next is end or lowercase)
			if i > 0 {
				prev := runes[i-1]
				if prev >= 'a' && prev <= 'z' {
					result = append(result, '_')
				} else if prev >= 'A' && prev <= 'Z' && i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z' {
					result = append(result, '_')
				}
			}
			result = append(result, []byte(string(unicode.ToLower(r)))...)
		} else {
			result = append(result, []byte(string(r))...)
		}
	}
	return string(result)
}
