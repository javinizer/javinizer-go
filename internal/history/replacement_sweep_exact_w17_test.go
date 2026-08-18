package history

// POSTER-WRITE-HARDENING codex PR#215 wave-17 (P2) — "require an exact
// destination match in targeted sweeps": SweepDestinations's candidate
// acceptance used to be a PREFIX test, so a sibling-decoy backup whose
// destination merely EXTENDS a requested destination's name
// ('cover.jpg.old.dlbak.<hex>' for a sweep of 'cover.jpg') was arbitrated —
// and, being unjournaled with a missing derived destination, RESTORED onto a
// path the caller never named. The candidate destination is now derived by
// stripping the validated '.dlbak.<16hex>' marker and compared for EXACT
// equality under the same probe-aware sweepSlash/DestKey key the journal
// comparisons use.

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The decoy shape from the finding: a targeted sweep of 'cover.jpg' must
// arbitrate 'cover.jpg.dlbak.<hex>' and NOTHING ELSE in the folder — the
// 'cover.jpg.old.dlbak.<hex>' sibling decoy is not a backup OF cover.jpg.
func TestSweepDestinationsW17_SiblingNameDecoyIsNotArbitrated(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dir := "/out/W17-EXACT"
	dest := dir + "/cover.jpg"
	backup := dest + ".dlbak." + p3HexA
	decoyDest := dir + "/cover.jpg.old"
	decoyBackup := decoyDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	// Crash-window shape: journaled backup armed, destination missing.
	writeSweepFile(t, fs, backup, "old-cover", time.Hour)
	writeSweepFile(t, fs, decoyBackup, "decoy-bytes", time.Hour)
	op := journalRow(t, repo, "job-w17", "W17-EXACT", dest, backup, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(context.Background(), []string{dest})
	require.NoError(t, err)
	require.Equal(t, 1, healed,
		"only the EXACT destination's crash-window backup is healed (a prefix match would also restore the decoy)")

	require.Equal(t, "old-cover", string(mustRead2(t, fs, dest)),
		"the exact backup still restores its destination")
	decoyBytes, readErr := afero.ReadFile(fs, decoyBackup)
	require.NoError(t, readErr, "the sibling decoy backup is never touched")
	require.Equal(t, "decoy-bytes", string(decoyBytes))
	exists, err := afero.Exists(fs, decoyDest)
	require.NoError(t, err)
	require.False(t, exists,
		"the decoy is never RESTORED onto a destination the caller never named")
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID),
		"the exact entry is consumed; the untargeted decoy keeps no journal state")
}

// Case/spelling normalization stays DestKey-consistent: on an insensitive
// root a DIFFERENT-CASE spelling of the requested destination still matches
// the exact backup destination (folded equality), while byte-different
// siblings stay refused.
func TestSweepDestinationsW17_CaseFoldedRequestedDestStillMatchesExactly(t *testing.T) {
	previousCaseProbe := fsutil.CaseSensitiveProbe
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return false, nil } // insensitive root
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = previousCaseProbe
		fsutil.ResetCaseSensitivityCache()
	})

	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dir := "/out/W17-CASE"
	dest := dir + "/cover.jpg"
	backup := dest + ".dlbak." + p3HexC
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "old-cover", time.Hour)
	op := journalRow(t, repo, "job-w17", "W17-CASE", dest, backup, 1, models.RevertStatusApplied)

	// Same folder spelling (ReadDir stays on-disk), differently-cased file.
	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(context.Background(),
		[]string{dir + "/Cover.JPG"})
	require.NoError(t, err)
	require.Equal(t, 1, healed,
		"folded spellings of one insensitive destination key still match exactly")
	require.Equal(t, "old-cover", string(mustRead2(t, fs, dest)),
		"the restore lands on the journaled/recorded destination spelling")
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}
