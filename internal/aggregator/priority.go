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
		fieldSnake := toSnakeCase(field)

		// Default: use the global priority list (unchanged behavior for fields
		// without a per-field override).
		fieldPriority := copySlice(globalPriority)

		// Guard a.cfg.Metadata == nil: MetadataConfigFromApp returns nil when the
		// app config has no metadata block, and getFieldPriorityFromConfig (below)
		// already treats cfg.Metadata as optional. Without this guard, dereferencing
		// a.cfg.Metadata.Priority here would panic for configs that rely only on
		// ScrapersPriority (CodeRabbit, PR #51).
		if a.cfg != nil && a.cfg.Metadata != nil {
			if fp := a.cfg.Metadata.Priority.PerFieldOverride(fieldSnake); fp != nil {
				// A per-field override is EXCLUSIVE: only the scrapers listed in the
				// override are consulted for that field — there is NO fallback to
				// the global priority list. This restores v1 (PowerShell Javinizer)
				// semantics (#50): `series: [tokyohot]` leaves Series empty when
				// tokyohot has no Series, instead of filling it from r18dev/dmm via
				// global fallback. There is no skip sentinel — suppression is the
				// emergent result of pointing a field at a scraper (or a never-
				// registered name) that doesn't provide it, OR of storing an explicit
				// empty slice (`series: []`), which means "consult no scrapers" and
				// leaves the field empty. PerFieldOverride returns a non-nil empty
				// slice for a present `[]`, so `fp != nil` honors it here (an absent
				// key returns nil and falls through to the global default above).
				fieldPriority = copySlice(fp)
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

// getFieldPriorityFromConfig returns the scraper priority list.
// Checks per-field override first, then global metadata priority, then scrapers priority.
// Returns nil when no config is available.
//
// A PRESENT per-field override wins exclusively — including an explicit empty
// slice (`series: []`), which yields an empty list ("consult no scrapers")
// rather than falling back to global. An ABSENT key (or nil slice) inherits the
// global metadata priority, then ScrapersPriority. This matches the default
// Aggregate path's resolvePriorities and keeps both scrape paths consistent.
func getFieldPriorityFromConfig(cfg *Config, fieldKey string) []string {
	if cfg == nil {
		return nil
	}

	if cfg.Metadata != nil {
		if fp := cfg.Metadata.Priority.PerFieldOverride(fieldKey); fp != nil {
			return fp
		}
		if len(cfg.Metadata.Priority.Priority) > 0 {
			return cfg.Metadata.Priority.Priority
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
