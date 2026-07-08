package aggregator

import (
	"context"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/models"
)

// buildWordReplacementSorted converts a cache map into a slice of pairs sorted
// longest-first (then lexicographically) so that longer patterns are replaced
// before shorter ones, avoiding partial matches.
func buildWordReplacementSorted(cache map[string]string) []struct{ orig, repl string } {
	pairs := make([]struct{ orig, repl string }, 0, len(cache))
	for orig, repl := range cache {
		pairs = append(pairs, struct{ orig, repl string }{orig, repl})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if len(pairs[i].orig) != len(pairs[j].orig) {
			return len(pairs[i].orig) > len(pairs[j].orig)
		}
		return pairs[i].orig < pairs[j].orig
	})
	return pairs
}

// WordProcessorInterface defines the contract for word replacement.
// Extracted from Aggregator to isolate word-replacement concerns with
// their own cache, sorted list, and mutex.
type wordProcessorInterface interface {
	// Apply replaces occurrences of known words in the input text according
	// to the word-replacement cache. Replacements are applied longest-first
	// to avoid partial matches.
	Apply(text string) string

	// applyToMovie applies word replacements to all text fields of a Movie,
	// including translations.
	applyToMovie(movie *models.Movie)

	// Reload refreshes the word replacement cache from the database.
	Reload(ctx context.Context)
}

// WordProcessor owns word replacement logic.
// Each instance has its own cache, sorted replacement list, and mutex —
// no shared mutable state with the parent Aggregator.
type wordProcessor struct {
	cfg    *MetadataConfig
	repo   wordLookup
	cache  map[string]string
	sorted []struct{ orig, repl string } // Pre-sorted longest-first
	mu     sync.RWMutex
}

// NewWordProcessor creates a WordProcessor from config and an optional repository.
// If cfg is nil, returns nil. If cfg.WordReplacement.Enabled and repo is non-nil,
// the cache is loaded from the database.
func NewWordProcessor(cfg *MetadataConfig, repo wordLookup) *wordProcessor {
	if cfg == nil {
		return nil
	}
	wp := &wordProcessor{
		cfg:   cfg,
		repo:  repo,
		cache: make(map[string]string),
	}
	if cfg.WordReplacement.Enabled && repo != nil {
		// Constructor context: there is no caller context available yet, so
		// we use context.Background(). The Reload method accepts a context
		// for callers that need cancellation support.
		wp.loadCache(context.Background())
	}
	return wp
}

// Apply replaces occurrences of known words in the input text.
//
// Two matching strategies, dispatched by whether the pattern contains the
// censor character '*':
//
//   - Patterns WITH '*' (censored-word tokens, e.g. "F***"): matched as a whole
//     token using replaceTokenBounded. The match must be bounded on both sides
//     by string start/end or a char that is neither a letter nor '*'. This
//     prevents a short censored token from matching as a substring inside a
//     longer, unlisted censored token — e.g. "F***" no longer fires inside
//     "F****d" (which would yield "Fuck*d"). Issue #106.
//   - Patterns WITHOUT '*' (e.g. the "[Recommended For Smartphones] " prefix
//     strip): matched as a plain substring via strings.ReplaceAll, preserving
//     the original behavior for patterns that are genuinely meant to match
//     as substrings.
func (wp *wordProcessor) Apply(text string) string {
	if wp == nil || wp.cfg == nil || !wp.cfg.WordReplacement.Enabled {
		return text
	}

	if text == "" {
		return text
	}

	wp.mu.RLock()
	sorted := wp.sorted
	wp.mu.RUnlock()

	if len(sorted) == 0 {
		return text
	}

	result := text
	for _, p := range sorted {
		if strings.ContainsRune(p.orig, '*') {
			result = replaceTokenBounded(result, p.orig, p.repl)
		} else if strings.Contains(result, p.orig) {
			result = strings.ReplaceAll(result, p.orig, p.repl)
		}
	}

	return result
}

