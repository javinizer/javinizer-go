package workflow

// POSTER-WRITE-HARDENING codex PR#215 wave-25 coverage —
//
//   - RecordReplacement stamp contract (finding 2): the downloader's
//     set-aside identity facts (size + mtime seconds) land atomically in the
//     same journal write as the entry itself; absent variadic args leave the
//     entry unstamped (legacy shape, byte-identical blobs).
//   - Completion-side error legs: persistNonJournalColumns (Complete nil and
//     non-nil result, CompleteFailed nil result) and mergeJournalInTx
//     (CompleteFailed non-nil result) — a non-reverted persistence failure
//     must surface, never be swallowed.

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w25FailNonJournalRepo fails ONLY UpdateNonJournalFields.
type w25FailNonJournalRepo struct {
	repo *database.BatchFileOperationRepository
	err  error
}

func (f *w25FailNonJournalRepo) Create(ctx context.Context, op *models.BatchFileOperation) error {
	return f.repo.Create(ctx, op)
}
func (f *w25FailNonJournalRepo) CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error {
	return f.repo.CreateBatch(ctx, ops)
}
func (f *w25FailNonJournalRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	return f.repo.FindByID(ctx, id)
}
func (f *w25FailNonJournalRepo) FindByBatchJobID(ctx context.Context, id string) ([]models.BatchFileOperation, error) {
	return f.repo.FindByBatchJobID(ctx, id)
}
func (f *w25FailNonJournalRepo) FindByBatchJobIDAndRevertStatus(ctx context.Context, id string, s models.RevertStatusEnum) ([]models.BatchFileOperation, error) {
	return f.repo.FindByBatchJobIDAndRevertStatus(ctx, id, s)
}
func (f *w25FailNonJournalRepo) Update(ctx context.Context, op *models.BatchFileOperation) error {
	return f.repo.Update(ctx, op)
}
func (f *w25FailNonJournalRepo) UpdateNonJournalFields(context.Context, *models.BatchFileOperation) error {
	return f.err
}
func (f *w25FailNonJournalRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	return f.repo.UpdateJournalInTx(ctx, id, fn)
}
func (f *w25FailNonJournalRepo) UpdateRevertStatus(ctx context.Context, id uint, s models.RevertStatusEnum) error {
	return f.repo.UpdateRevertStatus(ctx, id, s)
}
func (f *w25FailNonJournalRepo) CountByBatchJobID(context.Context, string) (int64, error) {
	return 0, nil
}
func (f *w25FailNonJournalRepo) CountByBatchJobIDAndRevertStatus(context.Context, string, models.RevertStatusEnum) (int64, error) {
	return 0, nil
}
func (f *w25FailNonJournalRepo) CountByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, nil
}
func (f *w25FailNonJournalRepo) CountRevertedByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, nil
}
func (f *w25FailNonJournalRepo) FindOperationsByDestination(ctx context.Context, d string) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsByDestination(ctx, d)
}
func (f *w25FailNonJournalRepo) FindOperationsWithReplacements(ctx context.Context) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsWithReplacements(ctx)
}
func (f *w25FailNonJournalRepo) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsWithLedger(ctx)
}

// w25FailJournalRepo fails ONLY UpdateJournalInTx (journal merge leg).
type w25FailJournalRepo struct {
	w25FailNonJournalRepo
}

func (f *w25FailJournalRepo) UpdateJournalInTx(context.Context, uint, database.JournalUpdateFn) error {
	return f.err
}

