package r18devdump

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ImportOptions carries provenance metadata stored alongside the imported rows.
type ImportOptions struct {
	SourceURL  string
	SourceDate string
	BeforeSwap func() error
	AfterSwap  func()
}

// ImportResult describes a completed import.
type ImportResult struct {
	Rows int64
	Path string
}

// importBatchSize is the number of rows per multi-row INSERT. The widest table
// (derived_video) has 21 columns, so 40 rows = 840 bound parameters — safely
// under SQLite's default SQLITE_MAX_VARIABLE_NUMBER (999 on older builds; the
// modern default is far higher).
const importBatchSize = 40

// tableSchema maps each dump table to its CREATE TABLE statement + the column
// order its INSERTs use. The importer dispatches incoming DumpRows by table
// name; only tables present here are stored.
var tableSchema = map[string]struct {
	create  string
	columns []string
}{
	derivedVideoTable: {
		create: `CREATE TABLE videos (
			content_id        TEXT NOT NULL PRIMARY KEY,
			dvd_id            TEXT,
			dvd_id_norm       TEXT,
			title_en          TEXT,
			title_ja          TEXT,
			comment_en        TEXT,
			comment_ja        TEXT,
			runtime_mins      INTEGER,
			release_date      TEXT,
			sample_url        TEXT,
			maker_id          TEXT,
			label_id          TEXT,
			series_id         TEXT,
			jacket_full_url   TEXT,
			jacket_thumb_url  TEXT,
			gallery_full_first  TEXT,
			gallery_full_last   TEXT,
			gallery_thumb_first TEXT,
			gallery_thumb_last  TEXT,
			site_id           TEXT,
			service_code      TEXT
		)`,
		columns: []string{"content_id", "dvd_id", "dvd_id_norm", "title_en", "title_ja", "comment_en", "comment_ja", "runtime_mins", "release_date", "sample_url", "maker_id", "label_id", "series_id", "jacket_full_url", "jacket_thumb_url", "gallery_full_first", "gallery_full_last", "gallery_thumb_first", "gallery_thumb_last", "site_id", "service_code"},
	},
	"derived_actress": {
		create: `CREATE TABLE actresses (
			id           TEXT NOT NULL PRIMARY KEY,
			name_romaji  TEXT,
			image_url    TEXT,
			name_kanji   TEXT,
			name_kana    TEXT
		)`,
		columns: []string{"id", "name_romaji", "image_url", "name_kanji", "name_kana"},
	},
	"derived_maker": {
		create: `CREATE TABLE makers (
			id      TEXT NOT NULL PRIMARY KEY,
			name_en TEXT,
			name_ja TEXT
		)`,
		columns: []string{"id", "name_en", "name_ja"},
	},
	"derived_label": {
		create: `CREATE TABLE labels (
			id      TEXT NOT NULL PRIMARY KEY,
			name_en TEXT,
			name_ja TEXT
		)`,
		columns: []string{"id", "name_en", "name_ja"},
	},
	"derived_series": {
		create: `CREATE TABLE series (
			id      TEXT NOT NULL PRIMARY KEY,
			name_en TEXT,
			name_ja TEXT
		)`,
		columns: []string{"id", "name_en", "name_ja"},
	},
	"derived_director": {
		create: `CREATE TABLE directors (
			id          TEXT NOT NULL PRIMARY KEY,
			name_kanji  TEXT,
			name_kana   TEXT,
			name_romaji TEXT
		)`,
		columns: []string{"id", "name_kanji", "name_kana", "name_romaji"},
	},
	"derived_category": {
		create: `CREATE TABLE categories (
			id      TEXT NOT NULL PRIMARY KEY,
			name_en TEXT,
			name_ja TEXT
		)`,
		columns: []string{"id", "name_en", "name_ja"},
	},
	"derived_video_actress": {
		create: `CREATE TABLE video_actresses (
			content_id  TEXT NOT NULL,
			actress_id  TEXT NOT NULL,
			ordinality  INTEGER,
			release_date TEXT,
			PRIMARY KEY (content_id, actress_id)
		)`,
		columns: []string{"content_id", "actress_id", "ordinality", "release_date"},
	},
	"derived_video_category": {
		create: `CREATE TABLE video_categories (
			content_id   TEXT NOT NULL,
			category_id  TEXT NOT NULL,
			release_date TEXT,
			PRIMARY KEY (content_id, category_id)
		)`,
		columns: []string{"content_id", "category_id", "release_date"},
	},
	"derived_video_director": {
		create: `CREATE TABLE video_directors (
			content_id TEXT NOT NULL,
			director_id TEXT NOT NULL,
			PRIMARY KEY (content_id, director_id)
		)`,
		columns: []string{"content_id", "director_id"},
	},
	"source_dmm_trailer": {
		create: `CREATE TABLE trailers (
			content_id TEXT NOT NULL PRIMARY KEY,
			url        TEXT
		)`,
		columns: []string{"content_id", "url"},
	},
}

