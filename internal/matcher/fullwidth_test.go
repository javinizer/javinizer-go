package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Fullwidth ASCII spellings (common in JP-sourced filenames) must behave like
// their halfwidth counterparts in every matcher tier, while kana and kanji
// survive untouched and plain halfwidth input is unaffected.
func TestMatchFile_FullwidthSpellingsFoldToASCII(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		wantID string
		marker string
	}{
		{"RCT-156-ＨＤ.mkv", "RCT-156H", "HD"},
		{"RCT-156-ｈｄ.mkv", "RCT-156H", "HD"},
		{"RCT-156-Ｈ.mkv", "RCT-156H", "H"},
		{"DV-818-ＡＩ.mkv", "DV-818AI", "AI"},
		{"ＲＣＴ-156-HD.mkv", "RCT-156H", "HD"},
		{"ＲＣＴ-１５６-ＨＤ.mkv", "RCT-156H", "HD"},
		{"ＲＣＴ１５６ＨＤ.mkv", "RCT-156H", "HD"},
		{"ＲＣＴ-156-ＨＤ アイ・無修正.mkv", "RCT-156H", "HD"},
		// Control: legitimately halfwidth input is unaffected.
		{"RCT-156-HD.mkv", "RCT-156H", "HD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, 0, got.PartNumber)
			assert.Empty(t, got.MultipartPattern)
			assert.Equal(t, tc.wantID, m.MatchString(tc.name))
		})
	}

	// Tier-2 raw and plain catalog spellings fold too.
	assert.Equal(t, "1RCT00156H", m.MatchString("１ｒｃｔ００１５６ｈ.mkv"))
	assert.Equal(t, "IPX-535", m.MatchString("ＩＰＸ-５３５.mkv"))

	// Kana and kanji survive the fold untouched; ASCII-only input is
	// returned unchanged.
	assert.Equal(t, "RCT-156-HD アイ・無修正", foldFullwidthASCII("ＲＣＴ-156-ＨＤ アイ・無修正"))
	assert.Equal(t, "RCT-156-HD", foldFullwidthASCII("RCT-156-HD"))
}

// A fullwidth extension (．ｍｋｖ) must fold and strip like an ASCII one in
// MatchFile: the fold precedes the video-extension strip — mirroring
// MatchString — so the extension resolves even when FileMatchInfo.Extension
// is empty (a caller that derived no extension) or holds the folded ASCII
// ".mkv" while Name keeps the raw fullwidth spelling (the scanner's folded
// extension cannot be trimmed from the raw name). Without the fold-first
// order the folded ".mkv" stays inside the stem and buries a trailing part
// number behind it (the suffix would read "-2.mkv" and part 2 is lost).
func TestMatchFile_FullwidthExtensionFoldThenStrip(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	fwName := "RCT-156-HD-2．ｍｋｖ"
	for _, ext := range []string{"", ".mkv"} {
		t.Run("extension "+ext, func(t *testing.T) {
			file := models.FileMatchInfo{Path: "/v/" + fwName, Name: fwName, Extension: ext}
			got := m.MatchFile(file)
			require.NotNil(t, got, "a fullwidth extension must resolve like an ASCII one")
			assert.Equal(t, "RCT-156H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
			assert.Equal(t, 2, got.PartNumber, "the part number behind the fullwidth extension survives the fold+strip")
			assert.Equal(t, "builtin", got.MatchedBy)
			assert.Equal(t, "RCT-156H", m.MatchString(fwName))
		})
	}

	// Fullwidth spelling throughout the name — including the part digit and
	// the extension — keeps resolving, and the control: a plain ASCII
	// extension is unaffected.
	for _, tc := range []struct {
		name, ext, wantID, marker string
		part                      int
	}{
		{"ＲＣＴ-156-ＨＤ-２．ｍｋｖ", ".mkv", "RCT-156H", "HD", 2},
		{"RCT-156-HD-2.mkv", ".mkv", "RCT-156H", "HD", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := m.MatchFile(models.FileMatchInfo{Path: "/v/" + tc.name, Name: tc.name, Extension: tc.ext})
			require.NotNil(t, got)
			assert.Equal(t, tc.wantID, got.ID)
			assert.Equal(t, tc.marker, got.RemasterMarker)
			assert.Equal(t, tc.part, got.PartNumber)
		})
	}

	// The custom regex keeps its raw-first contract with a fullwidth
	// extension present: the first attempt sees the raw name with the
	// unfoldable extension, and the folded retry sees the folded,
	// extension-stripped stem.
	rawCfg := &Config{RegexEnabled: true, RegexPattern: `(ＲＣＴ-\d+)`}
	rawMatcher, err := NewMatcher(rawCfg)
	require.NoError(t, err)
	got := rawMatcher.MatchFile(models.FileMatchInfo{Path: "/v/ＲＣＴ-156．ｍｋｖ", Name: "ＲＣＴ-156．ｍｋｖ", Extension: ""})
	require.NotNil(t, got, "a fullwidth-written custom regex must see the raw name including the fullwidth extension")
	assert.Equal(t, "ＲＣＴ-156", got.ID)
	assert.Equal(t, "regex", got.MatchedBy)

	foldCfg := &Config{RegexEnabled: true, RegexPattern: `(RCT-\d+)`}
	foldMatcher, err := NewMatcher(foldCfg)
	require.NoError(t, err)
	got = foldMatcher.MatchFile(models.FileMatchInfo{Path: "/v/ＲＣＴ-156．ｍｋｖ", Name: "ＲＣＴ-156．ｍｋｖ", Extension: ""})
	require.NotNil(t, got, "a halfwidth-written custom regex must match via the folded, extension-stripped retry")
	assert.Equal(t, "RCT-156", got.ID)
	assert.Equal(t, "regex", got.MatchedBy)
}

// A fullwidth slash (／) inside a filename is a legal character, not a path
// separator. MatchString must take the basename before folding — mirroring
// MatchFile — so the folded slash cannot make filepath.Base discard the
// ID-bearing segment of a JP-sourced name.
func TestMatchString_FullwidthSlashStaysInsideBasename(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)

	for _, tc := range []struct {
		input string
		want  string
	}{
		{"IPX-535／sample.mkv", "IPX-535"},
		{"dir/IPX-535／sample.mkv", "IPX-535"},
		{"/abs/dir/IPX-535／sample.mkv", "IPX-535"},
		{"dir/IPX-535／sample．ｍｋｖ", "IPX-535"},
		// Control: pure-ASCII paths behave exactly as before.
		{"dir/IPX-535.mkv", "IPX-535"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, m.MatchString(tc.input))
		})
	}

	// MatchString must agree with MatchFile, which basenames before folding
	// and keeps the folded slash inside the name.
	file := models.FileMatchInfo{Path: "/v/dir/IPX-535／sample.mkv", Name: "IPX-535／sample.mkv", Extension: ".mkv"}
	got := m.MatchFile(file)
	require.NotNil(t, got, "MatchFile finds the id despite the folded slash")
	assert.Equal(t, "IPX-535", got.ID)
	assert.Equal(t, "IPX-535", m.MatchString(file.Path))
}
