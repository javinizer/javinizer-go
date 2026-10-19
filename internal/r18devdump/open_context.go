package r18devdump

import (
	"context"
	"database/sql"
	"fmt"
)

// OpenContext opens a read-only dump sidecar and uses ctx for its initial
// readiness check. Cancellation is cooperative with the database driver; it
// cannot interrupt a filesystem operation that does not honor context.
// Once opened, the Store remains usable until Close, independently of ctx.
func OpenContext(ctx context.Context, path string) (*Store, error) {
	return openContextWithOpenDB(ctx, path, sql.Open)
}

func openContextWithOpenDB(ctx context.Context, path string, openDB func(string, string) (*sql.DB, error)) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", path)
	db, err := openDB("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open dump db: %w", err)
	}
	err = db.PingContext(ctx)
	if err == nil {
		// Some drivers return success even if cancellation happened during open.
		err = ctx.Err()
	}
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping dump db: %w", err)
	}
	return &Store{db: db, path: path}, nil
}
