package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestApplyWordReplacement_CensoredTokenNotMatchedAsSubstring is the regression
// guard for issue #106: a short censored token (F***) must NOT be replaced when
// it appears as a prefix of a longer, unlisted censored token (F****d).
//
// The default word-replacement list ships {F***: Fuck, F***e: Force,
// F*****g: Forcing} but NOT F****d. Under the old strings.ReplaceAll matcher,
// F*** matched as a substring at position 0 of F****d, producing "Fuck*d".
// Boundary-aware matching treats F*** as a whole censored token (bounded by
// non-letter, non-asterisk chars), so the trailing '*' of F****d blocks the
// match and F****d is left unchanged.
func TestApplyWordReplacement_CensoredTokenNotMatchedAsSubstring(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"F***":    "Fuck",
		"F***e":   "Force",
		"F*****g": "Forcing",
	})

	// F****d is NOT in the cache — F*** must not fire inside it.
	assert.Equal(t, "F****d", wp.Apply("F****d"))
}

// TestApplyWordReplacement_CensoredTokenMatchesStandalone locks the positive
// side of boundary-aware matching: a censored token that IS bounded (by string
// start/end, whitespace, or punctuation) is still replaced. This guards against
// the boundary check becoming too strict and dropping legitimate replacements.
func TestApplyWordReplacement_CensoredTokenMatchesStandalone(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"F***":    "Fuck",
		"F***e":   "Force",
		"F*****g": "Forcing",
	})

	assert.Equal(t, "Fuck", wp.Apply("F***"))
	assert.Equal(t, "Fuck movie", wp.Apply("F*** movie"))
	assert.Equal(t, "movie Fuck", wp.Apply("movie F***"))
	assert.Equal(t, "Fuck, title", wp.Apply("F***, title"))
	assert.Equal(t, "(Fuck)", wp.Apply("(F***)"))

	// Listed longer censored tokens still replace when standalone — guards
	// against an over-strict boundary rejecting a token whose end lands on
	// end-of-string, and locks the longest-first sort + boundary interaction
	// for asterisk patterns.
	assert.Equal(t, "Forcing", wp.Apply("F*****g"))
	assert.Equal(t, "Force", wp.Apply("F***e"))
}

// TestApplyWordReplacement_CensoredTokenNoMatchReturnsUnchanged confirms the
// no-match fast path: when the censored pattern is absent from the text, the
// text is returned unchanged (and without avoidable allocation via the builder).
func TestApplyWordReplacement_CensoredTokenNoMatchReturnsUnchanged(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"F***": "Fuck",
	})

	assert.Equal(t, "nothing here", wp.Apply("nothing here"))
	assert.Equal(t, "", wp.Apply(""))
}

// TestApplyWordReplacement_CensoredTokenAdjacentTokens ensures the boundary
// check (which inspects chars without consuming them) doesn't starve an
// immediately-following token of its leading boundary. Two censored tokens
// separated by a space must both replace.
func TestApplyWordReplacement_CensoredTokenAdjacentTokens(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"F***":  "Fuck",
		"S***e": "Slave",
	})

	assert.Equal(t, "Fuck Slave", wp.Apply("F*** S***e"))
}

// TestApplyWordReplacement_NonAsteriskPatternStillSubstring confirms that
// patterns WITHOUT '*' (e.g. the "[Recommended For Smartphones] " prefix strip)
// keep the old substring-matching behavior — they are genuinely meant to match
// as substrings, not as bounded tokens.
func TestApplyWordReplacement_NonAsteriskPatternStillSubstring(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"[Recommended For Smartphones] ": "",
	})

	assert.Equal(t, "Title", wp.Apply("[Recommended For Smartphones] Title"))
}

// TestApplyWordReplacements_CensoredTokenInTitle is the end-to-end regression
// guard for issue #106: a movie title containing an unlisted censored token
// (F****d, the r18dev-censored form of "Forced") must not be corrupted by the
// shorter F*** entry in the default list into "Fuck*d". The title is passed
// through applyToMovie exactly as the aggregator does during a scrape.
func TestApplyWordReplacements_CensoredTokenInTitle(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"F***":    "Fuck",
		"F***e":   "Force",
		"F*****g": "Forcing",
	})

	movie := &models.Movie{
		Title: "F****d Entry",
	}
	wp.applyToMovie(movie)

	assert.Equal(t, "F****d Entry", movie.Title)
}

// TestApplyWordReplacement_MultibyteBoundary confirms the boundary check decodes
// full UTF-8 runes (not single bytes) when classifying the char adjacent to a
// censored token. This matters for r18dev titles, which can be Japanese.
//
// A censored token directly abutting a multibyte rune (Japanese katakana/kanji,
// or full-width punctuation like 「」) must be classified by the actual rune:
// a Japanese letter extends the word (blocks the match), while a Japanese
// punctuation char is a boundary (allows the match). The old byte-level read
// misclassified lead/continuation bytes and got these wrong.
func TestApplyWordReplacement_MultibyteBoundary(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"F***": "Fuck",
	})

	// Censored token followed directly by a Japanese letter (no separator):
	// ド is a letter and extends the word, so F*** must NOT fire.
	assert.Equal(t, "F***ドラマ", wp.Apply("F***ドラマ"))
	// With a separator it replaces.
	assert.Equal(t, "Fuck ドラマ", wp.Apply("F*** ドラマ"))
	// Japanese full-width brackets 「」 are punctuation (boundaries), so the
	// token inside them replaces.
	assert.Equal(t, "「Fuck」", wp.Apply("「F***」"))
}
