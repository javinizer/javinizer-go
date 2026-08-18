package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// P3 reverter move-back: journaled media replacements restore in true reverse
// destination-sequence order, reject climbs above newer applied owners, and
// gate status transitions on full move-back success.

// p3OpRepo is a full in-memory BatchFileOperationRepositoryInterface fixture.
type p3OpRepo struct {
	mu     sync.Mutex
	ops    map[uint]*models.BatchFileOperation
	nextID uint
}

func newP3OpRepo() *p3OpRepo {
	return &p3OpRepo{ops: map[uint]*models.BatchFileOperation{}, nextID: 1}
}

func (m *p3OpRepo) Create(_ context.Context, op *models.BatchFileOperation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	op.ID = m.nextID
	m.nextID++
	cp := *op
	m.ops[op.ID] = &cp
	return nil
}

func (m *p3OpRepo) CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error {
	for _, op := range ops {
		if err := m.Create(ctx, op); err != nil {
			return err
		}
	}
	return nil
}

func (m *p3OpRepo) FindByID(_ context.Context, id uint) (*models.BatchFileOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if op, ok := m.ops[id]; ok {
		cp := *op
		return &cp, nil
	}
	return nil, errors.New("not found")
}

func (m *p3OpRepo) byPred(pred func(*models.BatchFileOperation) bool) []models.BatchFileOperation {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.BatchFileOperation, 0, len(m.ops))
	for _, op := range m.ops {
		if pred(op) {
			out = append(out, *op)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *p3OpRepo) FindByBatchJobID(_ context.Context, batchJobID string) ([]models.BatchFileOperation, error) {
	return m.byPred(func(op *models.BatchFileOperation) bool { return op.BatchJobID == batchJobID }), nil
}

func (m *p3OpRepo) FindByBatchJobIDAndRevertStatus(_ context.Context, batchJobID string, status models.RevertStatusEnum) ([]models.BatchFileOperation, error) {
	return m.byPred(func(op *models.BatchFileOperation) bool {
		return op.BatchJobID == batchJobID && op.RevertStatus == status
	}), nil
}

func (m *p3OpRepo) Update(_ context.Context, op *models.BatchFileOperation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *op
	m.ops[op.ID] = &cp
	return nil
}

// UpdateNonJournalFields mirrors the wave-15 production contract in-memory:
// every non-journal column follows op; generated_files stays with the stored
// row (UpdateJournalInTx owns it); and when the stored row is already
// reverted while op carries a completion status, the reverted status stays
// authoritative and the typed ErrOperationRowReverted race error surfaces,
// exactly like the sqlite repository.
func (m *p3OpRepo) UpdateNonJournalFields(_ context.Context, op *models.BatchFileOperation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *op
	if stored, ok := m.ops[op.ID]; ok {
		cp.GeneratedFiles = stored.GeneratedFiles
		if stored.RevertStatus == models.RevertStatusReverted && op.RevertStatus != models.RevertStatusReverted {
			cp.RevertStatus = stored.RevertStatus
			cp.RevertedAt = stored.RevertedAt
			m.ops[op.ID] = &cp
			return fmt.Errorf("w15 mirror: %w: batch file operation %d", database.ErrOperationRowReverted, op.ID)
		}
	}
	m.ops[op.ID] = &cp
	return nil
}

// UpdateJournalInTx mirrors the production repo's transaction contract
// in-memory: the mutex plays the BEGIN IMMEDIATE write lock, the merge runs
// against the freshly read stored row (ID/GeneratedFiles/RevertStatus
// hydrated, as the real lean view is), and a fn error rolls back untouched.
func (m *p3OpRepo) UpdateJournalInTx(_ context.Context, id uint, fn database.JournalUpdateFn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.ops[id]
	if !ok {
		return fmt.Errorf("update journal tx row %d: %w", id, database.ErrNotFound)
	}
	current := &models.BatchFileOperation{
		ID:             stored.ID,
		GeneratedFiles: stored.GeneratedFiles,
		RevertStatus:   stored.RevertStatus,
	}
	next, persist, err := fn(current)
	if err != nil {
		return err
	}
	if persist {
		cp := *stored
		cp.GeneratedFiles = models.MarshalLedgerJSON(next)
		m.ops[id] = &cp
	}
	return nil
}

func (m *p3OpRepo) UpdateRevertStatus(_ context.Context, id uint, status models.RevertStatusEnum) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if op, ok := m.ops[id]; ok {
		op.RevertStatus = status
	}
	return nil
}

