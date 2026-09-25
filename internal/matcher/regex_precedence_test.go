package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A user-specified custom regex outranks every automated tier (fused
// normalization, builtin pattern, tier-2 content-id) while keeping its
// pre-folding semantics: it sees the raw (unfolded) filename first, exactly
// as it did before fullwidth ASCII folding existed, and a miss is retried
// once against the folded form so halfwidth-written regexes also match
// fullwidth filenames.

// TestMatchFile_CustomRegexSeesRawUnfoldedName guards the pre-folding
// contract: a custom regex written against fullwidth spellings matches the
// raw fullwidth filename, and its capture is returned verbatim (uppercased)
// with no folding applied.
func TestMatchFile_CustomRegexSeesRawUnfoldedName(t *testing.T) {
	cfg := &Config{RegexEnabled: true, RegexPattern: `(ＲＣＴ-\d+)`}
	m, err := NewMatcher(cfg)
	require.NoError(t, err)

	got := matchOne(t, m, "ＲＣＴ-156.mkv")
	require.NotNil(t, got, "fullwidth-written custom regex must match the raw fullwidth name")
	assert.Equal(t, "ＲＣＴ-156", got.ID, "capture must be verbatim (uppercased), not folded")
	assert.Equal(t, "regex", got.MatchedBy)
	assert.Equal(t, 0, got.PartNumber)
	assert.Empty(t, got.PartSuffix)

	// A fullwidth character class sees the raw lowercase spelling and the
	// capture is uppercased in place, still fullwidth.
	classCfg := &Config{RegexEnabled: true, RegexPattern: `([Ａ-Ｚａ-ｚ]+-\d+)`}
	classMatcher, err := NewMatcher(classCfg)
	require.NoError(t, err)

	got = matchOne(t, classMatcher, "ｒｃｔ-156.mkv")
	require.NotNil(t, got, "fullwidth character class must match the raw fullwidth name")
	assert.Equal(t, "ＲＣＴ-156", got.ID, "fullwidth lowercase capture must uppercase verbatim, not fold")
	assert.Equal(t, "regex", got.MatchedBy)
}

// TestMatchFile_CustomRegexFoldedRetry lets a halfwidth-written custom
// regex match a fullwidth filename through the folded retry, still ahead
// of every automated tier.
func TestMatchFile_CustomRegexFoldedRetry(t *testing.T) {
	cfg := &Config{RegexEnabled: true, RegexPattern: `(IPX-\d+)`}
	m, err := NewMatcher(cfg)
	require.NoError(t, err)

	got := matchOne(t, m, "ＩＰＸ-535.mkv")
	require.NotNil(t, got, "halfwidth-written custom regex must match via the folded retry")
	assert.Equal(t, "IPX-535", got.ID)
	assert.Equal(t, "regex", got.MatchedBy, "the folded retry is still the custom regex, not the builtin tier")
	assert.Equal(t, 0, got.PartNumber)
	assert.Empty(t, got.PartSuffix)
}

// TestMatchFile_CustomRegexOutranksAutomatedTiers pins the precedence
// requirement: an enabled custom regex wins over the automated tiers even
// for content-id-shaped filenames that tier-2 would otherwise claim.
func TestMatchFile_CustomRegexOutranksAutomatedTiers(t *testing.T) {
	cfg := &Config{RegexEnabled: true, RegexPattern: `(rct\d+)`}
	m, err := NewMatcher(cfg)
	require.NoError(t, err)

	got := matchOne(t, m, "1rct00156h.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT00156", got.ID, "custom regex capture must win over the automated content id")
	assert.Equal(t, "regex", got.MatchedBy)

	// Control: without a custom regex the same filename resolves to the
	// tier-2 content id, proving the regex produced the different id.
	plain, err := NewMatcher(&Config{})
	require.NoError(t, err)
	plainGot := matchOne(t, plain, "1rct00156h.mkv")
	require.NotNil(t, plainGot)
	assert.Equal(t, "1RCT00156H", plainGot.ID)
	assert.Equal(t, "contentid", plainGot.MatchedBy)
}

// TestMatchFile_CustomRegexDisabledChangesNothing guards the no-custom-
// regex path: folding and the automated tiers behave exactly as before,
// whether the regex is disabled or simply absent/non-matching.
func TestMatchFile_CustomRegexDisabledChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *Config
	}{
		{"disabled with pattern", &Config{RegexEnabled: false, RegexPattern: `(rct\d+)`}},
		{"absent pattern", &Config{RegexEnabled: true, RegexPattern: ""}},
		{"enabled but non-matching", &Config{RegexEnabled: true, RegexPattern: `(ZZZ-\d+)`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := NewMatcher(tc.cfg)
			require.NoError(t, err)

			got := matchOne(t, m, "ＲＣＴ-156-ＨＤ.mkv")
			require.NotNil(t, got, "automated tiers must still fold and match")
			assert.Equal(t, "RCT-156H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
			assert.Equal(t, "builtin", got.MatchedBy)
		})
	}
}

