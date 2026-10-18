package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A separated remaster id followed by a profile-bearing codec tag must not
// hand its post-separator fragment to the catalog scan as a replacement
// id: DTS-HD192 and AAC-LC192 split at their separator, so the HD192/LC192
// fragments arrive alone while the codec-profile prefix belongs to them —
// the veto span extends back over the prefix and the quality vocabulary
// decides the compound as a whole (round 33a, extending round 13's web
// compound mechanism). The profile word may ride inside the fragment
// (DTS-HD192), inside the prefix (DTS-HD MA768), or fused across the
// split (DTS-HDMA768), and both entry points agree on every layout.
func TestCodecProfileCompoundFragmentVetoed(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	for _, name := range []string{
		// The finding's spellings: the profile rides inside the fragment.
		"ABC.123.HD DTS-HD192.mkv",
		"ABC.123.HD AAC-LC192.mkv",
		// The probed profile variants behind the same codec heads.
		"ABC.123.HD DTS-MA192.mkv",
		"ABC.123.HD AAC-HE192.mkv",
		"ABC.123.HD AAC-SBR192.mkv",
		"ABC.123.HD DTS-X192.mkv",
		"ABC.123.HD DTS-HDMA768.mkv",
		// The profile word may ride inside the prefix instead: DTS-HD
		// MA768 leaves the MA768 fragment standing.
		"ABC.123.HD DTS-HD MA768.mkv",
		"ABC.123.HD DTS-HD-MA768.mkv",
		"ABC.123.HD DTS-HD.MA768.mkv",
		"ABC.123.HD DTS-HD-HRA768.mkv",
		// The Dolby/TrueHD profile family rides the same span.
		"ABC.123.HD TRUEHD-ATMOS768.mkv",
		"ABC.123.HD EAC3-ATMOS768.mkv",
		"ABC.123.HD DDP-ATMOS768.mkv",
		"ABC.123.HD DD-EX448.mkv",
		"ABC.123.HD AC3-EX448.mkv",
		// The codec-head spellings the vocabulary already vetoed alone
		// keep their result: the candidate itself matched.
		"ABC.123.HD HE-AAC192.mkv",
		"ABC.123.HD AC3-EAC3640.mkv",
		"ABC.123.HD AAC-HE-AAC192.mkv",
		// Channel-count tails behind the profile never displaced the
		// id (their fragments are too short for the id grammars) and
		// still do not.
		"ABC.123.HD DTS-HD MA5.1.mkv",
		"ABC.123.HD AAC-LC 5.1.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
			assert.Equal(t, "ABC-123H", m.MatchString(name))
		})
	}
}

// The codec-profile veto stays bounded. The bare codec-rate spellings of
// rounds 7/16b stay metadata behind the separated id, the bare id shapes
// of the real hd/lc/ma/x/dd/dts series stay id grammar (the veto span
// extends only over a codec head with a leading boundary, and the
// compound itself requires profile letters behind the head's separator),
// and the round-13 web compounds keep their own span and bounds.
func TestCodecProfileVetoBounds(t *testing.T) {
	m, err := NewMatcher(&Config{})
	require.NoError(t, err)
	// Rounds 7/16b controls: the bare codec-rate spellings stay metadata
	// behind the separated remaster id.
	for _, name := range []string{
		"ABC.123.HD DTS768.mkv",
		"ABC.123.HD AC3640.mkv",
		"ABC.123.HD PCM192.mkv",
		"ABC.123.HD DDP5.1.mkv",
		"ABC.123.HD EAC3.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := matchOne(t, m, name)
			require.NotNil(t, got)
			assert.Equal(t, "ABC-123H", got.ID)
			assert.Equal(t, "HD", got.RemasterMarker)
		})
	}
	// The profile words hd, lc, ma, he, x, and ex and the codec heads
	// dts, aac, and dd are real series in the r18.dev content-id prefix
	// lookup; their bare id shapes keep matching as ids, and a real
	// trailing id still replaces the separated remaster id exactly as
	// before. DTS-24 keeps the bare literals' existing veto.
	for _, tc := range []struct{ name, id string }{
		{"ABC.123.HD HD1080.mkv", "HD1080"},
		{"ABC.123.HD LC1234.mkv", "LC1234"},
		{"ABC.123.HD MA1234.mkv", "MA1234"},
		{"ABC.123.HD X192.mkv", "X192"},
		{"ABC.123.HD DD-24.mkv", "DD-24"},
		{"ABC.123.HD AC307.mkv", "AC307"},
		{"ABC.123.HD DTS00123.mkv", "DTS00123"},
		{"ABC.123.HD DTS-24.mkv", "ABC-123H"},
		{"ABC.123.HD XDTS-HD192.mkv", "HD192"},
		{"ABC.123.HD 189dts00087.mkv", "189DTS00087"},
		{"ABC.123.HD WEB-DL2160.mkv", "ABC-123H"},
		{"ABC.123.HD WEB-DL12345.mkv", "DL12345"},
		{"ABC.123.HD BD-RIP1080.mkv", "ABC-123H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := matchOne(t, m, tc.name)
			require.NotNil(t, got)
			assert.Equal(t, tc.id, got.ID)
			assert.Equal(t, tc.id, m.MatchString(tc.name))
		})
	}
}
