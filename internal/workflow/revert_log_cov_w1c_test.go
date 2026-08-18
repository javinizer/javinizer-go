package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestW1CRevertLogMergeFallbackArms(t *testing.T) {
	fresh := `{"delete":["/fresh/movie.nfo"]}`
	priorWithReplacement := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{
			Destination: "/dest/movie.jpg",
			Backup:      "/dest/movie.jpg.dlbak",
			DestSeq:     3,
		}},
	})

	// Exercise both sides of the prior-ledger fallback and the fresh-ledger
	// parse fallback. These are legacy/malformed rows that Complete must not
	// silently turn into a replacement-only ledger.
	tests := []struct {
		name  string
		prior string
		fresh string
		want  string
	}{
		{name: "malformed prior", prior: `{"replacements":broken`, fresh: fresh, want: fresh},
		{name: "empty replacements prior", prior: `{"replacements":[]}`, fresh: fresh, want: fresh},
		{name: "empty object prior", prior: `{}`, fresh: fresh, want: fresh},
		{name: "malformed fresh", prior: priorWithReplacement, fresh: `{"delete":broken`, want: `{"delete":broken`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mergeReplacementLedger(tt.prior, tt.fresh))
		})
	}

	// A non-empty prior root makes the prior condition false while still
	// exercising the successful fresh parse and root preservation.
	priorWithRoot := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/dest/root"}})
	merged := mergeReplacementLedger(priorWithRoot, fresh)
	got, err := models.ParseGeneratedFiles(merged)
	require.NoError(t, err)
	require.Equal(t, []string{"/dest/root"}, got.Roots)
	require.Equal(t, []string{"/fresh/movie.nfo"}, got.Delete)

	// appendLedgerRoot's tolerances stand on their own after seedRoot moved to
	// the journal transaction (review 4960250562): malformed bodies return
	// byte-identical, dedup keeps the blob, empty seeds a root-only ledger.
	require.Equal(t, `{"roots":broken`, appendLedgerRoot(`{"roots":broken`, "/root"))
	seeded := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{"/dest/root"}})
	require.Equal(t, seeded, appendLedgerRoot(seeded, "/dest/root"), "duplicate root is a no-op")
	gotRootless, err := models.ParseGeneratedFiles(appendLedgerRoot("", "/dest/root"))
	require.NoError(t, err)
	require.Equal(t, []string{"/dest/root"}, gotRootless.Roots)
}

func TestW1CRevertLogMutatorsNilRows(t *testing.T) {
	ctx := context.Background()

	// The mutators now persist through UpdateJournalInTx (review 4960250562):
	// a missing row surfaces as database.ErrNotFound out of the transaction,
	// which every mutator maps to its "record not found" leg.
	notFoundTx := fmt.Errorf("journal tx: %w", database.ErrNotFound)

	recordRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	recordRepo.On("FindOperationsByDestination", mock.Anything, mock.Anything).Return([]models.BatchFileOperation(nil), nil)
	recordRepo.On("UpdateJournalInTx", mock.Anything, uint(1), mock.Anything).Return(notFoundTx)
	recordLog := NewDBRevertLog(recordRepo, nil, "job", nil, nil, nil, nil)
	require.Error(t, recordLog.RecordReplacement(ctx, "1", "/dest/file.jpg", "/dest/file.jpg.bak"))

	releaseRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	releaseRepo.On("UpdateJournalInTx", mock.Anything, uint(1), mock.Anything).Return(notFoundTx)
	releaseLog := NewDBRevertLog(releaseRepo, nil, "job", nil, nil, nil, nil)
	require.Error(t, releaseLog.ReleaseReplacement(ctx, "1", "/dest/file.jpg", "/dest/file.jpg.bak"))

	confirmRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	confirmRepo.On("UpdateJournalInTx", mock.Anything, uint(1), mock.Anything).Return(notFoundTx)
	confirmLog := NewDBRevertLog(confirmRepo, nil, "job", nil, nil, nil, nil)
	require.Error(t, confirmLog.ConfirmReplacement(ctx, "1", "/dest/file.jpg", "/dest/file.jpg.bak"))

	seedRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	seedRepo.On("UpdateJournalInTx", mock.Anything, uint(1), mock.Anything).Return(notFoundTx)
	seedLog := NewDBRevertLog(seedRepo, nil, "job", nil, nil, nil, nil).(*dbRevertLog)
	require.Error(t, seedLog.seedRoot(ctx, "1", "/dest/root"))
}

func TestW1CReleaseReplacementKeepsUnmatchedEntries(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	ledger := models.MarshalLedgerJSON(models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{
			Destination: "/dest/kept.jpg",
			Backup:      "/dest/kept.jpg.bak",
			DestSeq:     1,
		}},
	})
	// The release merge runs inside the journal transaction against the
	// freshly re-read row; drive fn to prove the unmatched entry is untouched
	// and persist stays false (idempotent no-op).
	repo.On("UpdateJournalInTx", mock.Anything, uint(1), mock.Anything).Return(func(_ context.Context, _ uint, fn database.JournalUpdateFn) error {
		next, persist, err := fn(&models.BatchFileOperation{ID: 1, GeneratedFiles: ledger})
		require.NoError(t, err)
		require.False(t, persist, "unmatched release is a journal no-op")
		require.Len(t, next.Replacements, 1, "unmatched entries are kept")
		return err
	})
	log := NewDBRevertLog(repo, nil, "job", nil, nil, nil, nil)

	// The requested pair is absent, so the existing entry takes the append
	// arm and the idempotent no-change return follows.
	require.NoError(t, log.ReleaseReplacement(context.Background(), "1", "/dest/other.jpg", "/dest/other.jpg.bak"))
}

func TestW1CNextDestSequenceSkipsMalformedRows(t *testing.T) {
	ctx := context.Background()
	destination := "/dest/sequence.jpg"
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	repo.On("FindOperationsByDestination", mock.Anything, destination).Return([]models.BatchFileOperation{
		{GeneratedFiles: `{"replacements":broken`},
		{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: destination, Backup: "/dest/old.bak", DestSeq: 7}},
		})},
	}, nil)
	// The append lands through the journal transaction: drive fn against the
	// lean row the production merge sees and attest the computed sequence.
	repo.On("UpdateJournalInTx", mock.Anything, uint(1), mock.Anything).Return(func(_ context.Context, id uint, fn database.JournalUpdateFn) error {
		next, persist, err := fn(&models.BatchFileOperation{ID: id, GeneratedFiles: `{}`})
		require.NoError(t, err)
		require.True(t, persist, "a fresh append always persists")
		require.Len(t, next.Replacements, 1)
		require.Equal(t, int64(8), next.Replacements[0].DestSeq, "malformed sibling rows are skipped for the sequence floor")
		return err
	})

	log := NewDBRevertLog(repo, nil, "job", nil, nil, nil, nil)
	require.NoError(t, log.RecordReplacement(ctx, "1", destination, "/dest/new.bak"))
}