func (m *p3OpRepo) CountByBatchJobID(context.Context, string) (int64, error) { return 0, nil }
func (m *p3OpRepo) CountByBatchJobIDAndRevertStatus(context.Context, string, models.RevertStatusEnum) (int64, error) {
	return 0, nil
}
func (m *p3OpRepo) CountByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, nil
}
func (m *p3OpRepo) CountRevertedByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, nil
}

func (m *p3OpRepo) FindOperationsWithLedger(_ context.Context) ([]models.BatchFileOperation, error) {
	return m.byPred(func(op *models.BatchFileOperation) bool { return op.GeneratedFiles != "" }), nil
}

func (m *p3OpRepo) FindOperationsWithReplacements(_ context.Context) ([]models.BatchFileOperation, error) {
	return m.byPred(func(op *models.BatchFileOperation) bool {
		gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
		return err == nil && len(gf.Replacements) > 0
	}), nil
}

func (m *p3OpRepo) FindOperationsByDestination(_ context.Context, destination string) ([]models.BatchFileOperation, error) {
	// Mirror production (database repo): destination comparisons use the
	// probe-aware key form — backslash/slash normalize under the configured
	// platform seam and case folds only on insensitive/tolerant roots.
	want := sweepSlash2Test(destination)
	return m.byPred(func(op *models.BatchFileOperation) bool {
		gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
		if err != nil {
			return false
		}
		for _, rep := range gf.Replacements {
			if sweepSlash2Test(rep.Destination) == want {
				return true
			}
		}
		return false
	}), nil
}

func sweepSlash2Test(p string) string { return sweepSlash(p) }

// p3Fixture builds one applied Move operation whose apply overwrote media at
// <dest> (current content there is `current`), journaling the backup bytes
// under the per-op backup paths provided.
type p3Fixture struct {
	fs   afero.Fs
	repo *p3OpRepo
}

func newP3Fixture() *p3Fixture {
	return &p3Fixture{fs: afero.NewMemMapFs(), repo: newP3OpRepo()}
}

type p3Replacement struct {
	seq         int64
	backupBytes string
}

// addAppliedOp creates the applied operation row + on-disk state: video at
// NewPath (anchor), destination currently holding `currentBytes`, and one
// backup file per journaled replacement (backup path derived like the
// downloader's: dest + ".dlbak." + seq).
func (f *p3Fixture) addAppliedOp(t *testing.T, jobID, movieID string, includesDest bool, currentBytes string, replacements ...p3Replacement) (*models.BatchFileOperation, string) {
	t.Helper()
	newPath := "/dst/" + movieID + "/" + movieID + ".mkv"
	origPath := "/src/" + movieID + ".mkv"
	dest := "/dst/" + movieID + "/poster.jpg"

	require.NoError(t, f.fs.MkdirAll("/dst/"+movieID, config.DirPerm))
	require.NoError(t, afero.WriteFile(f.fs, newPath, []byte("video-"+movieID), config.FilePerm))
	require.NoError(t, afero.WriteFile(f.fs, dest, []byte(currentBytes), config.FilePerm))

	gf := models.GeneratedFilesJSON{}
	if includesDest {
		gf.Delete = []string{dest, "/dst/" + movieID + "/extra-info.txt"}
	} else {
		gf.Delete = []string{"/dst/" + movieID + "/extra-info.txt"}
	}
	require.NoError(t, afero.WriteFile(f.fs, "/dst/"+movieID+"/extra-info.txt", []byte("noise"), config.FilePerm))

	for _, rep := range replacements {
		backup := dest + ".dlbak." + string(rune('a'+rep.seq-1))
		require.NoError(t, afero.WriteFile(f.fs, backup, []byte(rep.backupBytes), config.FilePerm))
		gf.Replacements = append(gf.Replacements, models.ReplacementEntry{Destination: dest, Backup: backup, DestSeq: rep.seq})
	}
	raw, err := json.Marshal(gf)
	require.NoError(t, err)

	op := &models.BatchFileOperation{
		BatchJobID: jobID, MovieID: movieID, OriginalPath: origPath, NewPath: newPath,
		OperationType: models.OperationTypeMove, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, f.repo.Create(context.Background(), op))
	return op, dest
}