// Finding 2: the stamped facts land IN the same journal write and read back
// verbatim — history's removal gate consumes exactly these numbers.
func TestRevertLogW25_RecordReplacementStampsBackupFacts(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "W25STAMP-1")
	dest := "/dst/W25STAMP-1/poster.jpg"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, dest+".b",
		models.ReplacementBackupFacts{Size: 4321, ModUnix: 1_700_000_007}))

	gf := p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, int64(4321), gf.Replacements[0].BackupSize,
		"the size stamp round-trips through the persisted journal")
	require.Equal(t, int64(1_700_000_007), gf.Replacements[0].BackupModUnix,
		"the mtime stamp round-trips through the persisted journal")
	require.True(t, gf.Replacements[0].BackupFactsStamped())

	// The omission contract: no facts argument leaves the entry unstamped —
	// pre-wave-25 blobs stay byte-identical (both fields omitempty).
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, dest+".c"))
	gf = p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 2)
	require.False(t, gf.Replacements[1].BackupFactsStamped(), "variadic omission records a legacy-shape entry")
	legacyBlob := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{gf.Replacements[1]}})
	require.NotContains(t, legacyBlob, "backup_size",
		"unstamped entries serialize without the backup_size key")
	require.NotContains(t, legacyBlob, "backup_mod_unix",
		"unstamped entries serialize without the backup_mod_unix key")
}

// A non-reverted persist failure on the completion leg surfaces (Complete,
// nil result mark-failed path).
func TestRevertLogW25_CompleteNilResultPersistFailureSurfaces(t *testing.T) {
	db, repo, _ := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, newP3RecorderLog(t, repo), "W25CNF-1")
	sentinel := errors.New("w25 column store wedged")
	broken := NewDBRevertLog(&w25FailNonJournalRepo{repo: repo, err: sentinel},
		NewRevertLogConfig(true, nil), "job-w25", afero.NewMemMapFs(), nil, nil, nil)
	err := broken.Complete(ctx, opID, nil)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "mark record")
}

// Complete success path: the journal merge must succeed while only the
// follow-up non-journal publish fails.
func TestRevertLogW25_CompletePostApplyPersistFailureSurfaces(t *testing.T) {
	db, repo, _ := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, newP3RecorderLog(t, repo), "W25CPA-1")
	sentinel := errors.New("w25 column store wedged")
	broken := NewDBRevertLog(&w25FailNonJournalRepo{repo: repo, err: sentinel},
		NewRevertLogConfig(true, nil), "job-w25", afero.NewMemMapFs(), nil, nil, nil)
	err := broken.Complete(ctx, opID, &ApplyResult{Movie: &models.Movie{ID: "W25CPA-1"}})
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "update post-apply record")
}

// CompleteFailed nil-result mark-failed path: same surface contract.
func TestRevertLogW25_CompleteFailedNilResultPersistFailureSurfaces(t *testing.T) {
	db, repo, _ := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, newP3RecorderLog(t, repo), "W25CFN-1")
	sentinel := errors.New("w25 column store wedged")
	broken := NewDBRevertLog(&w25FailNonJournalRepo{repo: repo, err: sentinel},
		NewRevertLogConfig(true, nil), "job-w25", afero.NewMemMapFs(), nil, nil, nil)
	err := broken.CompleteFailed(ctx, opID, nil)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "mark record")
}

// CompleteFailed with a result: the journal merge inside the transaction is
// the failing leg.
func TestRevertLogW25_CompleteFailedJournalMergeFailureSurfaces(t *testing.T) {
	db, repo, _ := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, newP3RecorderLog(t, repo), "W25CFJ-1")
	sentinel := errors.New("w25 journal store wedged")
	broken := NewDBRevertLog(&w25FailJournalRepo{w25FailNonJournalRepo{repo: repo, err: sentinel}},
		NewRevertLogConfig(true, nil), "job-w25", afero.NewMemMapFs(), nil, nil, nil)
	err := broken.CompleteFailed(ctx, opID, &ApplyResult{Movie: &models.Movie{ID: "W25CFJ-1"}})
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "persist journal")
}

// newP3RecorderLog builds a healthy dbRevertLog on the handed repo so a test
// can Begin a row before swapping in a failing wrapper.
func newP3RecorderLog(t *testing.T, repo *database.BatchFileOperationRepository) RevertLog {
	t.Helper()
	return NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "job-w25", afero.NewMemMapFs(), nil, nil, nil)
}
