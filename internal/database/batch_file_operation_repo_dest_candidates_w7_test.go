package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING P2 review: the destination fallback must not hydrate
// every ledger row — the candidate SELECT prefilters generated_files AND
// new_path with the escaped destination pattern, then Go-side matching
// decides exactly as before.

// TestFindOperationsByDestination_BoundedFallbackFetchesOnlyCandidates seeds
// 200 operations of which exactly 3 journal the destination ONLY inside
// generated_files, plus textual decoys that must never affect the result.
// The caller-side sequence floor derives max(DestSeq)+1 == 4.
func TestFindOperationsByDestination_BoundedFallbackFetchesOnlyCandidates(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	destination := "/lib/big/poster.jpg"

	// 3 real matches living ONLY in the generated-files ledger (DestSeq 3, 1,
	// 2 in insertion order — id order must not drive the caller's max).
	seqs := []int64{3, 1, 2}
	matchIDs := map[uint]bool{}
	for i, seq := range seqs {
		op := &models.BatchFileOperation{
			BatchJobID: "job-bounded", MovieID: fmt.Sprintf("MTC-%03d", i),
			OriginalPath: fmt.Sprintf("/src/mtc-%03d.mkv", i), NewPath: fmt.Sprintf("/lib/big/mtc-%03d.mkv", i),
			OperationType: models.OperationTypeMove,
			GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
				Replacements: []models.ReplacementEntry{{
					Destination: destination,
					Backup:      fmt.Sprintf("%s.dlbak.%016x", destination, seq),
					DestSeq:     seq,
				}},
			}),
			RevertStatus: models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(ctx, op))
		matchIDs[op.ID] = true
	}

	// Decoy A: a ledger that mentions the destination as a SUBSTRING of a
	// delete-listed artifact — the LIKE prefilter catches the text, the exact
	// Go matcher must reject it.
	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID: "job-bounded", MovieID: "DECOY-A", OriginalPath: "/src/decoy-a.mkv", NewPath: "/lib/other/decoy-a.mkv",
		OperationType:  models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Delete: []string{destination + ".extra"}}),
		RevertStatus:   models.RevertStatusApplied,
	}))
	// Decoy B: new_path carries the destination text as a superstring — caught
	// by the new_path OR leg, rejected by the exact matcher (no journal entry).
	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID: "job-bounded", MovieID: "DECOY-B", OriginalPath: "/src/decoy-b.mkv", NewPath: destination + ".mkv",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))
	// 195 filler rows that neither textually nor semantically touch the
	// destination — a full-ledger scan would hydrate all of them.
	for i := 0; i < 195; i++ {
		require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
			BatchJobID: "job-bounded", MovieID: fmt.Sprintf("FIL-%03d", i),
			OriginalPath: fmt.Sprintf("/src/fil-%03d.mkv", i), NewPath: fmt.Sprintf("/lib/small/fil-%03d.mkv", i),
			OperationType: models.OperationTypeMove,
			GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
				Replacements: []models.ReplacementEntry{{
					Destination: fmt.Sprintf("/else/%03d/poster.jpg", i),
					Backup:      fmt.Sprintf("/else/%03d/poster.jpg.dlbak.%016x", i, i+1),
					DestSeq:     900 + int64(i),
				}},
			}),
			RevertStatus: models.RevertStatusApplied,
		}))
	}

	// The public path: only the 3 real rows, and the caller-derived next
	// sequence is 4 — decoys and fillers contribute nothing.
	rows, err := repo.FindOperationsByDestination(ctx, destination)
	require.NoError(t, err)
	require.Len(t, rows, 3, "only ledger-matching rows returned")
	var maxSeq int64
	for _, row := range rows {
		require.True(t, matchIDs[row.ID], "unexpected row %d (%s)", row.ID, row.MovieID)
		gf, perr := models.ParseGeneratedFiles(row.GeneratedFiles)
		require.NoError(t, perr)
		for _, rep := range gf.Replacements {
			if rep.DestSeq > maxSeq {
				maxSeq = rep.DestSeq
			}
		}
	}
	require.Equal(t, int64(3), maxSeq, "next destination sequence would be 4")

	// The bounded fallback SELECT itself hydrates only textual candidates:
	// 3 ledger matches + 2 decoys — never the 195 filler rows (pre-P2 the
	// fallback hydrated all 200).
	candidates, err := repo.findDestinationCandidates(ctx, destination)
	require.NoError(t, err)
	require.Len(t, candidates, 5, "fallback fetch is prefiltered, not a full-ledger scan")
}

// TestFindDestinationCandidates_NewPathLegRescuesFormMismatch keeps the
// cross-form ownership rescue alive without the full scan: a row whose
// ledger spells the destination under a CLEAN-EQUIVALENT form (which the
// generated_files LIKE cannot see) is still fetched because new_path carries
// the caller's spelling.
func TestFindDestinationCandidates_NewPathLegRescuesFormMismatch(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	query := "/cov/form/poster.jpg"

	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID: "job-form", MovieID: "FRM-001", OriginalPath: "/src/frm.mkv",
		NewPath:       query, // caller's spelling lives in the organized-path column
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{
				Destination: "/cov/form//poster.jpg", // clean-equivalent, textually distinct
				Backup:      "/cov/form//poster.jpg.dlbak.0123456789abcdef",
				DestSeq:     2,
			}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}))
	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID: "job-form", MovieID: "OFF-001", OriginalPath: "/src/off.mkv", NewPath: "/cov/other/poster.jpg",
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			Replacements: []models.ReplacementEntry{{Destination: "/cov/other/poster.jpg", Backup: "/cov/other/poster.jpg.dlbak.fedcba9876543210", DestSeq: 9}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}))

	rows, err := repo.FindOperationsByDestination(ctx, query)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the form-mismatched row is rescued through the new_path prefilter leg")
	require.Equal(t, "FRM-001", rows[0].MovieID)
}

// TestFindDestinationCandidates_QueryErrorSurfaces: a fallback scan failure
// must propagate (R7-2), never masquerade as absence — destructive callers
// treat unknown ownership as keep-and-retry.
func TestFindDestinationCandidates_QueryErrorSurfaces(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID: "job-err", MovieID: "ERR-001", OriginalPath: "/src/err.mkv", NewPath: "/lib/err.mkv",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))

	// Removing the new_path column breaks ONLY the fallback OR leg — the
	// generated_files prefilter still answers.
	require.NoError(t, db.DB.Exec("ALTER TABLE batch_file_operations DROP COLUMN new_path").Error)

	_, err := repo.findDestinationCandidates(ctx, "/lib/err.mkv")
	require.Error(t, err)

	_, err = repo.FindOperationsByDestination(ctx, "/lib/err.mkv")
	require.Error(t, err, "fallback failure must surface, not masquerade as absence")
}
