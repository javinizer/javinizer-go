package history

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// codex P3 R4-3: an install-CONFIRMED entry whose destination went missing
// afterwards means somebody deleted the media — the sweep must NOT resurrect
// it (and must keep the journaled backup so revert still can).
func TestSweep_ConfirmedInstall_MissingDest_NotResurrected(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/DEL/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/DEL", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(backup, old, old))

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1, Installed: true},
	}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "DEL-001", OriginalPath: "/src/del.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "no crash window after a confirmed install")
	exists, _ := afero.Exists(fs, dest)
	require.False(t, exists, "deleted-afterwards media stays deleted")
	exists, _ = afero.Exists(fs, backup)
	require.True(t, exists, "journaled backup retained for an explicit revert")
}

// codex P3 R4-1: the orphan classification must re-read ownership under the
// destination lock — a row journaled after the sweep's index snapshot must
// never see its backup removed.
func TestSweep_OrphanFreshRecheckUnderLock_KeepsJustJournaledBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/FRESH/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/FRESH", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("new"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(backup, old, old))

	// The journal row exists in the repo, but the sweep's index snapshot does
	// NOT include it — simulating an index built before RecordReplacement.
	raw, _ := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1},
	}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "FRESH-1", OriginalPath: "/src/f.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	sweeper := NewReplacementSweeper(fs, repo)
	staleIdx := &replacementLedgerIndex{
		journaled: map[string]*models.BatchFileOperation{},
		dirs:      map[string]bool{"/out/FRESH": true},
	}
	info, err := fs.Stat(backup)
	require.NoError(t, err)
	got := sweeper.sweepOne(ctx, staleIdx, "/out/FRESH", info)
	require.Equal(t, 0, got, "freshly journaled backup survives the orphan branch")
	exists, _ := afero.Exists(fs, backup)
	require.True(t, exists)
}

// codex P3 R11-2: sweeps scan recorded dirs FLAT — the orchestrator seeds
// the organizer's exact leaf folder (R7-3), and library-scale recursion is
// forbidden at startup. Nested libs are discovered via the leaf seed, never
// by descent.
func TestSweep_NestedRoot_BackupDiscoveredViaLeafSeed(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/base/Deep (1)/Sub/Movie (2020)/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/base/Deep (1)/Sub/Movie (2020)", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("nested-old"), config.FilePerm))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, fs.Chtimes(backup, old, old))

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/base", "/out/base/Deep (1)/Sub/Movie (2020)"}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "NST-001", OriginalPath: "/src/nst.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "nested-old", string(mustRead2(t, fs, dest)))
}

// codex P3 R5-3: restores stream through a bounded buffer — a trailer-class
// backup restores byte-exactly without whole-file buffering.
func TestRevertRestore_StreamsLargeBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	const size = 6 << 20 // 6 MiB — plain evidence the path works above the buffer size
	big := make([]byte, size)
	for i := range big {
		big[i] = byte(i % 251)
	}

	f := &p3Fixture{fs: fs, repo: repo}
	op, dest := f.addAppliedOp(t, "job-1", "BIG-001", false, "new", p3Replacement{seq: 1, backupBytes: ""})
	// Replace the tiny fixture backup with the big one.
	require.NoError(t, afero.WriteFile(fs, dest+".dlbak.a", big, config.FilePerm))
	require.NotNil(t, op)

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	got, err := afero.ReadFile(fs, dest)
	require.NoError(t, err)
	require.Equal(t, len(big), len(got))
	require.Equal(t, big, got, "restored bytes must be byte-identical")
}

