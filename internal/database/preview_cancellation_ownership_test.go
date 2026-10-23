package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type previewOwnedRollbackDriver struct{ state *previewOwnedRollbackState }
type previewOwnedRollbackState struct {
	mu                         sync.Mutex
	transactionContext         context.Context
	entered, release, finished chan struct{}
	releaseOnce                sync.Once
	afterSetup, afterBegin     func()
	begins                     atomic.Int32
}
type previewOwnedRollbackConn struct {
	*sqlite3.SQLiteConn
	state *previewOwnedRollbackState
}
type previewOwnedRollbackTx struct {
	driver.Tx
	state *previewOwnedRollbackState
}

func (d previewOwnedRollbackDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite3.SQLiteDriver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &previewOwnedRollbackConn{c.(*sqlite3.SQLiteConn), d.state}, nil
}
func (c *previewOwnedRollbackConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.SQLiteConn.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	c.state.mu.Lock()
	c.state.transactionContext = ctx
	c.state.mu.Unlock()
	c.state.begins.Add(1)
	if c.state.afterBegin != nil {
		c.state.afterBegin()
	}
	return &previewOwnedRollbackTx{tx, c.state}, nil
}
func (c *previewOwnedRollbackConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.SQLiteConn.ExecContext(ctx, query, args)
	if err == nil && query == "PRAGMA busy_timeout=50" && c.state.afterSetup != nil {
		c.state.afterSetup()
	}
	return result, err
}
func (tx *previewOwnedRollbackTx) Rollback() error {
	close(tx.state.entered)
	<-tx.state.release
	defer close(tx.state.finished)
	return tx.Tx.Rollback()
}

var previewOwnedRollbackSerial atomic.Uint64

// The wrapper observes a real SQLite transaction and gates its real rollback.
// It does not synthesize SQL outcomes or try to force database/sql's narrow
// concurrent-close panic. The hosted Windows trace supplies that panic evidence.
func TestPreviewCancellationRetainsCleanupOwnershipAndQueryCancellation(t *testing.T) {
	state := &previewOwnedRollbackState{entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	release := func() { state.releaseOnce.Do(func() { close(state.release) }) }
	name := fmt.Sprintf("preview-owned-rollback-%d", previewOwnedRollbackSerial.Add(1))
	sql.Register(name, previewOwnedRollbackDriver{state})
	gdb, err := gorm.Open(sqlite.Dialector{DriverName: name, DSN: filepath.Join(t.TempDir(), "preview.db") + "?_busy_timeout=4321"}, &gorm.Config{})
	require.NoError(t, err)
	db := &DB{DB: gdb}
	t.Cleanup(func() { _ = db.Close() })
	pool, err := gdb.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	require.NoError(t, db.Exec("CREATE TABLE ownership_probe(id INTEGER PRIMARY KEY, value TEXT)").Error)
	require.NoError(t, db.Exec("INSERT INTO ownership_probe VALUES(1, 'original')").Error)
	ctx, cancel := context.WithCancel(t.Context())
	type observation struct{ insertErr, requestErr, queryErr, lifetimeErr error }
	observed := make(chan observation, 1)
	type outcome struct {
		err        error
		panicValue any
	}
	result := make(chan outcome, 1)
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		release()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("preview worker failed to join")
		}
	})
	go func() {
		var out outcome
		defer func() { out.panicValue = recover(); result <- out; close(done) }()
		out.err = db.WithPreviewTx(ctx, func(u EditUnit) error {
			tx := u.Movies.(*MovieRepository).GetDB()
			insertErr := tx.Exec("INSERT INTO ownership_probe VALUES(2, 'tentative')").Error
			cancel()
			state.mu.Lock()
			lifetimeErr := state.transactionContext.Err()
			state.mu.Unlock()
			queryErr := tx.Exec("INSERT INTO ownership_probe VALUES(3, 'canceled')").Error
			observed <- observation{insertErr, tx.Statement.Context.Err(), queryErr, lifetimeErr}
			return queryErr
		})
	}()
	var got observation
	select {
	case got = <-observed:
	case <-time.After(5 * time.Second):
		t.Fatal("preview callback did not complete")
	}
	require.NoError(t, got.insertErr)
	require.ErrorIs(t, got.requestErr, context.Canceled)
	require.ErrorIs(t, got.queryErr, context.Canceled)
	require.NoError(t, got.lifetimeErr, "request cancellation must not transfer rollback ownership to database/sql")
	select {
	case <-state.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("rollback did not start")
	}
	select {
	case <-done:
		t.Fatal("preview returned before real rollback completed")
	default:
	}
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("preview failed to finish rollback")
	}
	out := <-result
	require.Nil(t, out.panicValue)
	require.ErrorIs(t, out.err, context.Canceled)
	select {
	case <-state.finished:
	default:
		t.Fatal("real rollback did not finish")
	}
	var rows []struct {
		ID    int
		Value string
	}
	require.NoError(t, db.Table("ownership_probe").Order("id").Find(&rows).Error)
	require.Equal(t, []struct {
		ID    int
		Value string
	}{{1, "original"}}, rows)
	var busy int
	require.NoError(t, db.Raw("PRAGMA busy_timeout").Scan(&busy).Error)
	require.Equal(t, 4321, busy)
	require.NoError(t, db.Exec("INSERT INTO ownership_probe VALUES(4, 'later write')").Error)
	var ids []int
	require.NoError(t, db.Table("ownership_probe").Order("id").Pluck("id", &ids).Error)
	require.Equal(t, []int{1, 4}, ids)
}

func TestPreviewCancellationAtAdmissionBoundariesSkipsCallback(t *testing.T) {
	for _, phase := range []string{"after setup", "after begin"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			state := &previewOwnedRollbackState{entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
			close(state.release) // This test observes completion without delaying rollback.
			if phase == "after setup" {
				state.afterSetup = cancel
			} else {
				state.afterBegin = cancel
			}
			name := fmt.Sprintf("preview-admission-cancel-%d", previewOwnedRollbackSerial.Add(1))
			sql.Register(name, previewOwnedRollbackDriver{state})
			gdb, err := gorm.Open(sqlite.Dialector{DriverName: name, DSN: filepath.Join(t.TempDir(), "boundary.db") + "?_busy_timeout=4321"}, &gorm.Config{})
			require.NoError(t, err)
			db := &DB{DB: gdb}
			t.Cleanup(func() { _ = db.Close() })
			pool, err := gdb.DB()
			require.NoError(t, err)
			pool.SetMaxOpenConns(1)
			require.NoError(t, db.Exec("CREATE TABLE boundary_probe(id INTEGER PRIMARY KEY)").Error)
			err = db.WithPreviewTx(ctx, func(EditUnit) error { t.Error("canceled admission must not run callback"); return nil })
			require.ErrorIs(t, err, context.Canceled)
			if phase == "after setup" {
				require.Zero(t, state.begins.Load(), "do not start a transaction after observed cancellation")
			} else {
				require.Equal(t, int32(1), state.begins.Load())
				select {
				case <-state.finished:
				default:
					t.Fatal("admitted transaction must finish rollback before return")
				}
			}
			var busy int
			require.NoError(t, db.Raw("PRAGMA busy_timeout").Scan(&busy).Error)
			require.Equal(t, 4321, busy)
			require.NoError(t, db.Exec("INSERT INTO boundary_probe VALUES(1)").Error)
			var count int64
			require.NoError(t, db.Table("boundary_probe").Count(&count).Error)
			require.Equal(t, int64(1), count)
		})
	}
}