// replaceTokenBounded replaces every non-overlapping occurrence of orig in text
// with repl, but only when the match is bounded on both sides by string
// start/end or a character that is neither a Unicode letter nor '*'. This is
// the "whole censored token" rule: '*' is the censor character, so a run like
// "F***" is a complete censored word only if the char after it is not a letter
// (which would extend the word) and not '*' (which would mean it's actually a
// longer censored token, e.g. "F****d").
//
// The boundary chars are INSPECTED but not consumed, so two censored tokens
// separated by a single space ("F*** S***e") both match — the space serves as
// the trailing boundary for the first and the leading boundary for the second.
func replaceTokenBounded(text, orig, repl string) string {
	if orig == "" || !strings.Contains(text, orig) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	i := 0
	for {
		idx := strings.Index(text[i:], orig)
		if idx < 0 {
			b.WriteString(text[i:])
			break
		}
		start := i + idx
		end := start + len(orig)
		if start > 0 && !isCensorBoundary(boundaryRuneBefore(text, start)) {
			b.WriteString(text[i:end])
			i = end
			continue
		}
		if end < len(text) && !isCensorBoundary(boundaryRuneAfter(text, end)) {
			b.WriteString(text[i:end])
			i = end
			continue
		}
		b.WriteString(text[i:start])
		b.WriteString(repl)
		i = end
	}
	return b.String()
}

// isCensorBoundary reports whether r may act as a boundary for a censored-word
// token: any char that is NOT a Unicode letter and NOT '*'. Digits, spaces,
// punctuation, and symbols all qualify; letters and '*' extend the token.
func isCensorBoundary(r rune) bool {
	return r != '*' && !unicode.IsLetter(r)
}

// boundaryRuneBefore decodes the full rune immediately preceding byte index
// `start` in `text` (UTF-8 aware). Returns -1 if start is at the beginning.
// Used so multibyte adjacent chars (e.g. Japanese) are classified by their
// actual rune, not a single lead/continuation byte.
func boundaryRuneBefore(text string, start int) rune {
	if start <= 0 || start > len(text) {
		return -1
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	return r
}

// boundaryRuneAfter decodes the full rune starting at byte index `end` in
// `text` (UTF-8 aware). Returns -1 if end is at the end of the string.
func boundaryRuneAfter(text string, end int) rune {
	if end < 0 || end >= len(text) {
		return -1
	}
	r, _ := utf8.DecodeRuneInString(text[end:])
	return r
}

// ApplyToMovie applies word replacements to all text fields of a Movie.
func (wp *wordProcessor) applyToMovie(movie *models.Movie) {
	if wp == nil || wp.cfg == nil || !wp.cfg.WordReplacement.Enabled {
		return
	}

	movie.Title = wp.Apply(movie.Title)
	movie.OriginalTitle = wp.Apply(movie.OriginalTitle)
	movie.Description = wp.Apply(movie.Description)
	movie.Director = wp.Apply(movie.Director)
	movie.Maker = wp.Apply(movie.Maker)
	movie.Label = wp.Apply(movie.Label)
	movie.Series = wp.Apply(movie.Series)

	for i := range movie.Translations {
		t := &movie.Translations[i]
		t.Title = wp.Apply(t.Title)
		t.OriginalTitle = wp.Apply(t.OriginalTitle)
		t.Description = wp.Apply(t.Description)
		t.Director = wp.Apply(t.Director)
		t.Maker = wp.Apply(t.Maker)
		t.Label = wp.Apply(t.Label)
		t.Series = wp.Apply(t.Series)
	}
}

// Reload refreshes the word replacement cache from the database.
func (wp *wordProcessor) Reload(ctx context.Context) {
	if wp == nil {
		return
	}
	wp.loadCache(ctx)
}

// loadCache loads word replacements from the repository into memory.
// Note: when called from the constructor, there is no caller context available,
// so context.Background() is used. Callers that need cancellation should use
// Reload(ctx) instead, which delegates to this method with the provided context.
func (wp *wordProcessor) loadCache(ctx context.Context) {
	if wp.repo == nil {
		return
	}

	replacementMap, err := wp.repo.GetReplacementMap(ctx)
	if err == nil {
		wp.mu.Lock()
		wp.cache = replacementMap
		wp.sorted = buildWordReplacementSorted(replacementMap)
		wp.mu.Unlock()
	}
}
