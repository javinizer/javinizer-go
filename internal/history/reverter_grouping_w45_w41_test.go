package history

// POSTER-WRITE-HARDENING wave-45 (codex P2, PR#215 finding F2) — the
// replacement-restore groupings freeze destination-key semantics for a whole
// restoreReplacementJournal invocation: fsutil.DestKey used to re-probe per
// ENTRY, and a first-call-fails/second-call-succeeds probe mixture sorted one
// file's case-variant cousins into separate groups whose Go map iteration
// then restored stacked chains in a nondeterministic interleave. Grouping now
// resolves each root's posture exactly once per invocation, and the restore
// loops walk a deterministic DestSeq-descending group order.

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Probe-seam mutation is process-global — never parallel.
func TestGroupReplacementEntriesW45_OneProbePosturePerInvocation(t *testing.T) {
	root := t.TempDir()
	lower := filepath.Join(root, "poster.jpg")
	upper := filepath.Join(root, "Poster.jpg")
	reps := []models.ReplacementEntry{
		{Destination: lower, Backup: filepath.Join(root, "backup.1"), DestSeq: 1},
		{Destination: upper, Backup: filepath.Join(root, "backup.2"), DestSeq: 2},
	}

	prevProbe := fsutil.CaseSensitiveProbe
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = prevProbe
		fsutil.ResetCaseSensitivityCache()
	})
	fsutil.ResetCaseSensitivityCache()

	caseCalls := 0
	fsutil.CaseSensitiveProbe = func(string) (bool, error) {
		caseCalls++
		if caseCalls == 1 {
			return false, errors.New("transient probe outage")
		}
		return false, nil // definitive INSENSITIVE on recovery
	}

	byDest, spelling, order := groupReplacementEntries(reps)
	require.Equal(t, 1, caseCalls,
		"one probe per root per invocation — the uncached transient failure froze the conservative posture")
	require.Len(t, byDest, 2,
		"under the frozen conservative posture both case variants stay independent chains — never a mixed-posture split")
	require.ElementsMatch(t, []string{lower, upper}, mapValues(spelling))
	require.Len(t, order, 2)

	byDest2, _, _ := groupReplacementEntries(reps)
	require.Equal(t, 2, caseCalls, "the recovered probe re-resolves on the next invocation")
	require.Len(t, byDest2, 1, "the definitive insensitive posture folds both cousin spellings into ONE group")
	for _, entries := range byDest2 {
		require.Len(t, entries, 2)
		require.Equal(t, int64(2), entries[0].DestSeq, "in-group restore order is DestSeq descending")
		require.Equal(t, int64(1), entries[1].DestSeq)
	}
}

// The restore loops' iteration order is fully deterministic: highest
// journaled DestSeq first (true reverse replace order), key-ascending on
// ties — independent of both Go map seeding and journal entry order.
func TestGroupReplacementEntriesW45_DeterministicGroupOrder(t *testing.T) {
	root := t.TempDir()
	destA := filepath.Join(root, "a1", "poster.jpg") // stacked chain, max seq 7
	destB := filepath.Join(root, "b1", "poster.jpg") // max seq 5
	destC := filepath.Join(root, "c1", "poster.jpg") // max seq 5 — tie with B
	reps := []models.ReplacementEntry{
		{Destination: destB, Backup: "bak-b", DestSeq: 5},
		{Destination: destA, Backup: "bak-a2", DestSeq: 7},
		{Destination: destC, Backup: "bak-c", DestSeq: 5},
		{Destination: destA, Backup: "bak-a1", DestSeq: 2},
	}
	reversed := []models.ReplacementEntry{reps[3], reps[2], reps[1], reps[0]}

	keyA, keyB, keyC := fsutil.DestKey(destA), fsutil.DestKey(destB), fsutil.DestKey(destC)
	require.NotEqual(t, keyB, keyC)
	first, second := keyB, keyC
	if keyC < keyB {
		first, second = keyC, keyB
	}
	expected := []string{keyA, first, second}

	byDest, _, order := groupReplacementEntries(reps)
	require.Equal(t, expected, order)
	_, _, reversedOrder := groupReplacementEntries(reversed)
	require.Equal(t, expected, reversedOrder, "journal entry order cannot perturb the group walk")
	require.Equal(t, []int64{7, 2}, []int64{byDest[keyA][0].DestSeq, byDest[keyA][1].DestSeq},
		"in-group restore order stays DestSeq descending")
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
