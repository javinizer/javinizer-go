package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func wildcardCfg() *MetadataConfig {
	cfg := &config.Config{
		Metadata: config.MetadataConfig{
			WordReplacement: config.WordReplacementConfig{Enabled: true},
		},
	}
	return MetadataConfigFromApp(&cfg.Metadata)
}

// TestApplyWordReplacement_WildcardMatrix locks the #228 wildcard semantics:
// '?' / '？' match greedily one-or-more glyphs of the censor class, everything
// else is literal, and matches are token-bounded with the pinned
// longest-or-nothing boundary policy (no backtracking past a rejection).
// oneWild builds a processor with a single wildcard entry — mirrors the spec
// scenarios, which assume one entry per pattern family (equivalent-sentinel
// entries are identical patterns and would shadow by length ordering).
func oneWild(t *testing.T, pattern, repl string) *wordProcessor {
	t.Helper()
	return newWordProcessorWithEntries(wildcardCfg(), []wordReplacementEntry{
		{orig: pattern, repl: repl, wildcardRe: compileWildcardPattern(pattern)},
	})
}

func TestApplyWordReplacement_WildcardMatrix(t *testing.T) {
	variants := []struct{ name, in string }{
		{"circle cb", "チ○ポ"},
		{"large circle", "チ◯ポ"},
		{"ideographic zero", "チ〇ポ"},
		{"black circle", "チ●ポ"},
		{"ascii asterisk", "チ*ポ"},
		{"fullwidth asterisk", "チ＊ポ"},
		{"multiplication x", "チ×ポ"},
		{"multiplication x heavy", "チ✕ポ"},
		{"heavy x", "チ✖ポ"},
	}
	for _, v := range variants {
		t.Run("glyph/"+v.name, func(t *testing.T) {
			assert.Equal(t, "チンポ", oneWild(t, "チ?ポ", "チンポ").Apply(v.in))
		})
	}

	cases := []struct {
		name, pattern, repl, in, want string
	}{
		// Run covers multiple glyphs; CJK embedding works.
		{"multi glyph run", "チ?ポ", "チンポ", "チ●●ポを咥える人妻", "チンポを咥える人妻"},
		{"embedded cjk", "ま?こ[1]", "まんこ", "彼女のま●こ[1]の話", "彼女のまんこの話"},
		// Sentinel runs collapse: entry チ??ポ fires on a single glyph.
		{"sentinel collapse", "チ??ポ", "ダブル", "チ●ポ", "ダブル"},
		// Fullwidth sentinel entry.
		{"fullwidth sentinel", "チ？ポ", "全角", "チ○ポ", "全角"},
		// Non-sentinel chars are literal: [1] required.
		{"literal suffix present", "ま?こ[1]", "まんこ", "ま○こ[1]", "まんこ"},
		{"literal suffix absent", "ま?こ[1]", "まんこ", "ま○こ2", "ま○こ2"},
		// Longest-or-nothing boundary policy: no retry-shorter leaking.
		{"no backtrack cjk", "チ?", "X", "チ○●x", "チ○●x"},
		{"no backtrack latin tail", "F?", "Fuck", "F***○d", "F***○d"},
		// #106-style guard still holds for wildcards.
		{"latin over-extension", "F?", "Fuck", "F****d", "F****d"},
		// Standalone censored runs replace (one entry covers F*** etc).
		{"standalone latin run", "F?", "Fuck", "F***", "Fuck"},
		{"spaced latin run", "F?", "Fuck", "彼は F*** が好き", "彼は Fuck が好き"},
		// Trailing CJK letter is a boundary; trailing Latin letter blocks.
		{"cjk seam replaces", "F?", "Fuck", "F●●ま", "Fuckま"},
		{"latin seam blocks", "F?", "Fuck", "F●●x", "F●●x"},
		// Wildcard entry with no sentinel acts as exact literal (bounded).
		{"no sentinel literal", "ワイルド", "W", "ワイルド", "W"},
		{"no sentinel blocked by latin", "ワイルド", "W", "ワイルドx", "ワイルドx"},
		{"no sentinel bounded in cjk", "ワイルド", "W", "彼女のワイルド時代", "彼女のW時代"},
		// Leading-boundary reject: Latin char directly before the match blocks.
		{"leading latin blocks", "?ポ", "Y", "x●ポ", "x●ポ"},
		{"leading cjk boundary passes", "?ポ", "Y", "彼●ポ", "彼Y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, oneWild(t, tc.pattern, tc.repl).Apply(tc.in))
		})
	}
}

