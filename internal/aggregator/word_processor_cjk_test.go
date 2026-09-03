package aggregator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// TestApplyWordReplacement_CJKExtensionRule locks the #227 extension predicate:
// only '*' and Latin-script letters extend a censored token; CJK and other
// non-Latin letters are boundaries, so '*' entries match when embedded in
// unsegmented Japanese text, while the #106 over-extension guards still hold.
func TestApplyWordReplacement_CJKExtensionRule(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"チ*ポ":   "チンポ",
		"F***":  "Fuck",
		"F***e": "Force",
	})

	cases := []struct {
		name string
		in   string
		want string
	}{
		// Core #227 fix: CJK pattern embedded in unsegmented Japanese text.
		{"cjk pattern embedded mid-sentence", "チ*ポを咥える人妻", "チンポを咥える人妻"},
		{"cjk pattern standalone", "チ*ポ", "チンポ"},
		{"cjk pattern full-width brackets", "「チ*ポ」", "「チンポ」"},
		{"cjk pattern space separated", "彼は チ*ポ が好き", "彼は チンポ が好き"},
		{"japanese title end to end", "某女優のチ*ポ地獄", "某女優のチンポ地獄"},
		// Over-extension must still be rejected through the predicate: the
		// pattern IS a substring and the adjacent trailing '*' extends it.
		{"trailing asterisk extends", "人気のチ*ポ*", "人気のチ*ポ*"},
		// #106 guard must survive the narrower predicate.
		{"latin over-extension unchanged", "F****d", "F****d"},
		// #227 deliberate flip: Latin token abutting CJS letters replaces.
		{"latin token abutting katakana", "F***ドラマ", "Fuckドラマ"},
		// Latin letter in boundary position extends regardless of accent:
		// pins the rule to the Latin script, guarding against an ASCII-only
		// narrowing of the extender classification.
		{"accented latin boundary position extends", "F***éve", "F***éve"},
		{"latin letter suffix extends", "F***bsuffix", "F***bsuffix"},
		// Multi-entry CJK seam: with {F***, F***e} present, the F***e entry
		// must apply cleanly at the seam and the F*** pass must leave the
		// result alone (pins inter-pattern stability at a non-Latin seam).
		{"multi entry at cjk seam", "F***eドラマ", "Forceドラマ"},
		// Sponsor-class coverage for the changelog's "other non-Latin" claim:
		// halfwidth katakana and Cyrillic letters are also boundaries now.
		{"halfwidth katakana boundary", "F***ｶﾀｶﾅ", "Fuckｶﾀｶﾅ"},
		{"cyrillic boundary", "F***Анна", "FuckАнна"},
		// Full-width asterisk is not the censor char; no substring, no match.
		{"full width asterisk untouched", "チ＊ポを咥える", "チ＊ポを咥える"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, wp.Apply(tc.in))
		})
	}
}

// TestApplyToMovie_CJKEmbeddedCensoredToken covers the aggregator pipeline
// path (spec: movie-field application), including translation entries.
func TestApplyToMovie_CJKEmbeddedCensoredToken(t *testing.T) {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	wp := newWordProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"チ*ポ": "チンポ",
	})
	require.NotNil(t, wp)

	movie := &models.Movie{
		Title:       "某女優のチ*ポ地獄",
		Description: "彼女のチ*ポ物語",
		Translations: []models.MovieTranslation{
			{Language: "en", Title: "She loves チ*ポ"},
		},
	}
	wp.applyToMovie(movie)

	assert.Equal(t, "某女優のチンポ地獄", movie.Title)
	assert.Equal(t, "彼女のチンポ物語", movie.Description)
	require.Len(t, movie.Translations, 1)
	assert.Equal(t, "She loves チンポ", movie.Translations[0].Title)
}