func p3ReadFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return string(data)
}

func TestRevert_OverwriteReplacedPoster_RestoresOriginalBytes(t *testing.T) {
	f := newP3Fixture()
	op, dest := f.addAppliedOp(t, "job-1", "AAA-001", false, "new-poster", p3Replacement{seq: 1, backupBytes: "original-poster"})
	backup := op.RevertStatus // silence unused pattern clarity below
	_ = backup

	r := NewReverter(f.fs, f.repo)
	res, err := r.RevertBatch(context.Background(), "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)
	require.Equal(t, "original-poster", p3ReadFile(t, f.fs, dest), "revert must put the pre-overwrite bytes back")

	exists, err := afero.Exists(f.fs, dest+".dlbak.a")
	require.NoError(t, err)
	require.False(t, exists, "consumed backup must be moved back, not left behind")

	row, err := f.repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "consumed journal entries must not linger")
}

func TestRevert_TwoStackedOverwrites_RestoresReverseChronological(t *testing.T) {
	f := newP3Fixture()
	// One operation stacked two replaces on the same destination: seq1 set
	// aside the original bytes, seq2 set aside the intermediate bytes.
	_, dest := f.addAppliedOp(t, "job-1", "STK-001", false, "newest",
		p3Replacement{seq: 1, backupBytes: "original"},
		p3Replacement{seq: 2, backupBytes: "intermediate"})

	r := NewReverter(f.fs, f.repo)
	res, err := r.RevertBatch(context.Background(), "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)
	require.Equal(t, "original", p3ReadFile(t, f.fs, dest),
		"stacked replaces must unwind in true reverse sequence, landing on the oldest bytes")
}

func TestRevert_OverwriteOrder_UsesDestSequenceNotBeginOrder(t *testing.T) {
	f := newP3Fixture()
	// opA began first (lower row id) but replaced LAST (DestSeq 2); opB began
	// second but replaced FIRST (DestSeq 1). True reverse order restores A
	// then B — id-ordered restore would land on "B-bytes" (wrong).
	opA, destA := f.addAppliedOp(t, "job-1", "ORD-00A", false, "A-bytes", p3Replacement{seq: 2, backupBytes: "B-bytes"})
	require.NotNil(t, opA)
	// opB shares the same destination pattern? destination differs per movie —
	// give opB the same destination by rewriting its journal entry below.
	opB, _ := f.addAppliedOp(t, "job-1", "ORD-00B", false, "B-unused", p3Replacement{seq: 1, backupBytes: "original"})

	// Repoint opB's journal (and its backup file) at opA's destination so both
	// operations chain on one destination — the cross-operation case.
	bBackup := destA + ".dlbak.a"
	staleBackup := "/dst/ORD-00B/poster.jpg.dlbak.a"
	data, err := afero.ReadFile(f.fs, staleBackup)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(f.fs, bBackup, data, config.FilePerm))
	require.NoError(t, f.fs.Remove(staleBackup))
	gf, err := models.ParseGeneratedFiles(opB.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Destination = destA
	gf.Replacements[0].Backup = bBackup
	raw, err := json.Marshal(gf)
	require.NoError(t, err)
	opB.GeneratedFiles = string(raw)
	require.NoError(t, f.repo.Update(context.Background(), opB))

	r := NewReverter(f.fs, f.repo)
	res, err := r.RevertBatch(context.Background(), "job-1")
	require.NoError(t, err)
	require.Equal(t, 2, res.Succeeded, "both operations revert; neither triggers the newer-applied rejection inside one run")
	require.Equal(t, "original", p3ReadFile(t, f.fs, destA),
		"reverse destination-sequence order (A seq2 then B seq1) must land on the oldest bytes")
}

func TestRevert_OlderOperationWithNewerAppliedDest_RejectedWithInstruction(t *testing.T) {
	f := newP3Fixture()
	// Older op (seq 1) is scrape-reverted alone while a NEWER owner (seq 2)
	// still stands applied on the same destination.
	opOld, dest := f.addAppliedOp(t, "job-1", "OLD-001", false, "newer-bytes", p3Replacement{seq: 1, backupBytes: "oldest"})
	opNew, _ := f.addAppliedOp(t, "job-1", "NEW-001", false, "unused", p3Replacement{seq: 2, backupBytes: "old"})
	newBackup := dest + ".dlbak.b"
	data, err := afero.ReadFile(f.fs, "/dst/NEW-001/poster.jpg.dlbak.b")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(f.fs, newBackup, data, config.FilePerm))
	require.NoError(t, f.fs.Remove("/dst/NEW-001/poster.jpg.dlbak.b"))
	gf, err := models.ParseGeneratedFiles(opNew.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Destination = dest
	gf.Replacements[0].Backup = newBackup
	raw, err := json.Marshal(gf)
	require.NoError(t, err)
	opNew.GeneratedFiles = string(raw)
	require.NoError(t, f.repo.Update(context.Background(), opNew))

	r := NewReverter(f.fs, f.repo)
	res, err := r.RevertScrape(context.Background(), "job-1", "OLD-001")
	require.NoError(t, err)
	require.Equal(t, 1, res.Failed)
	require.Len(t, res.Outcomes, 1)
	require.Equal(t, models.RevertOutcomeFailed, res.Outcomes[0].Outcome)
	require.Contains(t, res.Outcomes[0].Error, "revert that operation first", "rejection must instruct the operator")

	row, err := f.repo.FindByID(context.Background(), opOld.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusApplied, row.RevertStatus, "rejected revert must not flip status")
	require.Equal(t, "newer-bytes", p3ReadFile(t, f.fs, dest), "destination must remain the newer operation's bytes")
}

func TestRevert_MoveBackFailure_LeavesOperationApplied(t *testing.T) {
	f := newP3Fixture()
	op, dest := f.addAppliedOp(t, "job-1", "MBF-001", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	// Sabotage: the journaled backup vanished before the revert ran.
	require.NoError(t, f.fs.Remove(dest+".dlbak.a"))

	r := NewReverter(f.fs, f.repo)
	res, err := r.RevertBatch(context.Background(), "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Failed, "failed move-back fails the operation, not silently skips it")

	row, err := f.repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, models.RevertStatusApplied, row.RevertStatus, "failed move-back leaves the operation Applied")
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "failed entry must stay journaled for retry/sweep")
}

func TestCleanupGeneratedFiles_MoveBackBeforeDelete_NoClobber(t *testing.T) {
	f := newP3Fixture()
	// codex R15-2 model: delete-list membership proves the op CREATED this
	// path (CreatedPaths excludes replaced destinations). A create-then-
	// replace chain inside one op unwinds to NOTHING pre-existing: the
	// journaled backup holds THIS op's intermediate bytes, and the revert
	// winds the whole chain away.
	_, dest := f.addAppliedOp(t, "job-1", "MBD-001", true, "new-poster", p3Replacement{seq: 1, backupBytes: "original-poster"})

	r := NewReverter(f.fs, f.repo)
	res, err := r.RevertBatch(context.Background(), "job-1")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	exists, err := afero.Exists(f.fs, dest)
	require.NoError(t, err)
	require.False(t, exists,
		"op-created chain fully unwinds — nothing pre-existed at this destination")

	exists, err = afero.Exists(f.fs, "/dst/MBD-001/extra-info.txt")
	require.NoError(t, err)
	require.False(t, exists, "non-restored delete-list entries still sweep")
}