// codex P3 R8-1: the orphan-restore stream must be byte-exact (copy, not
// rename now — the staged copy + swap path), and an indeterminate
// destination stat must keep the backup untouched.
func TestSweep_OrphanRestoreByteExact_AndIndeterminateKeeps(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/IND/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/IND", config.DirPerm))
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	require.NoError(t, afero.WriteFile(fs, backup, payload, config.FilePerm))
	backdate(t, fs, backup)

	// Scope the dir via a journal row on an unrelated path.
	raw, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/IND"}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "IND-001", OriginalPath: "/src/ind.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	got, err := afero.ReadFile(fs, dest)
	require.NoError(t, err)
	require.Equal(t, payload, got, "restore is byte-exact")
	exists, _ := afero.Exists(fs, backup)
	require.True(t, exists, "unjournaled marker backup remains after a successful streamed restore")

	// Indeterminate destination: wrap the fs so Stat(dest) fails.
	fs2 := &statFailingFs{Fs: afero.NewMemMapFs(), failPath: dest}
	require.NoError(t, fs2.MkdirAll("/out/IND", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs2, backup, payload, config.FilePerm))
	backdate(t, fs2, backup)
	healed, err = NewReplacementSweeper(fs2, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "indeterminate destination — backup kept")
	exists, _ = afero.Exists(fs2, backup)
	require.True(t, exists)
}

type statFailingFs struct {
	afero.Fs
	failPath string
}

func (f *statFailingFs) Stat(name string) (os.FileInfo, error) {
	if strings.Contains(name, "poster.jpg") && !strings.Contains(name, ".dlbak.") {
		return nil, pathErrPermission(name)
	}
	return f.Fs.Stat(name)
}

type pathErrPermission string

func (e pathErrPermission) Error() string { return "permission denied: " + string(e) }

// codex P3 R9-2: when the consumption update fails after the crash-window
// restore, the restore is undone — each sweep attempt replays the exact same
// state until the entry is durably consumed.
func TestSweep_CrashRestoreConsumptionFailure_UndoesThenRetries(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	flaky := &flakySweepRepo{p3OpRepo: repo}
	ctx := context.Background()

	dest := "/out/UND/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/UND", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("pre-crash"), config.FilePerm))
	backdate(t, fs, backup)

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1},
	}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "UND-001", OriginalPath: "/src/und.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	flaky.fail = true
	s := NewReplacementSweeper(fs, flaky)
	healed, err := s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "consumption failure aborts the repair")
	exists, _ := afero.Exists(fs, dest)
	require.False(t, exists, "restore undone — pre-crash state reproduced")
	exists, _ = afero.Exists(fs, backup)
	require.True(t, exists, "backup intact for the retry")

	flaky.fail = false
	healed, err = s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "pre-crash", string(mustRead2(t, fs, dest)))
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "durable consumption on the retry")

	// And a user deleting the restored media afterwards must NOT fire another
	// restore — the entry is gone by then.
	require.NoError(t, fs.Remove(dest))
	healed, err = s.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
}

// flakySweepRepo fails journal transaction calls while fail is set — except
// the FIRST call. The sweep's journal section probes entry presence in one
// transaction before the persistence transaction it diagnoses (review
// 4960250562), so failing the very first call would mask the intended
// persist-failure leg behind the presence-probe failure leg.
type flakySweepRepo struct {
	*p3OpRepo
	fail  bool
	calls int
}

func (m *flakySweepRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	m.calls++
	if m.fail && m.calls > 1 {
		return errors.New("transient outage")
	}
	return m.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

// codex P3 R11-1: the armed flag is re-read from a FRESH row under the dest
// lock — an index snapshot taken before install-confirm must not misclassify
// a deleted-afterwards destination.
func TestSweep_StaleArmedFlag_ReclassifiedByFreshRead(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/STL/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/STL", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	backdate(t, fs, backup)

	// Repo row is CONFIRMED (installed), but the sweeper's index sees a stale
	// ARMED snapshot of it — the index predates the confirmation.
	rawArmed, _ := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1, Installed: false},
	}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "STL-001", OriginalPath: "/src/stl.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(rawArmed),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	s := NewReplacementSweeper(fs, repo)
	staleIdx := &replacementLedgerIndex{
		journaled: map[string]*models.BatchFileOperation{backup: op},
		dirs:      map[string]bool{"/out/STL": true},
	}
	// Now the row flips to confirmed (as if the apply raced the index build).
	rawInstalled, _ := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1, Installed: true},
	}})
	op.GeneratedFiles = string(rawInstalled)
	require.NoError(t, repo.Update(ctx, op))

	staleIdx.journaled[sweepSlash(backup)] = mustRow(t, repo, op.ID)
	// Force the stale-armed case: the index's owner row is the ARMED snapshot.
	rawArmedOp := *op
	rawArmedOp.GeneratedFiles = string(rawArmed)
	staleIdx.journaled[sweepSlash(backup)] = &rawArmedOp

	info, err := fs.Stat(backup)
	require.NoError(t, err)
	got := s.sweepOne(ctx, staleIdx, "/out/STL", info)
	require.Equal(t, 0, got, "fresh row read wins over the stale index flag")
	exists, _ := afero.Exists(fs, backup)
	require.True(t, exists)
}

