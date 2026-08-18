package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// POSTER-WRITE-HARDENING review 4960250562 — UpdateJournalInTx durable
// cross-process journal RMW. Unit legs cover the envelope; the concurrency
// test drives two independent connection pools over ONE file database (the
// cross-process analogue) with a deterministic gate between the in-transaction
// re-read and the commit.

func seedJournalTxRow(t *testing.T, repo *BatchFileOperationRepository, gf models.GeneratedFilesJSON) *models.BatchFileOperation {
	t.Helper()
	op := &models.BatchFileOperation{
		BatchJobID:     "job-journal-tx",
		MovieID:        "TX-001",
		OriginalPath:   "/src/tx.mkv",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: models.MarshalLedgerJSON(gf),
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op
}

func TestUpdateJournalInTx_MergePersists(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{
		Replacements: []models.ReplacementEntry{{Destination: "/dst/a.jpg", Backup: "/dst/a.jpg.dlbak.1", DestSeq: 1}},
	})

	var sawCurrent models.BatchFileOperation
	err := repo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		sawCurrent = *current
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		require.NoError(t, perr)
		gf.Replacements = append(gf.Replacements, models.ReplacementEntry{Destination: "/dst/b.jpg", Backup: "/dst/b.jpg.dlbak.1", DestSeq: 1, Installed: true})
		return gf, true, nil
	})
	require.NoError(t, err)
	require.Equal(t, op.ID, sawCurrent.ID, "fn sees the requested row id")
	require.Equal(t, models.RevertStatusApplied, sawCurrent.RevertStatus, "fn sees the committed revert status")

	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 2, "merge result committed")
	require.Equal(t, "/dst/b.jpg", gf.Replacements[1].Destination)
	require.True(t, gf.Replacements[1].Installed)
	require.Equal(t, models.RevertStatusApplied, row.RevertStatus, "non-journal columns untouched")
	require.Equal(t, op.MovieID, row.MovieID)
	require.False(t, row.UpdatedAt.Before(op.UpdatedAt), "updated_at stamped forward like Save")
}

func TestUpdateJournalInTx_PersistFalseLeavesRowUntouched(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{Roots: []string{"/dst/root"}})
	before, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)

	var sawRaw string
	err = repo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		sawRaw = current.GeneratedFiles
		return models.GeneratedFilesJSON{Roots: []string{"/elsewhere"}}, false, nil
	})
	require.NoError(t, err)
	require.Equal(t, before.GeneratedFiles, sawRaw, "fn observes the persisted ledger")
	after, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, before.GeneratedFiles, after.GeneratedFiles, "persist=false commits untouched")
	require.Equal(t, before.UpdatedAt, after.UpdatedAt, "no write means no timestamp bump")
}

func TestUpdateJournalInTx_MissingRowReportsErrNotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	called := false
	err := repo.UpdateJournalInTx(context.Background(), 424242, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		called = true
		return models.GeneratedFilesJSON{}, true, nil
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotFound)
	require.False(t, called, "fn never runs when the row is absent")
}

func TestUpdateJournalInTx_NilMergeFunctionRefused(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.Error(t, repo.UpdateJournalInTx(context.Background(), 1, nil))
}

func TestUpdateJournalInTx_FnErrorRollsBack(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{Roots: []string{"/dst/root"}})
	sentinel := errors.New("merge conflict detected")

	err := repo.UpdateJournalInTx(ctx, op.ID, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return models.GeneratedFilesJSON{}, true, sentinel
	})
	require.ErrorIs(t, err, sentinel, "fn errors propagate verbatim")

	row, ferr := repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	require.Equal(t, op.GeneratedFiles, row.GeneratedFiles, "rolled back — nothing persisted")
}

// stubConnPool makes gorm's DB() return ErrInvalidDB — the *sql.DB
// acquisition leg of UpdateJournalInTx.
type stubConnPool struct{}

