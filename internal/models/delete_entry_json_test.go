package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The DeleteEntry pin shapes ride the GeneratedFilesJSON persistence
// contract: legacy blobs (path+sha256 only) parse into zero-value pin
// fields, the new shapes round-trip, and a legacy-shaped entry marshals
// byte-identically to its pre-shape form (omitempty keeps every new key
// absent), so mixed-version journal reads never misjudge an old pin.
func TestDeleteEntryJSON_PinShapeContract(t *testing.T) {
	t.Run("legacy blob parses into zero pin fields", func(t *testing.T) {
		gf, err := ParseGeneratedFiles(`{"planned_deletes":[{"path":"/lib/m.mkv","sha256":"aa11"}]}`)
		require.NoError(t, err)
		require.Len(t, gf.PlannedDeletes, 1)
		entry := gf.PlannedDeletes[0]
		assert.Equal(t, "/lib/m.mkv", entry.Path)
		assert.Equal(t, "aa11", entry.SHA256)
		assert.Empty(t, entry.LinkTarget)
		assert.False(t, entry.IdentityStrong)
		assert.Zero(t, entry.IdentityModUnix)
		assert.Empty(t, entry.CopyPartialSHA256)
	})

	t.Run("legacy-shaped entry marshals byte-identically", func(t *testing.T) {
		blob := MarshalLedgerJSON(GeneratedFilesJSON{PlannedDeletes: []DeleteEntry{{Path: "/lib/m.mkv", SHA256: "aa11"}}})
		assert.Equal(t, `{"planned_deletes":[{"path":"/lib/m.mkv","sha256":"aa11"}]}`, blob)
	})

	t.Run("every pin shape round-trips", func(t *testing.T) {
		entries := []DeleteEntry{
			{Path: "/lib/soft.mkv", LinkTarget: "/incoming/soft.mkv"},
			{Path: "/lib/hard.mkv", IdentityStrong: true, IdentityDev: 0xBEEF, IdentityIno: 0xCAFE, IdentitySize: 42, IdentityModUnix: 1700000000},
			{Path: "/lib/weak.mkv", IdentitySize: 42, IdentityModUnix: 1700000000},
			{Path: "/lib/copy.mkv", CopySize: 4096, CopyPartialSHA256: "bb22"},
		}
		blob := MarshalLedgerJSON(GeneratedFilesJSON{PlannedDeletes: entries})
		got, err := ParseGeneratedFiles(blob)
		require.NoError(t, err)
		assert.Equal(t, entries, got.PlannedDeletes)
		// omitempty keeps mutually exclusive shapes from leaking zero keys
		// into each other's blobs (the journal stays self-describing).
		assert.NotContains(t, blob, `"sha256":"bb22"`)
		assert.Equal(t, 1, strings.Count(blob, "link_target"))
		assert.Equal(t, 2, strings.Count(blob, "identity_mod_unix"), "both identity pins carry the tuple")
		assert.Equal(t, 1, strings.Count(blob, "identity_dev"), "only the strong pin carries the kernel pair")
	})
}