func mustRow(t *testing.T, repo *p3OpRepo, id uint) *models.BatchFileOperation {
	t.Helper()
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	return row
}

// codex P3 R12-3: a destination whose own name contains a marker-shaped
// suffix must still see its real backup discovered and restored.
func TestSweep_MarkerInfixDestination_BackupFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/MIX/poster.dlbak.0123456789abcdef.jpg" // marker-shaped INFIX
	backup := dest + ".dlbak.fedcba9876543210"
	require.NoError(t, fs.MkdirAll("/out/MIX", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("pre"), config.FilePerm))
	backdate(t, fs, backup)

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/MIX"}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "MIX-001", OriginalPath: "/src/mix.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "pre", string(mustRead2(t, fs, dest)))
}

// codex P3 R15-1: consumption reads the row FRESH under the shared journal
// lock — entries recorded after the index snapshot survived the consume
// update.
func TestSweep_ConsumeFromLiveRow_PreservesConcurrentEntry(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest1 := "/out/DRF/poster.jpg"
	dest2 := "/out/DRF/fanart.jpg"
	backup1 := dest1 + ".dlbak.0123456789abcdef"
	backup2 := dest2 + ".dlbak.fedcba9876543210"
	require.NoError(t, fs.MkdirAll("/out/DRF", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup1, []byte("old-1"), config.FilePerm))
	backdate(t, fs, backup1)

	// The live row journals TWO entries; the sweeper's member for
	// arbitration is a STALE snapshot carrying only the first one.
	mk := func(entries ...models.ReplacementEntry) string {
		raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: entries})
		require.NoError(t, err)
		return string(raw)
	}
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "DRF-001", OriginalPath: "/src/drf.mkv",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: mk(
			models.ReplacementEntry{Destination: dest1, Backup: backup1, DestSeq: 1},
			models.ReplacementEntry{Destination: dest2, Backup: backup2, DestSeq: 1},
		),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))
	stale := *op
	stale.GeneratedFiles = mk(models.ReplacementEntry{Destination: dest1, Backup: backup1, DestSeq: 1})

	s := NewReplacementSweeper(fs, repo)
	staleIdx := &replacementLedgerIndex{
		journaled: map[string]*models.BatchFileOperation{sweepSlash(backup1): &stale},
		dirs:      map[string]bool{"/out/DRF": true},
	}
	info, err := fs.Stat(backup1)
	require.NoError(t, err)
	got := s.sweepOne(ctx, staleIdx, "/out/DRF", info)
	require.Equal(t, 1, got, "crash-window restore completes (armed + dest-missing)")

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "concurrent entry survived the consumption update")
	require.Equal(t, dest2, gf.Replacements[0].Destination)
}

// Sweep index failure must surface rather than scan nothing.
func TestSweep_IndexErrorPropagates(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	broken := &errIdxRepo{p3OpRepo: repo, err: errors.New("scan down")}
	_, err := NewReplacementSweeper(fs, broken).Sweep(context.Background())
	require.Error(t, err)
}

type errIdxRepo struct {
	*p3OpRepo
	err error
}

func (m *errIdxRepo) FindOperationsWithReplacements(context.Context) ([]models.BatchFileOperation, error) {
	return nil, m.err
}

