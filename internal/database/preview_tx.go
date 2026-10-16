package database

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// WithPreviewTx runs DB-only reconciliation and always rolls it back. Callers
// must keep detached results private until this returns nil. A dedicated
// connection prevents a failed rollback from returning provisional writes to
// the pool: that connection is discarded before it can serve another request.
func (db *DB) WithPreviewTx(ctx context.Context, fn func(EditUnit) error) (err error) {
	if db == nil || db.DB == nil {
		return gorm.ErrInvalidDB
	}
	if _, nested := db.Statement.ConnPool.(gorm.TxCommitter); nested {
		return gorm.ErrInvalidDB
	}
	pool, err := db.DB.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var busyTimeout int
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return err
	}
	discarded := false
	defer func() {
		if discarded {
			return
		}
		// SQLite's busy handler can outlive context cancellation. Cap only this
		// pinned connection's wait and restore its previous setting before reuse.
		restoreCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, restoreErr := conn.ExecContext(restoreCtx, fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout)); restoreErr != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			err = errors.Join(err, fmt.Errorf("restore preview connection: %w", restoreErr))
		}
	}()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=50"); err != nil {
		return err
	}
	isolated := db.WithContext(ctx).Session(&gorm.Session{NewDB: true})
	isolated.Statement.ConnPool = conn
	tx := isolated.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if rollbackErr := tx.Rollback().Error; rollbackErr != nil {
			// database/sql does not discard a connection for an arbitrary driver
			// rollback error. Raw returning ErrBadConn closes it instead of pooling
			// it, including when SQLite still has an open failed transaction.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			discarded = true
			err = errors.Join(err, fmt.Errorf("rollback movie preview: %w", rollbackErr))
		}
	}()
	txDB := &DB{DB: tx, dsn: db.dsn, fs: db.fs}
	err = fn(EditUnit{Movies: NewMovieRepository(txDB), Actresses: NewActressRepository(txDB), Jobs: NewJobRepository(txDB)})
	return errors.Join(err, ctx.Err())
}
