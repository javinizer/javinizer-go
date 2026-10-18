package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func previewConnectionPreviewDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(&Config{DSN: filepath.Join(t.TempDir(), "preview.db") + "?_busy_timeout=4321", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Exec("CREATE TABLE previewConnection_probe(id INTEGER PRIMARY KEY,value TEXT)").Error)
	return db
}
func previewConnectionBusy(t *testing.T, conn *sql.Conn) int {
	t.Helper()
	var n int
	require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&n))
	return n
}
func TestPreviewRestoresOnlyItsConnection(t *testing.T) {
	db := previewConnectionPreviewDB(t)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(2)
	ordinary, err := pool.Conn(t.Context())
	require.NoError(t, err)
	defer ordinary.Close()
	require.Equal(t, 4321, previewConnectionBusy(t, ordinary))
	require.NoError(t, db.WithPreviewTx(t.Context(), func(u EditUnit) error {
		tx := u.Movies.(*MovieRepository).GetDB()
		var local int
		require.NoError(t, tx.Raw("PRAGMA busy_timeout").Scan(&local).Error)
		require.Equal(t, 50, local)
		require.Equal(t, 4321, previewConnectionBusy(t, ordinary), "another checked-out connection must retain its setting")
		return tx.Exec("INSERT INTO previewConnection_probe VALUES(1,'tentative')").Error
	}))
	restored, err := pool.Conn(t.Context())
	require.NoError(t, err)
	require.Equal(t, 4321, previewConnectionBusy(t, restored))
	require.NoError(t, restored.Close())
	_, err = ordinary.ExecContext(t.Context(), "INSERT INTO previewConnection_probe VALUES(2,'ordinary')")
	require.NoError(t, err)
	var ids []int
	require.NoError(t, db.Table("previewConnection_probe").Pluck("id", &ids).Error)
	require.Equal(t, []int{2}, ids)
}
func TestPreviewRestoreDenialDiscardsConnection(t *testing.T) {
	db := previewConnectionPreviewDB(t)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(t.Context())
	require.NoError(t, err)
	require.NoError(t, conn.Raw(func(raw any) error {
		raw.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, a, b, _ string) int {
			if op == sqlite3.SQLITE_PRAGMA && a == "busy_timeout" && b == "4321" {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}))
	require.NoError(t, conn.Close())
	err = db.WithPreviewTx(t.Context(), func(u EditUnit) error {
		return u.Movies.(*MovieRepository).GetDB().Exec("INSERT INTO previewConnection_probe VALUES(1,'tentative')").Error
	})
	require.ErrorContains(t, err, "restore preview connection")
	require.NoError(t, db.Exec("INSERT INTO previewConnection_probe VALUES(2,'ordinary')").Error)
	fresh, err := pool.Conn(t.Context())
	require.NoError(t, err)
	require.Equal(t, 4321, previewConnectionBusy(t, fresh))
	require.NoError(t, fresh.Close())
	var ids []int
	require.NoError(t, db.Table("previewConnection_probe").Pluck("id", &ids).Error)
	require.Equal(t, []int{2}, ids)
}
func TestCancelledPreviewCannotOutwaitBusyHandlerOrChangeOrdinaryWrite(t *testing.T) {
	db := previewConnectionPreviewDB(t)
	require.NoError(t, db.Exec("INSERT INTO previewConnection_probe VALUES(1,'original')").Error)
	writer := db.Begin()
	require.NoError(t, writer.Error)
	defer writer.Rollback()
	require.NoError(t, writer.Exec("UPDATE previewConnection_probe SET value='ordinary writer' WHERE id=1").Error)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := db.WithPreviewTx(ctx, func(u EditUnit) error {
		return u.Movies.(*MovieRepository).GetDB().Exec("UPDATE previewConnection_probe SET value='preview' WHERE id=1").Error
	})
	require.Error(t, err)
	require.Less(t, time.Since(started), 500*time.Millisecond)
	require.NoError(t, writer.Commit().Error)
	var value string
	require.NoError(t, db.Table("previewConnection_probe").Select("value").Where("id=1").Scan(&value).Error)
	require.Equal(t, "ordinary writer", value)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	conn, err := pool.Conn(t.Context())
	require.NoError(t, err)
	require.Equal(t, 4321, previewConnectionBusy(t, conn))
	require.NoError(t, conn.Close())
}

type previewConnectionSetupFaultDriver struct{ armed *atomic.Bool }
type previewConnectionSetupFaultConn struct {
	*sqlite3.SQLiteConn
	armed *atomic.Bool
}

func (d previewConnectionSetupFaultDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite3.SQLiteDriver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &previewConnectionSetupFaultConn{c.(*sqlite3.SQLiteConn), d.armed}, nil
}
func (c *previewConnectionSetupFaultConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.SQLiteConn.ExecContext(ctx, q, args)
	if err == nil && q == "PRAGMA busy_timeout=50" && c.armed.CompareAndSwap(true, false) {
		return nil, errors.New("injected post-apply setup failure")
	}
	return result, err
}

var previewConnectionSetupFaultSerial atomic.Uint64

func TestPreviewPostApplySetupFailureDoesNotPoolModifiedPragma(t *testing.T) {
	var armed atomic.Bool
	name := fmt.Sprintf("previewConnection-setup-fault-%d", previewConnectionSetupFaultSerial.Add(1))
	sql.Register(name, previewConnectionSetupFaultDriver{&armed})
	gdb, err := gorm.Open(sqlite.Dialector{DriverName: name, DSN: filepath.Join(t.TempDir(), "setup.db") + "?_busy_timeout=4321"}, &gorm.Config{})
	require.NoError(t, err)
	db := &DB{DB: gdb}
	t.Cleanup(func() { _ = db.Close() })
	pool, err := gdb.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	armed.Store(true)
	err = db.WithPreviewTx(t.Context(), func(EditUnit) error { t.Fatal("callback must not run"); return nil })
	require.ErrorContains(t, err, "injected post-apply setup failure")
	conn, err := pool.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	require.Equal(t, 4321, previewConnectionBusy(t, conn), "even an ambiguous setup outcome must not leak its changed PRAGMA")
}