// TestMatchString_CustomRegexRawThenFolded mirrors the MatchFile coverage
// for the MatchString helper.
func TestMatchString_CustomRegexRawThenFolded(t *testing.T) {
	t.Run("raw attempt preserved", func(t *testing.T) {
		cfg := &Config{RegexEnabled: true, RegexPattern: `(ＲＣＴ-\d+)`}
		m, err := NewMatcher(cfg)
		require.NoError(t, err)
		assert.Equal(t, "ＲＣＴ-156", m.MatchString("ＲＣＴ-156.mp4"), "fullwidth-written regex must see the raw name")

		classCfg := &Config{RegexEnabled: true, RegexPattern: `([Ａ-Ｚａ-ｚ]+-\d+)`}
		classMatcher, err := NewMatcher(classCfg)
		require.NoError(t, err)
		assert.Equal(t, "ＲＣＴ-156", classMatcher.MatchString("ｒｃｔ-156.mp4"), "capture must be verbatim uppercased, not folded")
	})

	t.Run("folded retry", func(t *testing.T) {
		cfg := &Config{RegexEnabled: true, RegexPattern: `(IPX-\d+)`}
		m, err := NewMatcher(cfg)
		require.NoError(t, err)
		assert.Equal(t, "IPX-535", m.MatchString("ＩＰＸ-535.mp4"), "halfwidth-written regex must match via the folded retry")
		// A fullwidth extension folds and strips for the folded candidate only.
		assert.Equal(t, "IPX-535", m.MatchString("ＩＰＸ-５３５．ｍｋｖ"), "folded retry must fold+strip fullwidth extensions")
	})

	t.Run("outranks tier-2 content id", func(t *testing.T) {
		cfg := &Config{RegexEnabled: true, RegexPattern: `(rct\d+)`}
		m, err := NewMatcher(cfg)
		require.NoError(t, err)
		assert.Equal(t, "RCT00156", m.MatchString("1rct00156h.mp4"), "custom regex must capture the intended id")

		plain, err := NewMatcher(&Config{})
		require.NoError(t, err)
		assert.Equal(t, "1RCT00156H", plain.MatchString("1rct00156h.mp4"), "control: automated path still yields the content id")
	})

	t.Run("disabled changes nothing", func(t *testing.T) {
		plain, err := NewMatcher(&Config{RegexEnabled: false, RegexPattern: `(IPX-\d+)`})
		require.NoError(t, err)
		assert.Equal(t, "RCT-156H", plain.MatchString("ＲＣＴ-156-ＨＤ.mkv"))
		assert.Equal(t, "IPX-535", plain.MatchString("ＩＰＸ-５３５．ｍｋｖ"), "folding + extension strip must behave as before")
		assert.Equal(t, "1RCT00156H", plain.MatchString("１ｒｃｔ００１５６ｈ.mkv"))
	})
}