// TestApplyWordReplacement_WildcardVsLiteralOrdering pins the documented
// mixed-cache rule: the longer pattern (by bytes) runs first regardless of mode.
func TestApplyWordReplacement_WildcardVsLiteralOrdering(t *testing.T) {
	// literal "チ●ポ" (longer) beats wildcard "チ?ポ" (shorter).
	wp := newWordProcessorWithEntries(wildcardCfg(), []wordReplacementEntry{
		{orig: "チ●ポ", repl: "LITERAL"},
		{orig: "チ?ポ", repl: "WILD", wildcardRe: compileWildcardPattern("チ?ポ")},
	})
	assert.Equal(t, "LITERAL", wp.Apply("チ●ポ"))
	assert.Equal(t, "WILD", wp.Apply("チ○ポ"))

	// Reverse construction order must not change the outcome.
	wp2 := newWordProcessorWithEntries(wildcardCfg(), []wordReplacementEntry{
		{orig: "チ?ポ", repl: "WILD", wildcardRe: compileWildcardPattern("チ?ポ")},
		{orig: "チ●ポ", repl: "LITERAL"},
	})
	assert.Equal(t, "LITERAL", wp2.Apply("チ●ポ"))
}

// TestCompileWildcardPattern_EmptyIsNil pins the compile guard individually
// (the loadCache skip is pinned by the hang canary below).
func TestCompileWildcardPattern_EmptyIsNil(t *testing.T) {
	assert.Nil(t, compileWildcardPattern(""))
	assert.NotNil(t, compileWildcardPattern("?"))
	assert.NotNil(t, compileWildcardPattern("チ?ポ"))
}

// TestWordProcessor_LoadCacheSkipsEmptyOriginal is the hang regression guard
// for review M1: an empty-original wildcard row must be dropped at loadCache
// (an empty regex would zero-width match and never advance the replace loop).
func TestWordProcessor_LoadCacheSkipsEmptyOriginal(t *testing.T) {
	repo := &modeFakeLookup{rows: []struct{ o, r, m string }{
		{"", "BOOM", "wildcard"},
		{"ok", "fine", "literal"},
	}}
	wp := NewWordProcessor(wildcardCfg(), repo)
	done := make(chan string, 1)
	go func() { done <- wp.Apply("anything at all") }()
	select {
	case got := <-done:
		assert.Equal(t, "fine", wp.Apply("ok"))
		_ = got
	case <-time.After(2 * time.Second):
		t.Fatal("Apply hung on empty-original wildcard entry")
	}
}

// TestWordProcessor_LoadCacheModes pins the repository→cache path: literal rows
// compile no matcher, wildcard rows compile, empty mode counts as literal.
func TestWordProcessor_LoadCacheModes(t *testing.T) {
	repo := &modeFakeLookup{rows: []struct{ o, r, m string }{
		{"old", "new", ""},
		{"チ?ポ", "チンポ", "wildcard"},
	}}
	wp := NewWordProcessor(wildcardCfg(), repo)
	assert.Equal(t, "new", wp.Apply("old"))
	assert.Equal(t, "チンポ", wp.Apply("チ●ポ"))
	assert.Equal(t, "unchanged", wp.Apply("unchanged"))
}

type modeFakeLookup struct {
	rows []struct{ o, r, m string }
}

func (f *modeFakeLookup) GetReplacementMap(context.Context) (map[string]string, error) {
	return nil, nil
}

func (f *modeFakeLookup) GetReplacementEntries(context.Context) ([]models.WordReplacement, error) {
	rows := make([]models.WordReplacement, 0, len(f.rows))
	for _, r := range f.rows {
		rows = append(rows, models.WordReplacement{Original: r.o, Replacement: r.r, MatchMode: r.m})
	}
	return rows, nil
}
