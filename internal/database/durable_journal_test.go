package database

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

var errDurableJournalTest = errors.New("synthetic pragma failure")

type testDurableJournalConnection struct {
	mode                                       string
	sync                                       int
	reads, sets, discards                      int
	modeErr, errorRead, errorSet, errorDiscard error
	failRead, failSet                          int
	ignoredSet                                 bool
}

func (c *testDurableJournalConnection) journalMode(context.Context) (string, error) {
	return c.mode, c.modeErr
}
func (c *testDurableJournalConnection) synchronous(context.Context) (int, error) {
	c.reads++
	if c.reads == c.failRead {
		return 0, c.errorRead
	}
	return c.sync, nil
}
func (c *testDurableJournalConnection) setSynchronous(ctx context.Context, mode int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.sets++
	if c.sets == c.failSet {
		return c.errorSet
	}
	if !c.ignoredSet {
		c.sync = mode
	}
	return nil
}
func (c *testDurableJournalConnection) discard() error { c.discards++; return c.errorDiscard }

func TestDurableJournalPragmaBoundaries(t *testing.T) {
	for _, prior := range []int{0, 1, 2, 3} {
		c := &testDurableJournalConnection{mode: "wal", sync: prior}
		restore, err := prepareDurableJournal(context.Background(), c)
		require.NoError(t, err)
		require.GreaterOrEqual(t, c.sync, 2)
		require.NoError(t, restore())
		require.Equal(t, prior, c.sync)
		require.Zero(t, c.discards)
	}
	for _, c := range []*testDurableJournalConnection{
		{modeErr: errDurableJournalTest}, {mode: "delete"}, {mode: "wal", failRead: 1, errorRead: errDurableJournalTest}, {mode: "wal", sync: 4},
	} {
		restore, err := prepareDurableJournal(context.Background(), c)
		require.Error(t, err)
		require.Nil(t, restore)
		require.Zero(t, c.sets)
	}
	for _, c := range []*testDurableJournalConnection{
		{mode: "wal", sync: 1, failSet: 1, errorSet: errDurableJournalTest},
		{mode: "wal", sync: 1, failRead: 2, errorRead: errDurableJournalTest},
		{mode: "wal", sync: 1, ignoredSet: true},
	} {
		restore, err := prepareDurableJournal(context.Background(), c)
		require.Error(t, err)
		require.NotNil(t, restore)
		require.NoError(t, restore())
		require.Equal(t, 1, c.sync)
	}
	for _, discardErr := range []error{nil, driver.ErrBadConn, errDurableJournalTest} {
		c := &testDurableJournalConnection{mode: "wal", sync: 1, errorDiscard: discardErr}
		restore, err := prepareDurableJournal(context.Background(), c)
		require.NoError(t, err)
		c.failSet = 2
		c.errorSet = errDurableJournalTest
		require.ErrorContains(t, restore(), "connection discarded")
		require.Equal(t, 1, c.discards)
	}
	for _, kind := range []string{"readback", "read-error"} {
		c := &testDurableJournalConnection{mode: "wal", sync: 1}
		restore, err := prepareDurableJournal(context.Background(), c)
		require.NoError(t, err)
		if kind == "readback" {
			c.ignoredSet = true
		} else {
			c.failRead = 3
			c.errorRead = errDurableJournalTest
		}
		require.Error(t, restore())
		require.Equal(t, 1, c.discards)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &testDurableJournalConnection{mode: "wal", sync: 1}
	restore, err := prepareDurableJournal(ctx, c)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, restore())
	require.Equal(t, 1, c.sync)
}

func TestDurableJournalNativePinnedRestoreAndErrors(t *testing.T) {
	for _, kind := range []string{"commit", "callback", "canceled-commit", "write-fault"} {
		t.Run(kind, func(t *testing.T) {
			db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "journal.sqlite"), LogLevel: "silent"})
			require.NoError(t, err)
			defer db.Close()
			require.NoError(t, db.AutoMigrate(&models.BatchFileOperation{}, &models.Job{}))
			sqlDB, err := db.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			require.NoError(t, db.Exec("PRAGMA synchronous=1").Error)
			repo := NewBatchFileOperationRepository(db)
			op := &models.BatchFileOperation{BatchJobID: "durable", RevertStatus: models.RevertStatusApplied}
			require.NoError(t, repo.Create(context.Background(), op))
			if kind == "write-fault" {
				require.NoError(t, db.Exec(`CREATE TRIGGER fault_durable BEFORE UPDATE OF generated_files ON batch_file_operations BEGIN SELECT RAISE(ABORT,'journal write failure'); END`).Error)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err = repo.UpdateJournalInTx(WithDurableJournal(ctx), op.ID, func(*models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
				if kind == "callback" {
					return models.GeneratedFilesJSON{}, false, errDurableJournalTest
				}
				if kind == "canceled-commit" {
					cancel()
					return models.GeneratedFilesJSON{}, false, nil
				}
				return models.GeneratedFilesJSON{Roots: []string{"durable-proof"}}, true, nil
			})
			if kind == "commit" {
				require.NoError(t, err)
			} else if kind == "canceled-commit" {
				// Cancellation may race a completed COMMIT. Either result must
				// release the transaction and restore the exact pooled setting.
				if err != nil {
					require.ErrorIs(t, err, context.Canceled)
				}
			} else {
				require.Error(t, err)
			}
			var mode int
			require.NoError(t, db.Raw("PRAGMA synchronous").Scan(&mode).Error)
			require.Equal(t, 1, mode)
			conn, err := sqlDB.Conn(context.Background())
			require.NoError(t, err)
			native := nativeDurableJournalConnection{conn}
			restore, err := prepareDurableJournal(context.Background(), native)
			require.NoError(t, err)
			mode, err = native.synchronous(context.Background())
			require.NoError(t, err)
			require.Equal(t, 2, mode)
			require.NoError(t, restore())
			require.ErrorIs(t, native.discard(), driver.ErrBadConn)
			_ = conn.Close()
			require.NoError(t, sqlDB.Ping())
		})
	}
}
