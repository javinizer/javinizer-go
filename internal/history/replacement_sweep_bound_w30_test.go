package history

// POSTER-WRITE-HARDENING wave-30 — sweep coverage pins for the ledger
// rearm-refused consumption legs left over from wave-22/29: busy-marker
// arbitration (owned name, arbitration failure), the restore-certified
// destination's presence re-verification, and cancellation winning mid-loop.

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w30RefusedRow seeds one more rearm-refused restore-pending row (same shape
// as w29RearmRefusedRow) at a caller-chosen dest.
func w30RefusedRow(t *testing.T, repo *p3OpRepo, movieID, dest, hexTail string) {
	t.Helper()
	backup := dest + ".dlbak." + hexTail
	op := &models.BatchFileOperation{
		BatchJobID: "job-w30", MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: dest, Backup: backup, DestSeq: 1,
			RestorePending: true, RestorePendingKind: models.RestorePendingKindRearmRefused,
		}}}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
}

func w30PendingCount(t *testing.T, repo *p3OpRepo) int {
	t.Helper()
	pending := 0
	for _, row := range repo.ops {
		gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
		require.NoError(t, err)
		for _, rep := range gf.Replacements {
			if rep.RestorePending {
				pending++
			}
		}
	}
	return pending
}

// An owned busy marker keeps the rearm-refused pending untouched: the sweep
// defers the whole entry to the owning process's lifecycle.
func TestSweepW30_RearmRefusedBusyOwnedNameKept(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W30-BUSY/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W30-BUSY", 0o755))
	writeSweepFile(t, fs, dest, "restored", 0)
	w30RefusedRow(t, repo, "W30-BUSY", dest, p3HexA)

	release, err := fsutil.AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	t.Cleanup(release)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Zero(t, healed, "the busy owner wins arbitration — nothing consumed")
	require.Equal(t, 1, w30PendingCount(t, repo), "the entry stays live")
}

// An arbitration FAILURE (busy name uncreateable/uninspectable) keeps the
// entry live too — fail-closed like every other indeterminate name.
func TestSweepW30_RearmRefusedBusyArbitrationFailureKept(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W30-BUSYDENY/poster.jpg"
	require.NoError(t, base.MkdirAll("/out/W30-BUSYDENY", 0o755))
	writeSweepFile(t, base, dest, "restored", 0)
	w30RefusedRow(t, repo, "W30-BUSYDENY", dest, p3HexB)

	denied := &w30OpenDenyFs{Fs: base, denyPath: fsutil.ReplacementBusyPath(dest)}
	healed, err := NewReplacementSweeper(denied, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Zero(t, healed)
	require.Equal(t, 1, w30PendingCount(t, repo))
}

// w30OpenDenyFs fails OpenFile on exactly one path — the busy marker — so
// AcquireReplacementBusy never classifies the name.
type w30OpenDenyFs struct {
	afero.Fs
	denyPath string
}

func (w *w30OpenDenyFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if name == w.denyPath {
		return nil, os.ErrPermission
	}
	return w.Fs.OpenFile(name, flag, perm)
}

// The restore-certified destination must still be PRESENT before the only
// journal record is consumed: an absent destination keeps the entry.
func TestSweepW30_RearmRefusedDestAbsentKept(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W30-DESTGONE/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W30-DESTGONE", 0o755))
	w30RefusedRow(t, repo, "W30-DESTGONE", dest, p3HexA)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Zero(t, healed, "dest absence keeps the entry — consumption needs the certified destination")
	require.Equal(t, 1, w30PendingCount(t, repo))
}

// w30StatDenyFs fails name lookups on one path with a NON-ENOENT error — the
// "backup name state indeterminate" leg's driver.
type w30StatDenyFs struct {
	afero.Fs
	denyPath string
}

func (w *w30StatDenyFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == w.denyPath {
		return nil, false, os.ErrPermission
	}
	if ls, ok := w.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := w.Fs.Stat(name)
	return info, false, err
}

func (w *w30StatDenyFs) Stat(name string) (os.FileInfo, error) {
	if name == w.denyPath {
		return nil, os.ErrPermission
	}
	return w.Fs.Stat(name)
}

// An INDETERMINATE backup-name answer (lookup denied, not a clean ENOENT)
// keeps the entry too — a removal decision against an unproven name could
// destroy foreign bytes.
func TestSweepW30_RearmRefusedBackupStateIndeterminateKept(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W30-BAKIND/poster.jpg"
	require.NoError(t, base.MkdirAll("/out/W30-BAKIND", 0o755))
	writeSweepFile(t, base, dest, "restored", 0)
	w30RefusedRow(t, repo, "W30-BAKIND", dest, p3HexA)

	denied := &w30StatDenyFs{Fs: base, denyPath: dest + ".dlbak." + p3HexA}
	healed, err := NewReplacementSweeper(denied, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Zero(t, healed, "an indeterminate backup name keeps the entry live")
	require.Equal(t, 1, w30PendingCount(t, repo))
}

// w30CancelRepo cancels the sweep context as the FIRST rearm-refused
// consumption commits, so the loop's second entry observes cancellation.
type w30CancelRepo struct {
	*p3OpRepo
	once   sync.Once
	cancel context.CancelFunc
}

func (r *w30CancelRepo) UpdateJournalInTx(ctx context.Context, recordID uint, fn database.JournalUpdateFn) error {
	err := r.p3OpRepo.UpdateJournalInTx(ctx, recordID, fn)
	r.once.Do(r.cancel)
	return err
}

// Cancellation wins over ledger progress mid-loop: the second rearm-refused
// entry is left for the next sweep.
func TestSweepW30_RearmRefusedCancellationWinsMidLoop(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	destA := "/out/W30-CXA/poster.jpg"
	destB := "/out/W30-CXB/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/W30-CXA", 0o755))
	require.NoError(t, fs.MkdirAll("/out/W30-CXB", 0o755))
	writeSweepFile(t, fs, destA, "restored-a", 0)
	writeSweepFile(t, fs, destB, "restored-b", 0)
	w30RefusedRow(t, repo, "W30-CXA", destA, p3HexA)
	w30RefusedRow(t, repo, "W30-CXB", destB, p3HexB)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	wrapped := &w30CancelRepo{p3OpRepo: repo, cancel: cancel}

	healed, err := NewReplacementSweeper(fs, wrapped).Sweep(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, healed, "one entry consumed before cancellation hit the loop head")
	require.Equal(t, 1, w30PendingCount(t, repo), "the second entry stays live for the next sweep")
}

// database.BatchFileOperationRepositoryInterface conformance anchor — the
// wrapper must keep satisfying the sweeper's repo contract.
var _ database.BatchFileOperationRepositoryInterface = (*w30CancelRepo)(nil)
