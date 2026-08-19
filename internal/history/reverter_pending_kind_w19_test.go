package history

// POSTER-WRITE-HARDENING codex PR#215 wave-19 (P2) — "route the pending
// retry on its kind": a RestorePending journal entry now carries a kind.
// The legacy CLEAN kind (default) retries backup removal + consumption. The
// new REARM-REFUSED kind (set only where a refused no-replace re-arm left
// the backup name foreign-occupied or absent) certifies the restored
// destination and consumes the journal entry WITHOUT any backup-path
// operation: pre-wave-19 the retry statted the path first (absent name →
// every explicit revert failed forever) and then unconditionally REMOVED it
// (foreign occupant deleted). These tests pin the wave-19 state machine for
// the explicit-revert legs; the sweep mirror lives in
// replacement_sweep_pending_kind_w19_test.go.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w19PendingReplacement builds one applied operation whose single journaled
// replacement entry is restore-pending with the given kind, plus the on-disk
// state the retry encounters (nil destBytes/backupBytes = path absent).
func w19PendingReplacement(t *testing.T, fixture *p3Fixture, movieID, pendingKind string, destBytes, backupBytes []byte) (*models.BatchFileOperation, string, string) {
	t.Helper()
	dir := "/dst/" + movieID
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fixture.fs.MkdirAll(dir, config.DirPerm))
	require.NoError(t, afero.WriteFile(fixture.fs, dir+"/"+movieID+".mkv", []byte("video-"+movieID), config.FilePerm))
	if destBytes != nil {
		require.NoError(t, afero.WriteFile(fixture.fs, dest, destBytes, config.FilePerm))
	}
	if backupBytes != nil {
		require.NoError(t, afero.WriteFile(fixture.fs, backup, backupBytes, config.FilePerm))
	}

	entry := models.ReplacementEntry{Destination: dest, Backup: backup, DestSeq: 1}
	require.True(t, entry.SetRestorePending(pendingKind))
	raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{entry}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-w19k", MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv",
		NewPath:       dir + "/" + movieID + ".mkv",
		OperationType: models.OperationTypeMove, GeneratedFiles: raw,
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, fixture.repo.Create(context.Background(), op))
	return op, dest, backup
}

func w19Journal(t *testing.T, repo *p3OpRepo, id uint) models.GeneratedFilesJSON {
	t.Helper()
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return gf
}

// Collision-set pending (foreign owner at the backup name): the retry
// consumes the entry while the foreign file and the certified destination
// both stay byte-exact — no path operation runs against the unowned name.
func TestReverterPendingKindW19_RearmRefusedOccupantConsumedWithoutPathOps(t *testing.T) {
	fixture := newP3Fixture()
	op, dest, backup := w19PendingReplacement(t, fixture, "W19K-OCC", models.RestorePendingKindRearmRefused,
		[]byte("old"), []byte("foreign-bytes"))

	restored, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.True(t, restored[dest], "the marker-certified destination stays in the delete-subtraction set")
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the certified destination is never re-restored from the occupant")
	require.Equal(t, "foreign-bytes", p3ReadFile(t, fixture.fs, backup), "the foreign occupant is never removed")
	require.Empty(t, w19Journal(t, fixture.repo, op.ID).Replacements, "the journal entry consumed")
}

// Absent path + collision-kind pending (a no-replace-unsupported volume
// published nothing): consumption succeeds — the pre-wave-19 source stat
// made every explicit revert fail forever in exactly this state.
func TestReverterPendingKindW19_RearmRefusedAbsentBackupConsumed(t *testing.T) {
	fixture := newP3Fixture()
	op, dest, backup := w19PendingReplacement(t, fixture, "W19K-ABS", models.RestorePendingKindRearmRefused,
		[]byte("old"), nil)

	_, statErr := fixture.fs.Stat(backup)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the backup name is absent — pre-wave-19 this wedged the retry")

	restored, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err, "no path existence check gates a rearm-refused consumption")
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
	require.Empty(t, w19Journal(t, fixture.repo, op.ID).Replacements, "the deferred consumption completes")
}

