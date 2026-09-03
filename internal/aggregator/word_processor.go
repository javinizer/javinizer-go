package aggregator

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/models"
)

// wordReplacementEntry is one cached replacement: literal entries match
// exactly as pre-#228 (substring, or token-bounded when the pattern contains
// '*'); wildcard entries compile a censor-glyph-run matcher (wildcardRe).
type wordReplacementEntry struct {
	orig       string
	repl       string
	wildcardRe *regexp.Regexp
}

// censorGlyphClass is the fixed censor glyph set matched by the wildcard
// sentinel '?' (and its fullwidth twin). (#228)
const censorGlyphClass = "*＊○◯〇●×✕✖"

// wildcardSentinels are the runes that act as censor-run wildcards in a
// wildcard-mode pattern.
const wildcardSentinels = "?？"

// compileWildcardPattern builds the matcher for a wildcard-mode pattern.
// Sentinels collapse runs ("??" == "?") and match GREEDILY one-or-more glyphs
// from censorGlyphClass; every other rune is literal. A wildcard entry without
// a sentinel degenerates to an exact literal pattern (bounded), which callers
// treat as documented no-surprise behavior.
func compileWildcardPattern(pattern string) *regexp.Regexp {
	if pattern == "" {
		// An empty pattern would compile to a zero-width regex whose empty
		// match never advances the replace loop. Never compile it. (#228)
		return nil
	}
	var b strings.Builder
	for i := 0; i < len(pattern); {
		r, size := utf8.DecodeRuneInString(pattern[i:])
		i += size
		if strings.ContainsRune(wildcardSentinels, r) {
			for i < len(pattern) {
				nr, ns := utf8.DecodeRuneInString(pattern[i:])
				if !strings.ContainsRune(wildcardSentinels, nr) {
					break
				}
				i += ns
			}
			b.WriteString("[" + regexp.QuoteMeta(censorGlyphClass) + "]+")
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(r)))
	}
	// MustCompile is safe: segments are QuoteMeta'd literals plus a fixed
	// character class, so the generated pattern can never be invalid.
	return regexp.MustCompile(b.String())
}

// buildWordReplacementSorted converts entries into a slice sorted longest-first
// (then lexicographically) so that longer patterns are replaced before shorter
// ones, avoiding partial matches. Ordering applies across modes: a longer
// literal pattern wins over a shorter wildcard one.
func buildWordReplacementSorted(entries []wordReplacementEntry) []wordReplacementEntry {
	sorted := make([]wordReplacementEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		if len(sorted[i].orig) != len(sorted[j].orig) {
			return len(sorted[i].orig) > len(sorted[j].orig)
		}
		return sorted[i].orig < sorted[j].orig
	})
	return sorted
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
	cache  map[string]string      // orig -> repl, kept for introspection/debug
	sorted []wordReplacementEntry // Pre-sorted longest-first
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
// Three matching strategies, dispatched by entry mode and pattern shape:
//
//   - Wildcard entries (match_mode=wildcard): matched via a compiled
//     censor-glyph-run pattern — '?'/'？' match one-or-more glyphs of the
//     censor class, everything else is literal; token-bounded with the
//     longest-or-nothing boundary policy (see replaceWildcardBounded, #228).
//   - Literal patterns WITH '*' (censored-word tokens, e.g. "F***"): matched as a whole
//     token using replaceTokenBounded. The match must be bounded on both sides
//     by string start/end or a char that cannot extend a censored token — i.e.
//     anything other than '*' or a Latin-script character (#106 keeps "F***" from
//     firing inside "F****d"; #227 narrows letter-extension to the Latin script
//     so CJK letters count as boundaries and embedded patterns like "チ*ポ"
//     match inside unsegmented Japanese titles).
//   - Literal patterns WITHOUT '*' (e.g. the "[Recommended For Smartphones] "
//     prefix strip): matched as a plain substring via strings.ReplaceAll,
//     preserving the original behavior for patterns that are genuinely meant
//     to match as substrings.
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
		switch {
		case p.wildcardRe != nil:
			result = replaceWildcardBounded(result, p.wildcardRe, p.repl)
		case strings.ContainsRune(p.orig, '*'):
			result = replaceTokenBounded(result, p.orig, p.repl)
		case strings.Contains(result, p.orig):
			result = strings.ReplaceAll(result, p.orig, p.repl)
		}
	}

	return result
}

// replaceWildcardBounded replaces matches of a compiled wildcard pattern with
// repl, applying the same whole-token boundary rule as replaceTokenBounded
// with a LONGEST-OR-NOTHING policy: the boundary is inspected once at the
// maximal match extent the regexp produced (RE2 greedy give-back only shortens
// a class run to satisfy following literal segments, never to satisfy the
// boundary). A rejected candidate is skipped past, never retried shorter —
// this blocks partial-censored-token replacements (see design D3, #228).
func replaceWildcardBounded(text string, re *regexp.Regexp, repl string) string {
	if !re.MatchString(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	i := 0
	for {
		loc := re.FindStringIndex(text[i:])
		if loc == nil {
			b.WriteString(text[i:])
			break
		}
		start := i + loc[0]
		end := i + loc[1]
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

// replaceTokenBounded replaces every non-overlapping occurrence of orig in text
// with repl, but only when the match is bounded on both sides by string
// start/end or a boundary character per isCensorBoundary. This is the "whole
// censored token" rule: '*' is the censor character, so a run like "F***" is a
// complete censored word only if the char after it is not a Latin-script character
// (which would extend the word) and not '*' (which would mean it's actually a
// longer censored token, e.g. "F****d"). CJK letters can never be part of a
// Latin censored token, so they act as boundaries (#227).
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
// token: any char that cannot extend the token — i.e. anything other than '*'
// or a Latin-script character (the unicode.Latin table is script-based and
// also contains Roman numerals in category Nl — immaterial here). Digits,
// spaces, punctuation, symbols, and CJK or other non-Latin letters all qualify
// (#227); '*' and Latin-script characters extend the token (#106).
func isCensorBoundary(r rune) bool {
	return r != '*' && !unicode.In(r, unicode.Latin)
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

	rows, err := wp.repo.GetReplacementEntries(ctx)
	if err == nil {
		cache := make(map[string]string, len(rows))
		entries := make([]wordReplacementEntry, 0, len(rows))
		for _, row := range rows {
			if row.Original == "" {
				continue // defensive: empty patterns can never be useful
			}
			cache[row.Original] = row.Replacement
			entry := wordReplacementEntry{orig: row.Original, repl: row.Replacement}
			if row.MatchMode == models.MatchModeWildcard {
				entry.wildcardRe = compileWildcardPattern(row.Original)
			}
			entries = append(entries, entry)
		}
		wp.mu.Lock()
		wp.cache = cache
		wp.sorted = buildWordReplacementSorted(entries)
		wp.mu.Unlock()
	}
}