func (stubConnPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("stub conn pool")
}
func (stubConnPool) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	return nil, errors.New("stub conn pool")
}
func (stubConnPool) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("stub conn pool")
}
func (stubConnPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	return &sql.Row{}
}

func TestUpdateJournalInTx_InvalidSQLDB(t *testing.T) {
	repo := NewBatchFileOperationRepository(&DB{DB: &gorm.DB{Config: &gorm.Config{ConnPool: stubConnPool{}}}})
	err := repo.UpdateJournalInTx(context.Background(), 1, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return models.GeneratedFilesJSON{}, true, nil
	})
	require.Error(t, err)
}

func TestUpdateJournalInTx_ClosedDBConnFails(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	closed, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	err = repo.UpdateJournalInTx(context.Background(), 1, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return models.GeneratedFilesJSON{}, true, nil
	})
	require.Error(t, err)
}

func TestUpdateJournalInTx_MissingTableFailsReRead(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE batch_file_operations").Error)
	err := repo.UpdateJournalInTx(context.Background(), 1, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return models.GeneratedFilesJSON{}, true, nil
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotFound, "a schema failure is not a missing row")
}

func TestUpdateJournalInTx_RejectedPersistRollsBack(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()
	op := seedJournalTxRow(t, repo, models.GeneratedFilesJSON{Roots: []string{"/dst/root"}})

	// A failing trigger turns the UPDATE leg down deterministically after fn
	// has merged; the row must keep its pre-transaction bytes.
	require.NoError(t, db.DB.Exec("CREATE TRIGGER reject_journal_update BEFORE UPDATE ON batch_file_operations BEGIN SELECT RAISE(FAIL, 'journal update rejected'); END").Error)
	err := repo.UpdateJournalInTx(ctx, op.ID, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return models.GeneratedFilesJSON{Roots: []string{"/elsewhere"}}, true, nil
	})
	require.Error(t, err)
	require.NoError(t, db.DB.Exec("DROP TRIGGER reject_journal_update").Error)

	row, ferr := repo.FindByID(ctx, op.ID)
	require.NoError(t, ferr)
	require.Equal(t, op.GeneratedFiles, row.GeneratedFiles, "the rejected write rolled back")
}

func TestUpdateJournalInTx_BeginContentionHonorsBusyHandling(t *testing.T) {
	// A competing writer holding BEGIN IMMEDIATE with a zero busy timeout on
	// the repo's pool makes the BEGIN leg fail fast instead of hanging.
	dir := t.TempDir()
	dsn := filepath.Join(dir, "busy.db")
	seed, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, seed.RunMigrationsOnStartup(context.Background()))
	seedJournalTxRow(t, NewBatchFileOperationRepository(seed), models.GeneratedFilesJSON{Roots: []string{"/dst/root"}})
	require.NoError(t, seed.Close())

	holder, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	defer func() { _ = holder.Close() }()
	holderConn, err := holder.Conn(context.Background())
	require.NoError(t, err)
	_, err = holderConn.ExecContext(context.Background(), "BEGIN IMMEDIATE")
	require.NoError(t, err)
	defer func() {
		_, _ = holderConn.ExecContext(context.Background(), "ROLLBACK")
		_ = holderConn.Close()
	}()

	raw, err := sql.Open("sqlite3", dsn+"?_busy_timeout=0")
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	gdb, err := gorm.Open(sqlite.Dialector{Conn: raw}, &gorm.Config{})
	require.NoError(t, err)
	repo := NewBatchFileOperationRepository(&DB{DB: gdb})

	err = repo.UpdateJournalInTx(context.Background(), 1, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return models.GeneratedFilesJSON{}, true, nil
	})
	require.Error(t, err)
	var sqliteErr sqlite3.Error
	require.True(t, errors.As(err, &sqliteErr), "the BEGIN contention surfaces as a sqlite busy/locked error, got %v", err)
}

