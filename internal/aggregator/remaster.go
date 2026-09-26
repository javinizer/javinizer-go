package aggregator

import (
	"regexp"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/matcher"
)

// remasterDisplayTailRegex recognizes the marker-bearing display-ID
// spellings the fold canonicalizes: a trailing HD/AI/H remaster marker
// introduced by a separator (RCT-156-HD, RCT-156-Z-H) or glued directly to
// the catalog number (RCT156HD, RCT156H), with the E/Z catalog suffix
// letter optionally riding between the number and the marker (RCT156ZH,
// RCT-156-ZHD). Everything else — marker-free ids, codec tails (H.264),
// quality phrases, part labels — never reaches the canonicalizer and
// passes through unchanged.
var remasterDisplayTailRegex = regexp.MustCompile(`(?i)^(?:.+?[-_.\s]|.+?\d)[-_.\s]?[ez]?(HD|AI|H)$`)

// canonicalDisplayIDMatcher is the shared canonicalizer the display-ID fold
// delegates to: the matcher's own canonicalization, the same
// Matcher.MatchString the file pipeline runs, built once from the default
// config (no custom regex, so the fold always sees the built-in canonical
// tiers; NewMatcher's error return exists only for custom-regex
// compilation, which the default config never triggers — the parity test
// constructs the same matcher and requires no error). The matcher is
// immutable after construction, so the shared instance is safe for
// concurrent aggregation.
var canonicalDisplayIDMatcher = sync.OnceValue(func() *matcher.Matcher {
	m, _ := matcher.NewMatcher(&matcher.Config{})
	return m
})

// foldRemasterDisplayID canonicalizes a scraper-returned display ID for a
// remastered release to the matcher's canonical spelling
// (SERIES-NUMBER[SUFFIX][MARKER], e.g. RCT-156H, DV-899AI, T28-123H): the
// same canonicalization the matcher applies to the equivalent filename,
// so the aggregated movie carries the matched release's identity instead
// of forking it on the scraper's spelling. JavDB's compact RCT156HD
// previously folded only the marker (RCT156H) while the matcher
// canonicalized the file to RCT-156H, so the scraped metadata and the
// matched file resolved to different database/organized identities.
//
// The fold delegates to the matcher's canonicalization — the shared
// canonical function the v1 report recommended, already exported today as
// Matcher.MatchString — so every grammar quirk the matcher owns rides
// along instead of drifting in a local reimplementation: the series-number
// separator (RCT156HD and RCT-156-HD alike canonicalize to RCT-156H), the
// E/Z catalog suffix position (RCT-156-Z-H and RCT-156-ZH both fold to
// RCT-156ZH, the suffix fused to the number), the t28 convention
// (T28-123-HD stays T28-123H while the prefix-free compact T28123H reads
// as the T-series release T-28123H, and a four-digit tail rides the
// 6-digit T-series grammar as T-281234H), AI markers, stacked markers
// (the first marker wins), zero-padded numbers kept verbatim
// (RCT.00156.HD -> RCT-00156H), and the matcher's tier-2 raw spellings
// for cid-shaped ids (1rct00156hd -> 1RCT00156HD). The output is always
// the matcher's canonical spelling, including its uppercase convention, so
// lowercase scraper spellings (rct-156-h) no longer key apart from the
// matched release either.
//
// The marker-tail gate bounds the delegation: marker-free ids (RCT-156,
// IPX-00535Z) and marker-bearing strings the matcher itself rejects (prose
// word-years like vacation2024hd, quality phrases like 1080P-HD) are
// returned unchanged, so the fold never invents an identity the file
// pipeline would not produce.
func foldRemasterDisplayID(id string) string {
	trimmed := strings.TrimSpace(id)
	if !remasterDisplayTailRegex.MatchString(trimmed) {
		return id
	}
	if canonical := canonicalDisplayIDMatcher().MatchString(trimmed); canonical != "" {
		return canonical
	}
	return id
}
