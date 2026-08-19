package history

// POSTER-WRITE-HARDENING codex PR#215 wave-29 (P2) — "retry absent
// rearm-refused entries during startup sweeps": a restore-pending entry of
// the REARM-REFUSED kind points at an ABSENT backup name (a refused no-replace
// re-arm), so it never materializes as a `*.dlbak.<16hex>` marker file in any
// directory scan — the pre-wave-29 sweeps never retried it, its armed journal
// row stayed live indefinitely, and older replacement chains stayed blocked
// by checkDestBlocking. FULL sweeps now enumerate these entries straight from
// the ledger index and consume them JOURNAL-ONLY through retryPendingRemoval's
// wave-19 kind routing: the marker's certification of the destination bytes
// is re-verified (destination present by Lstat), no path operation ever
// touches the unowned backup name, and a consumption failure keeps the entry
// live with a warn. CLEAN-kind pendings keep their marker-file semantics, and
// scoped/targeted sweeps (SweepDirs/SweepDestinations) are untouched.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w29RearmRefusedRow seeds one applied operation row whose single replacement
// entry is restore-pending with the rearm-refused kind; the backup file itself
// is deliberately NOT created (the refused re-arm left the name absent).
func w29RearmRefusedRow(t *testing.T, repo *p3OpRepo, movieID, dest string) (*models.BatchFileOperation, string) {
	t.Helper()
	backup := dest + ".dlbak." + p3HexA
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1,
			RestorePending: true, RestorePendingKind: models.RestorePendingKindRearmRefused,
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op, backup
}

func w29RowEntry(t *testing.T, repo *p3OpRepo, opID uint) []models.ReplacementEntry {
	t.Helper()
	row, err := repo.FindByID(context.Background(), opID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return gf.Replacements
}

// The core fix: a ledger-seeded rearm-refused pending entry with an ABSENT
// backup is consumed journal-only by the full startup sweep — no fs operation
// needed (or performed) at the backup path, the entry is gone, and the
// certified destination is untouched.
func TestSweepW29_RearmRefusedAbsentBackupConsumedJournalOnly(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W29-SWEEP/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W29-SWEEP", 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	op, backup := w29RearmRefusedRow(t, repo, "W29-SWEEP", dest)

	// Journal-only probe: any removal against the unowned backup name is
	// counted; there must be none.
	counting := &w29BackupTouchFs{Fs: fs, victim: backup}
	s := NewReplacementSweeper(counting, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed, "the ledger-enumerated rearm-refused pending is consumed")
	require.Zero(t, counting.removes, "journal-only consumption runs NO path operation against the unowned backup name")

	require.Equal(t, "restored", string(mustRead2(t, fs, dest)), "the certified destination is untouched")
	_, serr := fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "no fs operation materialized the absent backup name")
	require.Empty(t, w29RowEntry(t, repo, op.ID), "the journal entry is consumed")
}

// The consumption contract is the wave-19 one: the marker certified the
// destination bytes, so a MISSING (or otherwise unclassifiable) destination
// keeps the entry live with a warn instead of erasing the journal's only
// record that a restore happened.
func TestSweepW29_RearmRefusedDestMissingRetainedAndWarned(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W29-NODEST/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W29-NODEST", 0o755))
	op, backup := w29RearmRefusedRow(t, repo, "W29-NODEST", dest)

	var logs bytes.Buffer
	restore := logging.SetOutput(&logs)
	defer restore()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "a missing certified destination consumes nothing")

	entries := w29RowEntry(t, repo, op.ID)
	require.Len(t, entries, 1, "the entry stays live")
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind())
	require.Contains(t, logs.String(), "rearm-refused pending kept", "the retention surfaces a warn")
	require.Contains(t, logs.String(), filepath29Base(backup), "the warn names the pending backup")
}