// TestUpdateJournalInTx_CrossProcessJournalRace is the durable regression for
// review 4960250562: an apply-arm (record + confirm on destination A) racing a
// revert-consume (destination B) of the SAME operation row through two
// independent connection pools over one file database. A gate between the
// in-transaction re-read and the commit makes the stale-snapshot window
// deterministic: before the transaction, B would have read the same snapshot
// as A and A's late-commit would have resurrected B's consumed entry
// (last-write-wins). With BEGIN IMMEDIATE, B's re-read strictly follows A's
// commit, so the final ledger carries A's armed+installed entry and NOT B's
// consumed one.
func TestUpdateJournalInTx_CrossProcessJournalRace(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "race.db")

	dbA, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, dbA.RunMigrationsOnStartup(ctx))
	defer func() { _ = dbA.Close() }()
	repoA := NewBatchFileOperationRepository(dbA)

	destB := "/dst/race/poster.jpg"
	entryB := models.ReplacementEntry{Destination: destB, Backup: destB + ".dlbak.b", DestSeq: 3, Installed: true}
	op := seedJournalTxRow(t, repoA, models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{entryB}})

	// Second pool over the SAME file: the cross-process analogue — no shared
	// process lock, no shared destination marker.
	dbB, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	defer func() { _ = dbB.Close() }()
	repoB := NewBatchFileOperationRepository(dbB)

	destA := "/dst/race/fanart.jpg"
	aReRead := make(chan struct{})
	aProceed := make(chan struct{})
	aErr := make(chan error, 1)

	// Apply side: arm destination A. The fn gate holds the write transaction
	// open between the row re-read and the commit.
	go func() {
		aErr <- repoA.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			close(aReRead)
			<-aProceed
			gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
			if perr != nil {
				return models.GeneratedFilesJSON{}, false, perr
			}
			gf.Replacements = append(gf.Replacements, models.ReplacementEntry{
				Destination: destA, Backup: destA + ".dlbak.a", DestSeq: 1,
			})
			return gf, true, nil
		})
	}()

	<-aReRead
	bErr := make(chan error, 1)
	var bSawA bool
	// Revert side: consume destination B. BEGIN IMMEDIATE blocks until A
	// commits, so B's in-transaction re-read MUST observe A's entry.
	go func() {
		bErr <- repoB.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
			if perr != nil {
				return models.GeneratedFilesJSON{}, false, perr
			}
			kept := gf.Replacements[:0]
			for _, e := range gf.Replacements {
				if e.Destination == destA {
					bSawA = true
					kept = append(kept, e)
					continue
				}
				if e.Destination == destB {
					continue // consumed
				}
				kept = append(kept, e)
			}
			if !bSawA {
				return models.GeneratedFilesJSON{}, false, errors.New("stale snapshot: apply-arm not visible inside revert-consume transaction")
			}
			gf.Replacements = kept
			return gf, true, nil
		})
	}()
	close(aProceed) // release A so B can proceed after A's commit

	require.NoError(t, <-aErr, "apply-arm transaction")
	require.NoError(t, <-bErr, "revert-consume transaction")
	require.True(t, bSawA, "B re-read the ledger strictly after A's commit")

	// Apply side completes the flow: confirm the armed entry installed.
	require.NoError(t, repoA.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, perr := models.ParseGeneratedFiles(current.GeneratedFiles)
		if perr != nil {
			return models.GeneratedFilesJSON{}, false, perr
		}
		for i := range gf.Replacements {
			if gf.Replacements[i].Destination == destA {
				gf.Replacements[i].Installed = true
			}
		}
		return gf, true, nil
	}))

	row, err := repoB.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "one entry survives: no resurrection, no clobber")
	survivor := gf.Replacements[0]
	require.Equal(t, destA, survivor.Destination, "the apply's entry is armed on the row")
	require.True(t, survivor.Installed, "the apply's entry is confirmed installed")
	require.Equal(t, destA+".dlbak.a", survivor.Backup)
}
