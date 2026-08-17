package workflow

import (
	"context"
	"testing"

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
}

func TestW1CRevertLogMutatorsNilRows(t *testing.T) {
	ctx := context.Background()

	recordRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	recordRepo.On("FindByID", mock.Anything, uint(1)).Return(nil, nil)
	recordLog := NewDBRevertLog(recordRepo, nil, "job", nil, nil, nil, nil)
	require.Error(t, recordLog.RecordReplacement(ctx, "1", "/dest/file.jpg", "/dest/file.jpg.bak"))

	releaseRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	releaseRepo.On("FindByID", mock.Anything, uint(1)).Return(nil, nil)
	releaseLog := NewDBRevertLog(releaseRepo, nil, "job", nil, nil, nil, nil)
	require.Error(t, releaseLog.ReleaseReplacement(ctx, "1", "/dest/file.jpg", "/dest/file.jpg.bak"))

	confirmRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	confirmRepo.On("FindByID", mock.Anything, uint(1)).Return(nil, nil)
	confirmLog := NewDBRevertLog(confirmRepo, nil, "job", nil, nil, nil, nil)
	require.Error(t, confirmLog.ConfirmReplacement(ctx, "1", "/dest/file.jpg", "/dest/file.jpg.bak"))

	seedRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	seedRepo.On("FindByID", mock.Anything, uint(1)).Return(nil, nil)
	seedLog := NewDBRevertLog(seedRepo, nil, "job", nil, nil, nil, nil).(*dbRevertLog)
	require.Error(t, seedLog.seedRoot(ctx, "1", "/dest/root"))
}

func TestW1CReleaseReplacementKeepsUnmatchedEntries(t *testing.T) {
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	row := &models.BatchFileOperation{
		ID: 1,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination: "/dest/kept.jpg",
				Backup:      "/dest/kept.jpg.bak",
				DestSeq:     1,
			}},
		}),
	}
	repo.On("FindByID", mock.Anything, uint(1)).Return(row, nil)
	log := NewDBRevertLog(repo, nil, "job", nil, nil, nil, nil)

	// The requested pair is absent, so the existing entry takes the append
	// arm and the idempotent no-change return follows.
	require.NoError(t, log.ReleaseReplacement(context.Background(), "1", "/dest/other.jpg", "/dest/other.jpg.bak"))
}

func TestW1CNextDestSequenceSkipsMalformedRows(t *testing.T) {
	ctx := context.Background()
	destination := "/dest/sequence.jpg"
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	row := &models.BatchFileOperation{ID: 1, GeneratedFiles: `{}`}
	repo.On("FindByID", mock.Anything, uint(1)).Return(row, nil)
	repo.On("FindOperationsByDestination", mock.Anything, destination).Return([]models.BatchFileOperation{
		{GeneratedFiles: `{"replacements":broken`},
		{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: destination, Backup: "/dest/old.bak", DestSeq: 7}},
		})},
	}, nil)
	repo.On("Update", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(nil)

	log := NewDBRevertLog(repo, nil, "job", nil, nil, nil, nil)
	require.NoError(t, log.RecordReplacement(ctx, "1", destination, "/dest/new.bak"))
}
