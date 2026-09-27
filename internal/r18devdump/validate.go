package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrDumpInvalid marks a staged sidecar database that failed dump validation
// (missing required structure or failed integrity check). Upload handlers map
// it to the async 'validation' error kind.
var ErrDumpInvalid = errors.New("invalid dump sidecar")

// requiredIndexes are the exact index names Import creates; a sidecar missing
// any of them is structurally invalid for the current read paths.
var requiredIndexes = []string{
	"idx_videos_dvd_id_norm",
	"idx_video_actresses_cid",
	"idx_video_categories_cid",
	"idx_video_directors_cid",
}

// ValidateSidecar opens the staged sidecar at path and verifies it is a
// complete, uncorrupted current-schema dump database. Compatibility policy:
// MISSING required structure is rejected with a named-structure error;
// ADDITIVE structure (extra tables/columns/indexes from a newer build) is
// accepted — all read paths use explicit column lists. There is no migration.
//
// On success it returns the open Store; the caller MUST Close it before any
// filesystem operation on the file (swap/rename fails with open handles on
// Windows). On failure it returns an error wrapping ErrDumpInvalid and holds
// no open handle.
func ValidateSidecar(ctx context.Context, path string) (*Store, error) {
	store, err := OpenContext(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDumpInvalid, err)
	}
	valid := false
	defer func() {
		if !valid {
			_ = store.Close()
		}
	}()

	// 1. Metadata table must exist and be readable.
	var metaCount int64
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM dump_meta").Scan(&metaCount); err != nil {
		return nil, fmt.Errorf("%w: dump_meta unreadable: %v", ErrDumpInvalid, err)
	}

	// 2. Required tables and columns (additive extras are fine).
	for dumpTable, schema := range tableSchema {
		sqlite := sqliteTableName(dumpTable)
		cols, err := tableColumns(ctx, store.db, sqlite)
		if err != nil {
			return nil, fmt.Errorf("%w: required table %s unreadable: %v", ErrDumpInvalid, sqlite, err)
		}
		if cols == nil {
			return nil, fmt.Errorf("%w: missing required table %s", ErrDumpInvalid, sqlite)
		}
		for _, want := range schema.columns {
			if !cols[want] {
				return nil, fmt.Errorf("%w: missing required column %s.%s", ErrDumpInvalid, sqlite, want)
			}
		}
	}

	// 3. Required indexes (table_info cannot prove these).
	for _, idx := range requiredIndexes {
		if err := indexExists(ctx, store.db, idx); err != nil {
			return nil, err
		}
	}

	// 4. Integrity: quick_check must return exactly 'ok'.
	var qc string
	if err := store.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&qc); err != nil {
		return nil, fmt.Errorf("%w: quick_check failed to run: %v", ErrDumpInvalid, err)
	}
	if qc != "ok" {
		return nil, fmt.Errorf("%w: integrity check: %s", ErrDumpInvalid, qc)
	}

	valid = true
	return store, nil
}

// tableColumns returns the column name set for table, or nil when the table
// does not exist (PRAGMA table_info returns zero rows for missing tables).
func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	cols := map[string]bool{}
	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		found = true
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return cols, nil
}

func indexExists(ctx context.Context, db *sql.DB, name string) error {
	var n int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", name).Scan(&n); err != nil {
		return fmt.Errorf("%w: index probe failed: %v", ErrDumpInvalid, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: missing required index %s", ErrDumpInvalid, name)
	}
	return nil
}