// Consumption failure keeps the entry live (retryPendingRemoval's
// durable-marker re-persist legs fail here too — log-only), the destination
// stays certified-in-place, and nothing touches the backup name.
func TestSweepW29_RearmRefusedConsumptionFailureKeepsEntryLive(t *testing.T) {
	fs := afero.NewMemMapFs()
	baseRepo := newP3OpRepo()
	repo := &failingUpdateRepo{p3OpRepo: baseRepo, updateErr: errors.New("w29 journal wedged")}
	ctx := context.Background()
	dest := "/out/W29-CONSUMEFAIL/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W29-CONSUMEFAIL", 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	op, backup := w29RearmRefusedRow(t, baseRepo, "W29-CONSUMEFAIL", dest)

	var logs bytes.Buffer
	restore := logging.SetOutput(&logs)
	defer restore()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)

	entries := w29RowEntry(t, baseRepo, op.ID)
	require.Len(t, entries, 1, "the entry stays live for a later retry")
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind())
	require.Contains(t, logs.String(), "kept", "the failure path warns")
	require.Equal(t, "restored", string(mustRead2(t, fs, dest)))
	_, serr := fs.Stat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist)

	// Heal the repo and retry: the entry consumes journal-only now.
	repo.updateErr = nil
	healed, err = NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Empty(t, w29RowEntry(t, baseRepo, op.ID))
}

// Danger pin (wave-29): a CLEAN-kind pending with an absent backup must NOT
// be picked up by the ledger leg — only the marker file (still present for
// the clean kind) may authorize its removal path, so the entry is untouched.
func TestSweepW29_CleanKindAbsentBackupUntouchedByLedgerLeg(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W29-CLEANABSENT/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W29-CLEANABSENT", 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	backup := dest + ".dlbak." + p3HexA
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "W29-CLEANABSENT", OriginalPath: "/src/w29-cleanabsent.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1,
			RestorePending: true, // legacy CLEAN kind (no RestorePendingKind field)
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "clean-kind pendings keep their marker-file semantics — the ledger leg never arms a removal")

	entries := w29RowEntry(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindClean, entries[0].PendingKind())
}

// Scoped and targeted sweeps keep their marker-file semantics: the ledger leg
// belongs to the FULL startup sweep only.
func TestSweepW29_ScopedSweepsLeaveTheLedgerLegUntouched(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W29-SCOPED/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W29-SCOPED", 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	op, _ := w29RearmRefusedRow(t, repo, "W29-SCOPED", dest)
	s := NewReplacementSweeper(fs, repo)

	healed, err := s.SweepDirs(ctx, []string{"/out/W29-SCOPED"})
	require.NoError(t, err)
	require.Equal(t, 0, healed, "SweepDirs never runs the ledger-only leg")
	require.Len(t, w29RowEntry(t, repo, op.ID), 1)

	healed, err = s.SweepDestinations(ctx, []string{dest})
	require.NoError(t, err)
	require.Equal(t, 0, healed, "SweepDestinations never runs the ledger-only leg")
	require.Len(t, w29RowEntry(t, repo, op.ID), 1)

	// ...while the full sweep consumes it journal-only.
	healed, err = s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Empty(t, w29RowEntry(t, repo, op.ID))
}

// An OCCUPIED backup name is never this leg's call: the directory scan sees
// the marker-file occupant and arbitrates it through the wave-19 journaled
// leg (journal-only consumption, occupant byte-intact). The ledger-first
// ordering must defer to exactly that arbitration.
func TestSweepW29_RearmRefusedOccupiedNameArbitratedByTheScan(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W29-OCCUPANT/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W29-OCCUPANT", 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	op, backup := w29RearmRefusedRow(t, repo, "W29-OCCUPANT", dest)
	writeSweepFile(t, fs, backup, "foreign-occupant", 0)

	s := NewReplacementSweeper(fs, repo)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed, "the scan's wave-19 leg consumes the entry journal-only")
	require.Equal(t, "foreign-occupant", string(mustRead2(t, fs, backup)),
		"the foreign occupant at the backup name is NEVER removed")
	require.Equal(t, "restored", string(mustRead2(t, fs, dest)))
	require.Empty(t, w29RowEntry(t, repo, op.ID), "the journal entry is consumed")
}

// w29BackupTouchFs counts Remove calls against the (unowned) backup name:
// journal-only legs must perform zero.
type w29BackupTouchFs struct {
	afero.Fs
	victim  string
	removes int
}

func (f *w29BackupTouchFs) Remove(name string) error {
	if strings.Contains(name, ".dlbak.") {
		f.removes++
	}
	return f.Fs.Remove(name)
}

// failingUpdateRepo (from replacement_sweep_p3 framework tests) is reused;
// keep database imported for the interface assertion style used here.
var _ database.BatchFileOperationRepositoryInterface = (*failingUpdateRepo)(nil)

// filepath29Base renders the backup's basename for log assertions without
// importing path/filepath (journal spellings are slash-formed).
func filepath29Base(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