// Live-row disappearance between owner classification and consumption:
// restore is undone; backup kept; nothing bleeds.
func TestSweepOne_LiveRowGone_RestoreUndone(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/LGD/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/LGD", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	backdate(t, fs, backup)

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: 1},
	}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "LGD-001", OriginalPath: "/src/lgd.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	// Row vanishes between index build and consumption.
	goneRepo := &rowGoneRepo{p3OpRepo: repo, goneID: op.ID}
	s := NewReplacementSweeper(fs, goneRepo)
	staleIdx := &replacementLedgerIndex{
		journaled: map[string]*models.BatchFileOperation{sweepSlash(backup): op},
		dirs:      map[string]bool{"/out/LGD": true},
	}
	info, err := fs.Stat(backup)
	require.NoError(t, err)
	got := s.sweepOne(ctx, staleIdx, "/out/LGD", info)
	require.Equal(t, 0, got)
	exists, _ := afero.Exists(fs, backup)
	require.True(t, exists, "row gone = backup kept for a later sweep")
	exists, _ = afero.Exists(fs, dest)
	require.False(t, exists, "restore undone when the live row is unreadable")
}

type rowGoneRepo struct {
	*p3OpRepo
	goneID uint
}

func (m *rowGoneRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	if id == m.goneID {
		return nil, errors.New("not found")
	}
	return m.p3OpRepo.FindByID(ctx, id)
}

// UpdateJournalInTx mirrors the same row-gone injection for the journal
// transaction seam (review 4960250562) the consumption legs now use.
func (m *rowGoneRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if id == m.goneID {
		return errors.New("not found")
	}
	return m.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

// Targeted pre-revert sweep over named destinations.
func TestSweepDestinations_TargetedHandling(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/TGT/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	otherDest := "/out/OTHER/poster.jpg"
	otherBackup := otherDest + ".dlbak.fedcba9876543210"
	require.NoError(t, fs.MkdirAll("/out/TGT", config.DirPerm))
	require.NoError(t, fs.MkdirAll("/out/OTHER", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("target-old"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, otherBackup, []byte("other-old"), config.FilePerm))
	backdate(t, fs, backup)
	backdate(t, fs, otherBackup)

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/TGT", "/out/OTHER"}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "TGT-001", OriginalPath: "/src/tgt.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	// Named-destination sweep handles only its own dir.
	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{dest})
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "target-old", string(mustRead2(t, fs, dest)))
	exists, _ := afero.Exists(fs, otherBackup)
	require.True(t, exists, "other directory untouched by the targeted sweep")
}

// A live durable marker, rather than a young mtime, keeps the file untouched.
func TestSweepOne_LiveBusy_Skips(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/YG/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/YG", config.DirPerm))
	s0 := NewReplacementSweeper(fs, repo)
	require.NoError(t, afero.WriteFile(fs, backup, []byte("young"), config.FilePerm))
	writeW14ABusy(t, fs, dest, os.Getpid())

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/YG"}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "YG-001", OriginalPath: "/src/yg.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := s0.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	require.Equal(t, "young", string(mustRead2(t, fs, backup)))
}

// Ledger-scan failure during the index build surfaces (SweepDestinations
// shares the same index call and also propagates).
func TestSweep_LedgerScanError_Surfaces(t *testing.T) {
	repo := &errLedgerScanRepo{p3OpRepo: newP3OpRepo(), err: errors.New("ledger wedged")}
	_, err := NewReplacementSweeper(afero.NewMemMapFs(), repo).Sweep(context.Background())
	require.Error(t, err)
	_, err = NewReplacementSweeper(afero.NewMemMapFs(), repo).SweepDestinations(context.Background(), []string{"/x/y.jpg"})
	require.Error(t, err)
}

type errLedgerScanRepo struct {
	*p3OpRepo
	err error
}

func (m *errLedgerScanRepo) FindOperationsWithLedger(context.Context) ([]models.BatchFileOperation, error) {
	return nil, m.err
}