// Legacy clean-pending (no kind field — the wave-18 payload): the retry
// still removes the operation's OWN backup and consumes — removal behavior
// unchanged.
func TestReverterPendingKindW19_LegacyCleanPendingRemovesOwnedBackup(t *testing.T) {
	fixture := newP3Fixture()
	op, dest, backup := w19PendingReplacement(t, fixture, "W19K-LEGACY", models.RestorePendingKindClean,
		[]byte("old"), []byte("old"))

	gf := w19Journal(t, fixture.repo, op.ID)
	require.Equal(t, "", gf.Replacements[0].RestorePendingKind, "clean kind is never materialized (legacy blob parity)")
	require.Equal(t, models.RestorePendingKindClean, gf.Replacements[0].PendingKind())

	restored, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
	_, statErr := fixture.fs.Stat(backup)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the clean-kind retry still removes the owned backup")
	require.Empty(t, w19Journal(t, fixture.repo, op.ID).Replacements)
}

// The certified destination is the only remaining copy of the restored
// bytes for a rearm-refused entry (the backup name holds none that we own):
// a missing destination refuses the consumption — the entry and the occupant
// are retained untouched.
func TestReverterPendingKindW19_RearmRefusedDestMissingRefusesConsumption(t *testing.T) {
	fixture := newP3Fixture()
	op, _, backup := w19PendingReplacement(t, fixture, "W19K-NODEST", models.RestorePendingKindRearmRefused,
		nil, []byte("foreign-bytes"))

	_, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.Error(t, err, "a missing certified destination must refuse consumption, not erase the record")
	require.Contains(t, err.Error(), "rearm-refused pending entry")
	require.Contains(t, err.Error(), "destination unreadable")

	gf := w19Journal(t, fixture.repo, op.ID)
	require.Len(t, gf.Replacements, 1, "the entry is retained")
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind())
	require.Equal(t, "foreign-bytes", p3ReadFile(t, fixture.fs, backup), "the occupant stays untouched")
}

// Consumption failure on a rearm-refused entry needs no compensation: nothing
// was removed, so no re-arm is attempted (no staging residue), the durable
// marker + kind survive, and the failure surfaces unchanged.
func TestReverterPendingKindW19_RearmRefusedConsumptionFailureKeepsMarker(t *testing.T) {
	fixture := newP3Fixture()
	op, dest, backup := w19PendingReplacement(t, fixture, "W19K-CFAIL", models.RestorePendingKindRearmRefused,
		[]byte("old"), []byte("foreign-bytes"))

	consumeErr := errors.New("w19k consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	_, err := NewReverter(fixture.fs, repo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, consumeErr)
	require.Contains(t, logs.String(), "consumption of a rearm-refused pending entry failed")
	require.Equal(t, 1, repo.calls, "exactly the consumption transaction ran — no re-arm, no marker rewrite")

	gf := w19Journal(t, fixture.repo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].PendingKind(),
		"the durable marker + kind survive the failed consumption")
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
	require.Equal(t, "foreign-bytes", p3ReadFile(t, fixture.fs, backup))
	for _, name := range w15DirListing(t, fixture.fs, filepath.Dir(dest)) {
		require.NotContains(t, name, rearmStagingSuffix+".", "no staged re-arm copy: consumption failure needs no compensation")
	}
}

// The marker-SETTING site: consumption failure + collided re-arm persists
// the rearm-refused KIND, not just the bare RestorePending bit — the payload
// shape the downloader's finding-2 mark and the sweep compensation share.
func TestReverterPendingKindW19_RefusedRearmMarksRearmRefusedPayload(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w19k", "W19K-MARK", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	consumeErr := errors.New("w19k consumption transaction wedged")
	repo := &w18TxFailRepo{p3OpRepo: fixture.repo, fail: map[int]error{1: consumeErr}}
	fs := &w15BackupRaceFs{Fs: fixture.fs, target: backup, foreign: []byte("foreign-bytes")}

	_, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, consumeErr)
	require.True(t, fs.fired, "the foreign claim raced the re-arm publish")

	gf := w19Journal(t, fixture.repo, op.ID)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, gf.Replacements[0].RestorePendingKind,
		"the refused re-arm persists the rearm-refused kind, not the bare wave-18 bit")

	row, ferr := fixture.repo.FindByID(context.Background(), op.ID)
	require.NoError(t, ferr)
	require.Contains(t, row.GeneratedFiles, `"restore_pending":true,"restore_pending_kind":"rearm_refused"`,
		"payload shape: kind field exactly after restore_pending")

	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
	require.Equal(t, "foreign-bytes", p3ReadFile(t, fixture.fs, backup), "the occupant is untouched")
}
