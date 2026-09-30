package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HasIdentityPin is the single presence classification for the hard-link
// pin: the explicit identity_pinned marker for new blobs, then the pre-marker
// evidence in precedence order (the ModUnix sentinel, then the strong bool).
// Every other pin shape, and every legacy row carrying NO identity evidence,
// must classify exactly as it always did (codex P2, PRRT_kwDORn9KaM6nHyl5).
func TestDeleteEntry_HasIdentityPin(t *testing.T) {
	testCases := []struct {
		name  string
		entry DeleteEntry
		want  bool
	}{
		{"explicit marker with an all-zero tuple", DeleteEntry{Path: "/lib/m.mkv", IdentityPinned: true}, true},
		{"explicit marker with a full epoch strong tuple", DeleteEntry{Path: "/lib/m.mkv", IdentityPinned: true, IdentityStrong: true, IdentityDev: 7, IdentityIno: 9, IdentitySize: 42}, true},
		{"legacy sentinel mtime only", DeleteEntry{Path: "/lib/m.mkv", IdentitySize: 42, IdentityModUnix: 1700000000}, true},
		{"legacy strong evidence overrides the zeroed sentinel", DeleteEntry{Path: "/lib/m.mkv", IdentityStrong: true, IdentityDev: 7, IdentityIno: 9, IdentitySize: 42}, true},
		{"legacy weak epoch row keeps its old reading", DeleteEntry{Path: "/lib/m.mkv", IdentitySize: 42}, false},
		{"zero value", DeleteEntry{}, false},
		{"sha pin", DeleteEntry{Path: "/lib/m.mkv", SHA256: "aa11"}, false},
		{"symlink pin", DeleteEntry{Path: "/lib/m.mkv", LinkTarget: "/in/m.mkv"}, false},
		{"copy partial pin", DeleteEntry{Path: "/lib/m.mkv", CopySize: 42, CopyPartialSHA256: "bb22"}, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.entry.HasIdentityPin())
		})
	}
}

// The epoch-mtime hard-link pin must carry its presence through the FULL
// journal round-trip — unmarshal -> marshal -> unmarshal — without
// degenerating, and pre-marker blobs must decode to exactly the
// classification their own evidence implies: the strong tuple overrides the
// zeroed sentinel; the evidence-free weak row keeps its old reading.
func TestDeleteEntryJSON_IdentityPinPresenceContract(t *testing.T) {
	epochStrong := DeleteEntry{Path: "/lib/hard.mkv", IdentityPinned: true, IdentityStrong: true, IdentityDev: 0xBEEF, IdentityIno: 0xCAFE, IdentitySize: 42}
	epochWeak := DeleteEntry{Path: "/lib/weak.mkv", IdentityPinned: true, IdentitySize: 42}

	t.Run("epoch pins stay present across repeated marshal decode cycles", func(t *testing.T) {
		blob := MarshalLedgerJSON(GeneratedFilesJSON{PlannedDeletes: []DeleteEntry{epochStrong, epochWeak}})
		assert.Equal(t, 2, strings.Count(blob, "identity_pinned"), "the marker materializes even though every tuple leg serializes zero")
		for cycle := 0; cycle < 2; cycle++ {
			gf, err := ParseGeneratedFiles(blob)
			require.NoError(t, err)
			require.Len(t, gf.PlannedDeletes, 2)
			for _, entry := range gf.PlannedDeletes {
				assert.Zero(t, entry.IdentityModUnix, "the epoch tuple decodes to a zero mtime")
				assert.True(t, entry.HasIdentityPin(), "decode cycle %d: the presence marker survived", cycle)
			}
			blob = MarshalLedgerJSON(gf)
		}
		gf, err := ParseGeneratedFiles(blob)
		require.NoError(t, err)
		assert.Equal(t, []DeleteEntry{epochStrong, epochWeak}, gf.PlannedDeletes, "the round-trip is byte-faithful")
	})

	t.Run("pre-marker strong epoch blob decodes to present", func(t *testing.T) {
		// The exact row pre-marker code journaled for an epoch-dated source on
		// an identity-exposing platform: identity_mod_unix serialized as
		// absent, identity_strong survived the omitempty.
		gf, err := ParseGeneratedFiles(`{"planned_deletes":[{"path":"/lib/hard.mkv","sha256":"","identity_strong":true,"identity_dev":48879,"identity_ino":51966,"identity_size":42}]}`)
		require.NoError(t, err)
		require.Len(t, gf.PlannedDeletes, 1)
		entry := gf.PlannedDeletes[0]
		assert.False(t, entry.IdentityPinned)
		assert.Zero(t, entry.IdentityModUnix)
		assert.True(t, entry.HasIdentityPin(), "the strong tuple is unambiguous hard-link evidence and must override the zeroed sentinel")
	})

	t.Run("pre-marker sentinel blob decodes to present", func(t *testing.T) {
		gf, err := ParseGeneratedFiles(`{"planned_deletes":[{"path":"/lib/legacy.mkv","sha256":"","identity_size":42,"identity_mod_unix":1700000000}]}`)
		require.NoError(t, err)
		require.Len(t, gf.PlannedDeletes, 1)
		assert.True(t, gf.PlannedDeletes[0].HasIdentityPin(), "the original sentinel keeps classifying")
	})

	t.Run("pre-marker weak epoch blob keeps its old classification", func(t *testing.T) {
		gf, err := ParseGeneratedFiles(`{"planned_deletes":[{"path":"/lib/weak.mkv","sha256":"","identity_size":42}]}`)
		require.NoError(t, err)
		require.Len(t, gf.PlannedDeletes, 1)
		assert.False(t, gf.PlannedDeletes[0].HasIdentityPin(), "pre-marker code recorded no evidence for this shape — the blob must NOT silently flip to removal")
	})
}