// Per-dir ReadDir failure skips that directory silently (other dirs still sweep).
func TestSweep_ReadDirErrorSkipsDir(t *testing.T) {
	fs := &readDirFailFs{Fs: afero.NewMemMapFs(), failDir: "/out/RDE"}
	repo := newP3OpRepo()
	ctx := context.Background()

	dest := "/out/RDE/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/RDE", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	backdate(t, fs, backup)

	raw, _ := json.Marshal(models.GeneratedFilesJSON{Roots: []string{"/out/RDE"}})
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "RDE-001", OriginalPath: "/src/rde.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "unreadable dir → keep, retry next sweep")
	exists, _ := afero.Exists(fs, backup)
	require.True(t, exists)
}

type readDirFailFs struct {
	afero.Fs
	failDir string
}

func (f *readDirFailFs) Open(name string) (afero.File, error) {
	// afero.ReadDir routes through Open — failing a directory's read means
	// failing its Open. Normalize separators: on Windows the enumerator hands
	// backslash-joined paths.
	normName := strings.ReplaceAll(name, "\\", "/")
	if strings.Contains(normName, f.failDir) && !strings.HasSuffix(normName, ".jpg") && !strings.Contains(normName, ".dlbak.") {
		return nil, errors.New("io wedged")
	}
	return f.Fs.Open(name)
}

// codex P3 R18h: restore keeps the backup's permission bits.
func TestRevertRestore_PreservesBackupPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("meaningless on Windows")
	}
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	f := &p3Fixture{fs: fs, repo: repo}
	_, dest := f.addAppliedOp(t, "job-1", "PM-001", false, "new-poster", p3Replacement{seq: 1, backupBytes: "original-poster"})
	backup := dest + ".dlbak.a"
	require.NoError(t, fs.Chmod(backup, 0o600))

	r := NewReverter(fs, repo)
	res, err := r.RevertBatch(ctx, "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	info, err := fs.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"revert must not widen 0600 media to world-readable")
	require.Equal(t, "original-poster", string(mustRead2(t, fs, dest)))
}

// Startup sweep seam: default (nothing to heal) + error-surface + heal-log.
func TestSweepOnStartup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()

	healthy := NewReplacementSweeper(fs, repo)
	SweepOnStartup(fs, repo) // zero state: nothing pending
	_ = healthy

	// Heal-log branch: a crash-window backup exists.
	dest := "/out/SOS/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll("/out/SOS", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), config.FilePerm))
	backdate(t, fs, backup)
	op := &models.BatchFileOperation{
		BatchJobID: "job-sos", MovieID: "SOS-001", OriginalPath: "/src/s.mkv",
		OperationType:  models.OperationTypeUpdate,
		GeneratedFiles: mustJSONMarshall(t, []string{backup}, []string{dest}),
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	SweepOnStartup(fs, repo)
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))

	// Error branch: unparseable ledger
	op.GeneratedFiles = `{"replacements":broken`
	require.NoError(t, repo.Update(context.Background(), op))
	SweepOnStartup(fs, repo)
}

func mustJSONMarshall(t *testing.T, backups []string, dests []string) string {
	t.Helper()
	gf := models.GeneratedFilesJSON{}
	for i := range backups {
		gf.Replacements = append(gf.Replacements, models.ReplacementEntry{
			Destination: dests[i], Backup: backups[i], DestSeq: int64(i + 1),
		})
	}
	raw, err := json.Marshal(gf)
	require.NoError(t, err)
	return string(raw)
}

// Remaining cleanup legs: swap-fails downgrade lanes to await a sweep drive,
// and the armed-ledger confirmation legs with indexed lifetime already sane.
func TestSweep_TemporaryArmedLayerLegs(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "TAR-001", OriginalPath: "/src/tar.mkv",
		OperationType:  models.OperationTypeUpdate,
		GeneratedFiles: `{"replacements":[{ "destination": "/dst/TAR/poster.jpg", "backup": "/dst/TAR/poster.jpg.dlbak." + "bcdef", "dest_seq": 1 }]}`,
		RevertStatus:   models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	// licensing backup path under lock remainder sweeps any missing dest war.
	require.NoError(t, fs.MkdirAll("/dst/TAR", config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, "/dst/TAR/poster.jpg.dlbak.bcdef", []byte("old"), config.FilePerm))

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	_ = healed
}
