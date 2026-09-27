package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrDumpInvalid marks a staged sidecar database that failed dump validation
// (missing/empty/unreadable required content, schema drift, or failed
// integrity check). Upload handlers map it to the async 'validation' error
// kind.
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
// complete, uncorrupted, current-schema dump database. Compatibility policy:
// MISSING or EMPTY required structure is rejected with a named-structure
// error; ADDITIVE structure (extra tables/columns/indexes from a newer build)
// is accepted — all read paths use explicit column lists. No migration.
//
// On success it returns the open Store; the caller MUST Close it before any
// filesystem operation on the file (swap/rename fails with open handles on
// Windows). On failure it returns an error wrapping ErrDumpInvalid and holds
// no open handle.
//
// Every probe is a single-row query, so each stage's error branch is
// reachable deterministically (direct stage calls with a cancelled context).
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

	for _, stage := range []func(context.Context, *sql.DB) error{
		validateProvenance,
		validateStructure,
		validateIndexes,
		validateNonEmpty,
		validateIntegrity,
	} {
		if err := stage(ctx, store.db); err != nil {
			return nil, err
		}
	}
	valid = true
	return store, nil
}

// queryCount runs a single-row COUNT query; every error surfaces with the
// stage-appropriate context string.
func queryCount(ctx context.Context, db *sql.DB, query string, args ...any) (int64, error) {
	var n int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// validateProvenance requires a present, non-empty dump_meta with the
// key/value columns Stats reads (a wrong-shaped dump_meta passes a row count
// while breaking every subsequent provenance read).
func validateProvenance(ctx context.Context, db *sql.DB) error {
	n, err := queryCount(ctx, db, "SELECT COUNT(*) FROM dump_meta")
	if err != nil {
		return fmt.Errorf("%w: dump_meta unreadable or missing: %v", ErrDumpInvalid, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: dump_meta carries no provenance rows", ErrDumpInvalid)
	}
	if err := validateColumns(ctx, db, "dump_meta", []string{"key", "value"}); err != nil {
		return err
	}
	return nil
}

// validateStructure checks every required table and column individually via
// single-row probes so missing structure names itself in the error. Additive
// extras pass silently.
func validateStructure(ctx context.Context, db *sql.DB) error {
	for dumpTable, schema := range tableSchema {
		table := sqliteTableName(dumpTable)
		n, err := queryCount(ctx, db,
			"SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table','view') AND name=?", table)
		if err != nil {
			return fmt.Errorf("%w: table probe: %v", ErrDumpInvalid, err)
		}
		if n == 0 {
			return fmt.Errorf("%w: missing required table %s", ErrDumpInvalid, table)
		}
		if err := validateColumns(ctx, db, table, schema.columns); err != nil {
			return err
		}
	}
	return nil
}

// validateColumns probes each required column individually (separated from
// validateStructure so its cancelled-context error path is reachable directly).
func validateColumns(ctx context.Context, db *sql.DB, table string, cols []string) error {
	for _, col := range cols {
		n, err := queryCount(ctx, db,
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, col)
		if err != nil {
			return fmt.Errorf("%w: column probe %s: %v", ErrDumpInvalid, table, err)
		}
		if n == 0 {
			return fmt.Errorf("%w: missing required column %s.%s", ErrDumpInvalid, table, col)
		}
	}
	return nil
}

// validateIndexes requires every import-created index by exact name.
func validateIndexes(ctx context.Context, db *sql.DB) error {
	for _, idx := range requiredIndexes {
		n, err := queryCount(ctx, db,
			"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", idx)
		if err != nil {
			return fmt.Errorf("%w: index probe %s: %v", ErrDumpInvalid, idx, err)
		}
		if n == 0 {
			return fmt.Errorf("%w: missing required index %s", ErrDumpInvalid, idx)
		}
	}
	return nil
}

// validateNonEmpty rejects zero-video dumps (a shell that would replace a
// working dump with nothing).
func validateNonEmpty(ctx context.Context, db *sql.DB) error {
	n, err := queryCount(ctx, db, "SELECT COUNT(*) FROM videos")
	if err != nil {
		return fmt.Errorf("%w: videos unreadable: %v", ErrDumpInvalid, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: dump contains zero videos", ErrDumpInvalid)
	}
	return nil
}

// validateIntegrity runs quick_check; the result must be exactly 'ok'.
func validateIntegrity(ctx context.Context, db *sql.DB) error {
	var report string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&report); err != nil {
		return fmt.Errorf("%w: integrity check could not run: %v", ErrDumpInvalid, err)
	}
	if strings.TrimSpace(report) != "ok" {
		return fmt.Errorf("%w: integrity check: %s", ErrDumpInvalid, report)
	}
	return nil
}
