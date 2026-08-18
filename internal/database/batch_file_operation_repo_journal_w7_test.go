package database

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING P1 review (journal serialization): the atomic
// generated-files RMW contract under CONCURRENT writers — every armed entry
// from every party must survive on one operation row, and a failed mutation
// must leave the previously committed journal byte-identical.

// TestUpdateJournalInTx_ConcurrentAppendsPreserveAllEntries hammers one
// operation row with two goroutines × 50 journal appends through TWO
// independent connection pools over one file database (the cross-process
// analogue of an apply and a revert racing on the same row). The final
// journal must contain exactly all 100 entries — no lost appends, no
// resurrected snapshots.
func TestUpdateJournalInTx_ConcurrentAppendsPreserveAllEntries(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "append-race.db")

	dbA, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, dbA.RunMigrationsOnStartup(ctx))
	defer func() { _ = dbA.Close() }()
	repoA := NewBatchFileOperationRepository(dbA)

	op := &models.BatchFileOperation{
		BatchJobID: "job-append-race", MovieID: "APR-001", OriginalPath: "/src/apr.mkv",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{}),
	}
	require.NoError(t, repoA.Create(ctx, op))

	dbB, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	defer func() { _ = dbB.Close() }()
	repoB := NewBatchFileOperationRepository(dbB)

	appendEntries := func(repo *BatchFileOperationRepository, side string) error {
		for i := 0; i < 50; i++ {
			dest := fmt.Sprintf("/dst/%s/%02d/poster.jpg", side, i)
			backup := fmt.Sprintf("/dst/%s/%02d/poster.jpg.dlbak.%016x", side, i, i+1)
			seq := int64(i + 1)
			if err := repo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
				gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
				if perr != nil {
					return models.GeneratedFilesJSON{}, false, perr
				}
				gf.Replacements = append(gf.Replacements, models.ReplacementEntry{Destination: dest, Backup: backup, DestSeq: seq})
				return gf, true, nil
			}); err != nil {
				return fmt.Errorf("%s append %d: %w", side, i, err)
			}
		}
		return nil
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs <- appendEntries(repoA, "apply") }()
	go func() { defer wg.Done(); errs <- appendEntries(repoB, "revert") }()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	row, err := repoA.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 100, "every concurrent append committed exactly once")

	seen := map[string]bool{}
	for _, e := range gf.Replacements {
		require.False(t, seen[e.Backup], "duplicate entry %s — an append was double-committed", e.Backup)
		seen[e.Backup] = true
	}
	for i := 0; i < 50; i++ {
		require.True(t, seen[fmt.Sprintf("/dst/apply/%02d/poster.jpg.dlbak.%016x", i, i+1)], "apply-side append %d survived", i)
		require.True(t, seen[fmt.Sprintf("/dst/revert/%02d/poster.jpg.dlbak.%016x", i, i+1)], "revert-side append %d survived", i)
	}
}

// TestUpdateJournalInTx_FnErrorLeavesPriorLedgerIntact pins the rollback
// contract: a merge that computes a full next ledger and THEN returns an
// error must leave the previously committed journal byte-identical (and
// non-journal columns untouched).
func TestUpdateJournalInTx_FnErrorLeavesPriorLedgerIntact(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{
		Roots: []string{"/dst/root-a", "/dst/root-b"},
		Replacements: []models.ReplacementEntry{
			{Destination: "/dst/a/poster.jpg", Backup: "/dst/a/poster.jpg.dlbak.0123456789abcdef", DestSeq: 1, Installed: true},
			{Destination: "/dst/b/poster.jpg", Backup: "/dst/b/poster.jpg.dlbak.fedcba9876543210", DestSeq: 2},
		},
	})
	prior, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Contains(t, prior.GeneratedFiles, "0123456789abcdef", "seed landed")

	sentinel := errors.New("merge rejected: ledger conflicts with live chain")
	calls := 0
	err = repo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		calls++
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		require.NoError(t, perr)
		gf.Replacements = append(gf.Replacements, models.ReplacementEntry{Destination: "/dst/c/poster.jpg", Backup: "/dst/c/poster.jpg.dlbak.aaaabbbbccccdddd", DestSeq: 3})
		return gf, true, sentinel // merge computed, then refused
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, calls, "fn ran once inside the rolled-back transaction")

	after, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, prior.GeneratedFiles, after.GeneratedFiles, "byte-identical rollback")
	require.Equal(t, prior.UpdatedAt, after.UpdatedAt, "no phantom timestamp bump")
	require.Equal(t, models.RevertStatusApplied, after.RevertStatus)

	gf, err := models.ParseGeneratedFiles(after.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 2, "the refused append never landed")
}
