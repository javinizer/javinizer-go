package models

// POSTER-WRITE-HARDENING codex PR#215 wave-19 (P2) — the RestorePending
// payload gains a KIND ("clean" legacy default vs "rearm_refused"). These
// tests pin the payload shape, the normalization (legacy entries default to
// clean; unknown kinds conservatively read as rearm-refused), and the
// one-way merge discipline (rearm-refused upgrades, never downgrades).

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplacementEntryPendingKindW19_NormalizationTable(t *testing.T) {
	cases := []struct {
		name  string
		entry ReplacementEntry
		want  string
	}{
		{"not pending has no kind", ReplacementEntry{}, ""},
		{"not pending ignores a kind field", ReplacementEntry{RestorePendingKind: RestorePendingKindRearmRefused}, ""},
		{"legacy pending (empty kind) defaults to clean", ReplacementEntry{RestorePending: true}, RestorePendingKindClean},
		{"explicit clean stays clean", ReplacementEntry{RestorePending: true, RestorePendingKind: RestorePendingKindClean}, RestorePendingKindClean},
		{"rearm-refused round-trips", ReplacementEntry{RestorePending: true, RestorePendingKind: RestorePendingKindRearmRefused}, RestorePendingKindRearmRefused},
		{"prune round-trips", ReplacementEntry{RestorePending: true, RestorePendingKind: RestorePendingKindPrune}, RestorePendingKindPrune},
		{
			"unknown kinds conservatively read as rearm-refused",
			ReplacementEntry{RestorePending: true, RestorePendingKind: "future-kind-from-a-newer-build"},
			RestorePendingKindRearmRefused,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.entry.PendingKind())
		})
	}
}

func TestReplacementEntrySetRestorePendingW19_MergeDiscipline(t *testing.T) {
	t.Run("clean mark on a non-pending entry writes no kind field", func(t *testing.T) {
		e := ReplacementEntry{}
		require.True(t, e.SetRestorePending(RestorePendingKindClean))
		require.True(t, e.RestorePending)
		require.Equal(t, "", e.RestorePendingKind, "the clean kind stays unwritten (omitempty) for legacy blob parity")
		require.Equal(t, RestorePendingKindClean, e.PendingKind())
	})

	t.Run("prune mark writes an explicit kind", func(t *testing.T) {
		e := ReplacementEntry{}
		require.True(t, e.SetRestorePending(RestorePendingKindPrune))
		require.Equal(t, RestorePendingKindPrune, e.RestorePendingKind)
		require.False(t, e.SetRestorePending(RestorePendingKindClean), "prune intent must not downgrade to restore-clean")

		clean := ReplacementEntry{RestorePending: true}
		require.True(t, clean.SetRestorePending(RestorePendingKindPrune), "prune cleanup upgrades a legacy clean marker")
		require.Equal(t, RestorePendingKindPrune, clean.PendingKind())

		refused := ReplacementEntry{RestorePending: true, RestorePendingKind: RestorePendingKindRearmRefused}
		require.False(t, refused.SetRestorePending(RestorePendingKindPrune), "prune cannot reclaim a name already proven unowned")
	})

	t.Run("rearm-refused mark on a non-pending entry writes the kind", func(t *testing.T) {
		e := ReplacementEntry{}
		require.True(t, e.SetRestorePending(RestorePendingKindRearmRefused))
		require.True(t, e.RestorePending)
		require.Equal(t, RestorePendingKindRearmRefused, e.RestorePendingKind)
	})

	t.Run("identical re-marks are no-ops", func(t *testing.T) {
		clean := ReplacementEntry{}
		require.True(t, clean.SetRestorePending(RestorePendingKindClean))
		require.False(t, clean.SetRestorePending(RestorePendingKindClean), "clean → clean is a no-change merge")

		refused := ReplacementEntry{}
		require.True(t, refused.SetRestorePending(RestorePendingKindRearmRefused))
		require.False(t, refused.SetRestorePending(RestorePendingKindRearmRefused), "rearm-refused → rearm-refused is a no-change merge")
	})

	t.Run("clean upgrades to rearm-refused, never the reverse", func(t *testing.T) {
		e := ReplacementEntry{}
		require.True(t, e.SetRestorePending(RestorePendingKindClean))
		require.True(t, e.SetRestorePending(RestorePendingKindRearmRefused),
			"a refused re-arm vacated the name: the clean mark MUST upgrade")
		require.Equal(t, RestorePendingKindRearmRefused, e.RestorePendingKind)

		require.False(t, e.SetRestorePending(RestorePendingKindClean),
			"a name once proven unowned never re-enters the removal path")
		require.Equal(t, RestorePendingKindRearmRefused, e.PendingKind(), "no downgrade")
	})
}

func TestReplacementEntryPendingKindW19_PayloadShape(t *testing.T) {
	t.Run("legacy blob parses as clean and re-marshals byte-identical", func(t *testing.T) {
		legacy := `{"replacements":[{"destination":"/d/poster.jpg","backup":"/d/poster.jpg.dlbak.0123456789abcdef","dest_seq":1,"restore_pending":true}]}`
		gf, err := ParseGeneratedFiles(legacy)
		require.NoError(t, err)
		require.Len(t, gf.Replacements, 1)
		require.True(t, gf.Replacements[0].RestorePending)
		require.Equal(t, RestorePendingKindClean, gf.Replacements[0].PendingKind(),
			"a pre-wave-19 pending entry carries no kind and defaults to clean")
		require.Equal(t, legacy, MarshalLedgerJSON(gf),
			"clean-kind entries never materialize the kind field — wave-18 blob parity")
	})

	t.Run("prune blob shape", func(t *testing.T) {
		e := ReplacementEntry{Destination: "/d/poster.jpg", Backup: "/d/poster.jpg.dlbak.0123456789abcdef", DestSeq: 1}
		require.True(t, e.SetRestorePending(RestorePendingKindPrune))
		blob := MarshalLedgerJSON(GeneratedFilesJSON{Replacements: []ReplacementEntry{e}})
		require.Contains(t, blob, `"restore_pending_kind":"prune"`)
		parsed, err := ParseGeneratedFiles(blob)
		require.NoError(t, err)
		require.Equal(t, RestorePendingKindPrune, parsed.Replacements[0].PendingKind())
	})

	t.Run("rearm-refused blob shape", func(t *testing.T) {
		e := ReplacementEntry{Destination: "/d/poster.jpg", Backup: "/d/poster.jpg.dlbak.0123456789abcdef", DestSeq: 1}
		require.True(t, e.SetRestorePending(RestorePendingKindRearmRefused))
		blob := MarshalLedgerJSON(GeneratedFilesJSON{Replacements: []ReplacementEntry{e}})
		require.Equal(t,
			`{"replacements":[{"destination":"/d/poster.jpg","backup":"/d/poster.jpg.dlbak.0123456789abcdef","dest_seq":1,"restore_pending":true,"restore_pending_kind":"rearm_refused"}]}`,
			blob, "the persisted pending marker carries kind exactly once, after restore_pending")

		parsed, err := ParseGeneratedFiles(blob)
		require.NoError(t, err)
		require.Equal(t, RestorePendingKindRearmRefused, parsed.Replacements[0].PendingKind(), "round-trip keeps the kind")
	})
}