// sqliteTableName maps a dump table name to its SQLite destination.
func sqliteTableName(dumpName string) string {
	switch dumpName {
	case derivedVideoTable:
		return "videos"
	case "derived_actress":
		return "actresses"
	case "derived_maker":
		return "makers"
	case "derived_label":
		return "labels"
	case "derived_series":
		return "series"
	case "derived_director":
		return "directors"
	case "derived_category":
		return "categories"
	case "derived_video_actress":
		return "video_actresses"
	case "derived_video_category":
		return "video_categories"
	case "derived_video_director":
		return "video_directors"
	case "source_dmm_trailer":
		return "trailers"
	default:
		return dumpName
	}
}

// Import streams a pg_dump from r, builds a fresh sidecar database at path
// (via a .tmp file + atomic rename), and returns the number of video rows
// imported. All dump tables are stored. The database is written under a
// single transaction for speed.
//
// If an error occurs, any partial .tmp file is removed and path is left
// untouched, so a failed import never corrupts a previously good dump.
func Import(ctx context.Context, r io.Reader, path string, opts ImportOptions) (ImportResult, error) {
	if path == "" {
		return ImportResult{}, fmt.Errorf("dump db path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return ImportResult{}, fmt.Errorf("create dump dir: %w", err)
	}
	tmpPath := path + ".tmp"
	_ = os.Remove(tmpPath)
	_ = os.Remove(tmpPath + "-wal")
	_ = os.Remove(tmpPath + "-shm")

	db, err := sql.Open("sqlite3", tmpPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return ImportResult{}, fmt.Errorf("open temp dump db: %w", err)
	}
	committed := false
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
		if !committed {
			_ = os.Remove(tmpPath)
		}
		_ = os.Remove(tmpPath + "-wal")
		_ = os.Remove(tmpPath + "-shm")
	}()

	// Create all tables + indexes in one Exec.
	schema := "CREATE TABLE dump_meta (key TEXT PRIMARY KEY, value TEXT);"
	for _, s := range tableSchema {
		schema += s.create + ";"
	}
	schema += `CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm);
		CREATE INDEX idx_video_actresses_cid ON video_actresses(content_id);
		CREATE INDEX idx_video_categories_cid ON video_categories(content_id);
		CREATE INDEX idx_video_directors_cid ON video_directors(content_id);`
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return ImportResult{}, fmt.Errorf("create schema: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, fmt.Errorf("begin tx: %w", err)
	}

	// Per-table batch accumulators.
	batches := make(map[string][]DumpRow)
	var totalVideos int64

	flush := func(table string) error {
		batch := batches[table]
		if len(batch) == 0 {
			return nil
		}
		n, err := insertBatch(ctx, tx, table, batch)
		if table == derivedVideoTable {
			totalVideos += n
		}
		batches[table] = batch[:0]
		return err
	}
	flushAll := func() error {
		// Sort table names so the final residual-batch flush is deterministic,
		// aiding reproducible debugging.
		tables := make([]string, 0, len(batches))
		for table := range batches {
			tables = append(tables, table)
		}
		sort.Strings(tables)
		for _, table := range tables {
			if err := flush(table); err != nil {
				return err
			}
		}
		return nil
	}

	emit := func(row DumpRow) error {
		// Honor cancellation between batch flushes so a large network-streamed
		// dump can be aborted without waiting for the next tx.ExecContext.
		if err := ctx.Err(); err != nil {
			return err
		}
		schema, ok := tableSchema[row.Table]
		if !ok {
			return nil // skip tables we don't store
		}
		// Map dump column positions to our stored column order.
		mapped := mapDumpRow(row, schema.columns)
		batches[row.Table] = append(batches[row.Table], DumpRow{Table: row.Table, Values: mapped})
		if len(batches[row.Table]) >= importBatchSize {
			return flush(row.Table)
		}
		return nil
	}

	if err := ParseDump(r, emit); err != nil {
		_ = tx.Rollback()
		return ImportResult{}, fmt.Errorf("parse dump: %w", err)
	}
	if err := flushAll(); err != nil {
		_ = tx.Rollback()
		return ImportResult{}, fmt.Errorf("insert rows: %w", err)
	}

	if err := writeMeta(ctx, tx, opts); err != nil {
		_ = tx.Rollback()
		return ImportResult{}, fmt.Errorf("write dump_meta: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ImportResult{}, fmt.Errorf("commit: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return ImportResult{}, fmt.Errorf("wal checkpoint: %w", err)
	}

	// Close the write connection before the rename. On POSIX the open handle
	// would follow the inode across rename, but Windows refuses to rename a
	// file that still has an open handle ("The process cannot access the file
	// because it is being used by another process"). Closing here also
	// ensures the WAL is fully checkpointed and the -wal/-shm sidecars are
	// cleaned up before the rename on every platform.
	if err := db.Close(); err != nil {
		return ImportResult{}, fmt.Errorf("close temp dump db: %w", err)
	}
	closed = true
	if opts.BeforeSwap != nil {
		if err := opts.BeforeSwap(); err != nil {
			return ImportResult{}, fmt.Errorf("prepare dump swap: %w", err)
		}
	}
	if opts.AfterSwap != nil {
		defer opts.AfterSwap()
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return ImportResult{}, fmt.Errorf("rename tmp db: %w", err)
	}
	committed = true
	return ImportResult{Rows: totalVideos, Path: path}, nil
}

// mapDumpRow reorders/transforms a DumpRow's values to match the stored column
// order for its table. For derived_video, it computes dvd_id_norm and converts
// NULLs. For other tables, it maps by column name.
func mapDumpRow(row DumpRow, storedCols []string) []string {
	// Build a column->value map from the dump row, and track which columns are
	// present in the COPY header. A column absent from the header is a genuine
	// NULL (bind \N so nullableValue emits SQL nil); a column present but
	// empty is a real empty string and must stay "" (distinct from NULL).
	colMap := make(map[string]string, len(row.Columns))
	present := make(map[string]bool, len(row.Columns))
	for i, col := range row.Columns {
		if i < len(row.Values) {
			colMap[col] = row.Values[i]
			present[col] = true
		}
	}

	mapped := make([]string, len(storedCols))
	for i, col := range storedCols {
		if present[col] {
			// The dump's NULL marker has already been converted to nullSentinel
			// by ParseDump; carry it through so nullableValue binds SQL nil. A
			// genuine empty string stays "" (distinct from NULL).
			mapped[i] = colMap[col]
		} else {
			// Column absent from this COPY block's header — there is no value
			// to store, so bind NULL (not an empty string).
			mapped[i] = nullSentinel
		}
	}

	// derived_video carries one computed column (dvd_id_norm) that is not
	// present in the dump's COPY block, so it is filled here. The general loop
	// above already NULL-normalizes every real column (\N -> ""), including
	// runtime_mins; do NOT re-read colMap["runtime_mins"] here — colMap holds
	// the RAW value (still \N for NULLs) and would re-inject the literal into
	// an INTEGER column, breaking later NullInt64 scans.
	if row.Table == derivedVideoTable {
		for i, col := range storedCols {
			if col == "dvd_id_norm" {
				did := colMap["dvd_id"]
				if did == nullSentinel || did == "" {
					mapped[i] = ""
				} else {
					mapped[i] = normalizeDVDID(did)
				}
			}
		}
	}
	return mapped
}

func insertBatch(ctx context.Context, tx *sql.Tx, dumpTable string, batch []DumpRow) (int64, error) {
	schema := tableSchema[dumpTable]
	cols := schema.columns
	tableName := sqliteTableName(dumpTable)

	var b []byte
	b = append(b, "INSERT OR IGNORE INTO "...)
	b = append(b, tableName...)
	b = append(b, " ("...)
	for i, c := range cols {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, c...)
	}
	b = append(b, ") VALUES "...)
	args := make([]any, 0, len(batch)*len(cols))
	for i, row := range batch {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '(')
		for j := range cols {
			if j > 0 {
				b = append(b, ',')
			}
			b = append(b, '?')
		}
		b = append(b, ')')
		for _, v := range row.Values {
			args = append(args, nullableValue(v))
		}
	}

	res, err := tx.ExecContext(ctx, string(b), args...)
	if err != nil {
		return 0, fmt.Errorf("exec batch (%s, %d rows): %w", dumpTable, len(batch), err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// nullableValue converts a dump value to its SQLite binding. ParseDump has
// already converted the dump's NULL marker to nullSentinel, which binds as SQL
// nil so INTEGER columns (runtime_mins, ordinality) stay clean and TEXT
// columns round-trip as NULL on read (letting lookups scan them with sql.Null*
// without error). A genuine empty string is preserved as "" — it is distinct
// from NULL in the source dump and must not be coerced.
func nullableValue(val string) any {
	if val == nullSentinel {
		return nil
	}
	return val
}

func writeMeta(ctx context.Context, tx *sql.Tx, opts ImportOptions) error {
	pairs := []struct{ k, v string }{
		{"source_url", opts.SourceURL},
		{"source_date", opts.SourceDate},
		{"imported_at", time.Now().UTC().Format(time.RFC3339)},
	}
	for _, p := range pairs {
		if _, err := tx.ExecContext(ctx,
			"INSERT OR REPLACE INTO dump_meta (key, value) VALUES (?, ?)",
			p.k, p.v,
		); err != nil {
			return err
		}
	}
	return nil
}
