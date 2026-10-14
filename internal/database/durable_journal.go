package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

type durableJournalContextKey struct{}

// WithDurableJournal opts a filesystem recovery proof into a FULL synchronous
// WAL commit on the transaction's pinned connection. It does not change the
// pool's defaults. Currently only the copy-first PrimaryUndo journal opts in.
func WithDurableJournal(ctx context.Context) context.Context {
	return context.WithValue(ctx, durableJournalContextKey{}, true)
}

type durableJournalConnection interface {
	journalMode(context.Context) (string, error)
	synchronous(context.Context) (int, error)
	setSynchronous(context.Context, int) error
	discard() error
}
type nativeDurableJournalConnection struct{ conn *sql.Conn }

func (c nativeDurableJournalConnection) journalMode(ctx context.Context) (string, error) {
	var mode string
	err := c.conn.QueryRowContext(ctx, "PRAGMA main.journal_mode").Scan(&mode)
	return mode, err
}
func (c nativeDurableJournalConnection) synchronous(ctx context.Context) (int, error) {
	var mode int
	err := c.conn.QueryRowContext(ctx, "PRAGMA main.synchronous").Scan(&mode)
	return mode, err
}
func (c nativeDurableJournalConnection) setSynchronous(ctx context.Context, mode int) error {
	_, err := c.conn.ExecContext(ctx, fmt.Sprintf("PRAGMA main.synchronous = %d", mode))
	return err
}
func (c nativeDurableJournalConnection) discard() error {
	return c.conn.Raw(func(any) error { return driver.ErrBadConn })
}

// The caller installs restore BEFORE BEGIN; its transaction rollback defer must
// run first. Restoration uses a fresh bounded context even after cancellation.
// A connection whose previous setting cannot be restored is never re-pooled.
func prepareDurableJournal(ctx context.Context, c durableJournalConnection) (restore func() error, err error) {
	mode, err := c.journalMode(ctx)
	if err != nil {
		return nil, err
	}
	if mode != "wal" {
		return nil, fmt.Errorf("durable recovery journal requires SQLite WAL mode, found %q", mode)
	}
	previous, err := c.synchronous(ctx)
	if err != nil {
		return nil, err
	}
	if previous < 0 || previous > 3 {
		return nil, fmt.Errorf("unknown SQLite synchronous setting %d", previous)
	}
	restore = func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		restoreErr := c.setSynchronous(cleanup, previous)
		if restoreErr == nil {
			actual, readErr := c.synchronous(cleanup)
			restoreErr = readErr
			if readErr == nil && actual != previous {
				restoreErr = fmt.Errorf("restored SQLite synchronous setting %d, expected %d", actual, previous)
			}
		}
		if restoreErr != nil {
			discardErr := c.discard()
			if errors.Is(discardErr, driver.ErrBadConn) {
				discardErr = nil
			}
			return errors.Join(fmt.Errorf("restore journal synchronous mode (connection discarded): %w", restoreErr), discardErr)
		}
		return nil
	}
	target := 2 // FULL; preserve an already stronger EXTRA setting.
	if previous > target {
		target = previous
	}
	if err := c.setSynchronous(ctx, target); err != nil {
		return restore, err
	}
	actual, err := c.synchronous(ctx)
	if err != nil {
		return restore, err
	}
	if actual != target {
		return restore, fmt.Errorf("durable journal synchronous readback %d, expected %d", actual, target)
	}
	return restore, nil
}
