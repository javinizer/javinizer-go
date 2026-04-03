package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"unicode"

	"github.com/pressly/goose/v3"
)

const canonicalPositiveDMMIndexName = "idx_actresses_dmm_id_positive"

//go:embed *.sql
var sqlMigrations embed.FS

// Filesystem returns embedded SQL migration files.
func Filesystem() fs.FS {
	return sqlMigrations
}

// GoMigrations returns programmatic migrations that are bundled into the binary.
func GoMigrations() []*goose.Migration {
	return []*goose.Migration{
		goose.NewGoMigration(2, &goose.GoFunc{RunDB: migrateActressesDMMIDIndexUp}, nil),
		goose.NewGoMigration(3, &goose.GoFunc{RunDB: migrateSchemaCompatibilityUp}, nil),
	}
}

type actressIndexMeta struct {
	Name                   string
	Unique                 int
	Partial                int
	Origin                 string
	SQL                    string
	Columns                []indexColumnSpec
	HasUnsupportedKeyParts bool
}

type indexColumnSpec struct {
	Name      string
	Collation string
	Desc      bool
}

type columnSpec struct {
	Name       string
	Definition string
}

func migrateActressesDMMIDIndexUp(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open sqlite connection for migration: %w", err)
	}
	defer func() { _ = conn.Close() }()

	exists, err := actressesTableExists(ctx, conn)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	dupValue, dupCount, err := findDuplicatePositiveDMMID(ctx, conn)
	if err != nil {
		return err
	}
	if dupCount > 1 {
		return fmt.Errorf("cannot migrate actresses.dmm_id: duplicate positive dmm_id=%d (count=%d)", dupValue, dupCount)
	}

	indexMeta, err := loadActressIndexes(ctx, conn)
	if err != nil {
		return err
	}

	requiresTableRebuild := false
	indexesToDrop := make([]string, 0)
	preservedIndexSQL := make([]string, 0)
	hasCanonicalPositiveDMMIndex := false

	for _, idx := range indexMeta {
		if isCanonicalPositiveDMMIndex(idx) {
			hasCanonicalPositiveDMMIndex = true
			continue
		}

		if isUniqueSingleColumnDMMIndex(idx) {
			if strings.EqualFold(strings.TrimSpace(idx.Origin), "c") {
				indexesToDrop = append(indexesToDrop, idx.Name)
			} else {
				// SQLite constraint-owned index (origin='u') cannot be dropped.
				requiresTableRebuild = true
			}
			continue
		}

		if idx.SQL == "" {
			continue
		}
		if strings.EqualFold(idx.Name, canonicalPositiveDMMIndexName) || strings.EqualFold(idx.Name, "idx_actresses_japanese_name") {
			continue
		}
		preservedIndexSQL = append(preservedIndexSQL, idx.SQL)
	}

	if requiresTableRebuild {
		if err := rebuildActressesTable(ctx, conn, preservedIndexSQL); err != nil {
			return err
		}
		return nil
	}

	for _, indexName := range indexesToDrop {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteSQLiteIdentifier(indexName))); err != nil {
			return fmt.Errorf("drop legacy dmm_id index %q: %w", indexName, err)
		}
	}

	if _, err := conn.ExecContext(ctx, "UPDATE actresses SET dmm_id = 0 WHERE dmm_id < 0"); err != nil {
		return fmt.Errorf("normalize negative dmm_id values: %w", err)
	}

	if _, err := conn.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_actresses_japanese_name ON actresses(japanese_name)"); err != nil {
		return fmt.Errorf("ensure idx_actresses_japanese_name: %w", err)
	}
	if !hasCanonicalPositiveDMMIndex {
		if _, err := conn.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_actresses_dmm_id_positive ON actresses(dmm_id) WHERE dmm_id > 0"); err != nil {
			return fmt.Errorf("ensure positive dmm_id unique index: %w", err)
		}
	}

	return nil
}

func migrateSchemaCompatibilityUp(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open sqlite connection for compatibility migration: %w", err)
	}
	defer func() { _ = conn.Close() }()

	createTableStatements := []string{
		`CREATE TABLE IF NOT EXISTS movies (
			content_id TEXT PRIMARY KEY,
			id TEXT,
			display_name TEXT,
			title TEXT,
			original_title TEXT,
			description TEXT,
			release_date DATETIME,
			release_year INTEGER,
			runtime INTEGER,
			director TEXT,
			maker TEXT,
			label TEXT,
			series TEXT,
			rating_score REAL,
			rating_votes INTEGER,
			poster_url TEXT,
			cover_url TEXT,
			cropped_poster_url TEXT,
			should_crop_poster NUMERIC,
			trailer_url TEXT,
			original_file_name TEXT,
			screenshots TEXT,
			source_name TEXT,
			source_url TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS actresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dmm_id INTEGER,
			first_name TEXT,
			last_name TEXT,
			japanese_name TEXT,
			thumb_url TEXT,
			aliases TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS genres (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS genre_replacements (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			original TEXT NOT NULL,
			replacement TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS actress_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			alias_name TEXT NOT NULL,
			canonical_name TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS movie_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			movie_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			movie_id TEXT,
			operation TEXT,
			original_path TEXT,
			new_path TEXT,
			status TEXT,
			error_message TEXT,
			metadata JSON,
			dry_run NUMERIC,
			created_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS content_id_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			search_id TEXT NOT NULL,
			content_id TEXT NOT NULL,
			source TEXT NOT NULL,
			created_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS movie_translations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			movie_id TEXT,
			language TEXT,
			title TEXT,
			original_title TEXT,
			description TEXT,
			director TEXT,
			maker TEXT,
			label TEXT,
			series TEXT,
			source_name TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			CONSTRAINT fk_movies_translations FOREIGN KEY (movie_id) REFERENCES movies(content_id)
		)`,
		`CREATE TABLE IF NOT EXISTS movie_actresses (
			movie_content_id TEXT,
			actress_id INTEGER,
			PRIMARY KEY (movie_content_id, actress_id),
			CONSTRAINT fk_movie_actresses_movie FOREIGN KEY (movie_content_id) REFERENCES movies(content_id),
			CONSTRAINT fk_movie_actresses_actress FOREIGN KEY (actress_id) REFERENCES actresses(id)
		)`,
		`CREATE TABLE IF NOT EXISTS movie_genres (
			movie_content_id TEXT,
			genre_id INTEGER,
			PRIMARY KEY (movie_content_id, genre_id),
			CONSTRAINT fk_movie_genres_movie FOREIGN KEY (movie_content_id) REFERENCES movies(content_id),
			CONSTRAINT fk_movie_genres_genre FOREIGN KEY (genre_id) REFERENCES genres(id)
		)`,
	}
	for _, stmt := range createTableStatements {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure base table: %w", err)
		}
	}

	tableColumns := map[string][]columnSpec{
		"movies": {
			{Name: "content_id", Definition: "TEXT"},
			{Name: "id", Definition: "TEXT"},
			{Name: "title", Definition: "TEXT"},
			{Name: "display_name", Definition: "TEXT"},
			{Name: "original_title", Definition: "TEXT"},
			{Name: "description", Definition: "TEXT"},
			{Name: "release_date", Definition: "DATETIME"},
			{Name: "release_year", Definition: "INTEGER"},
			{Name: "runtime", Definition: "INTEGER"},
			{Name: "director", Definition: "TEXT"},
			{Name: "maker", Definition: "TEXT"},
			{Name: "label", Definition: "TEXT"},
			{Name: "series", Definition: "TEXT"},
			{Name: "rating_score", Definition: "REAL"},
			{Name: "rating_votes", Definition: "INTEGER"},
			{Name: "poster_url", Definition: "TEXT"},
			{Name: "cover_url", Definition: "TEXT"},
			{Name: "cropped_poster_url", Definition: "TEXT"},
			{Name: "should_crop_poster", Definition: "NUMERIC"},
			{Name: "trailer_url", Definition: "TEXT"},
			{Name: "original_file_name", Definition: "TEXT"},
			{Name: "screenshots", Definition: "TEXT"},
			{Name: "source_name", Definition: "TEXT"},
			{Name: "source_url", Definition: "TEXT"},
			{Name: "created_at", Definition: "DATETIME"},
			{Name: "updated_at", Definition: "DATETIME"},
		},
		"actresses": {
			{Name: "dmm_id", Definition: "INTEGER"},
			{Name: "first_name", Definition: "TEXT"},
			{Name: "last_name", Definition: "TEXT"},
			{Name: "japanese_name", Definition: "TEXT"},
			{Name: "thumb_url", Definition: "TEXT"},
			{Name: "aliases", Definition: "TEXT"},
			{Name: "created_at", Definition: "DATETIME"},
			{Name: "updated_at", Definition: "DATETIME"},
		},
		"genres": {
			{Name: "name", Definition: "TEXT"},
		},
		"genre_replacements": {
			{Name: "original", Definition: "TEXT"},
			{Name: "replacement", Definition: "TEXT"},
			{Name: "created_at", Definition: "DATETIME"},
			{Name: "updated_at", Definition: "DATETIME"},
		},
		"actress_aliases": {
			{Name: "alias_name", Definition: "TEXT"},
			{Name: "canonical_name", Definition: "TEXT"},
			{Name: "created_at", Definition: "DATETIME"},
			{Name: "updated_at", Definition: "DATETIME"},
		},
		"movie_tags": {
			{Name: "movie_id", Definition: "TEXT"},
			{Name: "tag", Definition: "TEXT"},
			{Name: "created_at", Definition: "DATETIME"},
			{Name: "updated_at", Definition: "DATETIME"},
		},
		"history": {
			{Name: "movie_id", Definition: "TEXT"},
			{Name: "operation", Definition: "TEXT"},
			{Name: "original_path", Definition: "TEXT"},
			{Name: "new_path", Definition: "TEXT"},
			{Name: "status", Definition: "TEXT"},
			{Name: "error_message", Definition: "TEXT"},
			{Name: "metadata", Definition: "JSON"},
			{Name: "dry_run", Definition: "NUMERIC"},
			{Name: "created_at", Definition: "DATETIME"},
		},
		"content_id_mappings": {
			{Name: "search_id", Definition: "TEXT"},
			{Name: "content_id", Definition: "TEXT"},
			{Name: "source", Definition: "TEXT"},
			{Name: "created_at", Definition: "DATETIME"},
		},
		"movie_translations": {
			{Name: "movie_id", Definition: "TEXT"},
			{Name: "language", Definition: "TEXT"},
			{Name: "title", Definition: "TEXT"},
			{Name: "original_title", Definition: "TEXT"},
			{Name: "description", Definition: "TEXT"},
			{Name: "director", Definition: "TEXT"},
			{Name: "maker", Definition: "TEXT"},
			{Name: "label", Definition: "TEXT"},
			{Name: "series", Definition: "TEXT"},
			{Name: "source_name", Definition: "TEXT"},
			{Name: "settings_hash", Definition: "VARCHAR(16)"},
			{Name: "created_at", Definition: "DATETIME"},
			{Name: "updated_at", Definition: "DATETIME"},
		},
	}
	for tableName, specs := range tableColumns {
		if err := ensureTableColumns(ctx, conn, tableName, specs); err != nil {
			return err
		}
	}
	if err := reconcileLegacyMoviesContentID(ctx, conn); err != nil {
		return err
	}

	indexStatements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_movies_content_id ON movies(content_id)`,
		`CREATE INDEX IF NOT EXISTS idx_movies_id ON movies(id)`,
		`CREATE INDEX IF NOT EXISTS idx_actresses_japanese_name ON actresses(japanese_name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_genres_name ON genres(name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_genre_replacements_original ON genre_replacements(original)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_actress_aliases_alias_name ON actress_aliases(alias_name)`,
		`CREATE INDEX IF NOT EXISTS idx_actress_aliases_canonical_name ON actress_aliases(canonical_name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_movie_tag ON movie_tags(movie_id, tag)`,
		`CREATE INDEX IF NOT EXISTS idx_history_movie_id ON history(movie_id)`,
		`CREATE INDEX IF NOT EXISTS idx_history_created_at ON history(created_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_content_id_mappings_search_id ON content_id_mappings(search_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_movie_language ON movie_translations(movie_id, language)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_actresses_dmm_id_positive ON actresses(dmm_id) WHERE dmm_id > 0`,
	}
	for _, stmt := range indexStatements {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure base index: %w", err)
		}
	}

	return nil
}

func reconcileLegacyMoviesContentID(ctx context.Context, conn *sql.Conn) error {
	var moviesExists int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='movies'").Scan(&moviesExists); err != nil {
		return fmt.Errorf("check movies table existence: %w", err)
	}
	if moviesExists == 0 {
		return nil
	}

	// Backfill missing content_id values from legacy display IDs.
	if _, err := conn.ExecContext(ctx, `
		UPDATE movies
		SET content_id = lower(replace(trim(id), '-', ''))
		WHERE (content_id IS NULL OR trim(content_id) = '')
		  AND id IS NOT NULL
		  AND trim(id) <> ''
	`); err != nil {
		return fmt.Errorf("backfill movies.content_id from movies.id: %w", err)
	}

	var missingContentIDCount int
	if err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM movies
		WHERE content_id IS NULL OR trim(content_id) = ''
	`).Scan(&missingContentIDCount); err != nil {
		return fmt.Errorf("check missing movies.content_id values: %w", err)
	}
	if missingContentIDCount > 0 {
		return fmt.Errorf("cannot complete movies compatibility migration: %d row(s) are missing both content_id and id", missingContentIDCount)
	}

	var duplicateContentID string
	var duplicateCount int
	err := conn.QueryRowContext(ctx, `
		SELECT content_id, COUNT(*)
		FROM movies
		WHERE content_id IS NOT NULL AND trim(content_id) <> ''
		GROUP BY content_id
		HAVING COUNT(*) > 1
		LIMIT 1
	`).Scan(&duplicateContentID, &duplicateCount)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("check duplicate movies.content_id values: %w", err)
	}
	if err == nil && duplicateCount > 1 {
		return fmt.Errorf("cannot complete movies compatibility migration: duplicate content_id=%q (count=%d)", duplicateContentID, duplicateCount)
	}

	return nil
}

func ensureTableColumns(ctx context.Context, conn *sql.Conn, tableName string, columns []columnSpec) error {
	existingColumns, err := tableColumnsByName(ctx, conn, tableName)
	if err != nil {
		return err
	}

	for _, column := range columns {
		normalized := strings.ToLower(strings.TrimSpace(column.Name))
		if _, exists := existingColumns[normalized]; exists {
			continue
		}
		stmt := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s",
			quoteSQLiteIdentifier(tableName),
			quoteSQLiteIdentifier(column.Name),
			column.Definition,
		)
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add missing column %s.%s: %w", tableName, column.Name, err)
		}
	}

	return nil
}

func tableColumnsByName(ctx context.Context, conn *sql.Conn, tableName string) (map[string]struct{}, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", quoteSQLiteIdentifier(tableName)))
	if err != nil {
		return nil, fmt.Errorf("query table_info for %q: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]struct{})
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("scan table_info for %q: %w", tableName, err)
		}
		columns[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table_info for %q: %w", tableName, err)
	}

	return columns, nil
}

func actressesTableExists(ctx context.Context, conn *sql.Conn) (bool, error) {
	var count int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='actresses'").Scan(&count); err != nil {
		return false, fmt.Errorf("check actresses table existence: %w", err)
	}
	return count > 0, nil
}

func findDuplicatePositiveDMMID(ctx context.Context, conn *sql.Conn) (dmmID int, count int, err error) {
	var duplicateID int
	var duplicateCount int
	err = conn.QueryRowContext(ctx,
		"SELECT dmm_id, COUNT(*) FROM actresses WHERE dmm_id > 0 GROUP BY dmm_id HAVING COUNT(*) > 1 LIMIT 1",
	).Scan(&duplicateID, &duplicateCount)
	if err == nil {
		return duplicateID, duplicateCount, nil
	}
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return 0, 0, fmt.Errorf("check duplicate positive dmm_id values: %w", err)
}

func loadActressIndexes(ctx context.Context, conn *sql.Conn) ([]actressIndexMeta, error) {
	sqlByName := make(map[string]string)
	indexSQLRows, err := conn.QueryContext(ctx,
		"SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type='index' AND tbl_name='actresses'",
	)
	if err != nil {
		return nil, fmt.Errorf("list actresses index SQL definitions: %w", err)
	}
	for indexSQLRows.Next() {
		var name string
		var sqlText string
		if scanErr := indexSQLRows.Scan(&name, &sqlText); scanErr != nil {
			_ = indexSQLRows.Close()
			return nil, fmt.Errorf("scan actresses index SQL definitions: %w", scanErr)
		}
		sqlByName[name] = sqlText
	}
	if err := indexSQLRows.Err(); err != nil {
		_ = indexSQLRows.Close()
		return nil, fmt.Errorf("iterate actresses index SQL definitions: %w", err)
	}
	_ = indexSQLRows.Close()

	indexRows, err := conn.QueryContext(ctx, "PRAGMA index_list('actresses')")
	if err != nil {
		return nil, fmt.Errorf("list actresses indexes: %w", err)
	}
	defer func() { _ = indexRows.Close() }()

	result := make([]actressIndexMeta, 0)
	for indexRows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := indexRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, fmt.Errorf("scan actresses index list: %w", err)
		}

		meta := actressIndexMeta{
			Name:    name,
			Unique:  unique,
			Partial: partial,
			Origin:  origin,
			SQL:     sqlByName[name],
		}

		columns, hasUnsupportedKeyParts, err := loadIndexColumns(ctx, conn, name)
		if err != nil {
			return nil, err
		}
		meta.Columns = columns
		meta.HasUnsupportedKeyParts = hasUnsupportedKeyParts

		result = append(result, meta)
	}
	if err := indexRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate actresses index list: %w", err)
	}

	return result, nil
}

func loadIndexColumns(ctx context.Context, conn *sql.Conn, indexName string) ([]indexColumnSpec, bool, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("PRAGMA index_xinfo(%s)", quoteSQLiteIdentifier(indexName)))
	if err != nil {
		return nil, false, fmt.Errorf("list columns for index %q: %w", indexName, err)
	}
	defer func() { _ = rows.Close() }()

	columns := make([]indexColumnSpec, 0)
	hasUnsupportedKeyParts := false
	for rows.Next() {
		var seqno int
		var cid int
		var colName sql.NullString
		var desc int
		var collation sql.NullString
		var key int
		if err := rows.Scan(&seqno, &cid, &colName, &desc, &collation, &key); err != nil {
			return nil, false, fmt.Errorf("scan columns for index %q: %w", indexName, err)
		}
		if key != 1 {
			continue
		}
		if !colName.Valid || strings.TrimSpace(colName.String) == "" {
			// Cannot faithfully reconstruct key parts when SQLite does not expose a column name
			// (for example expression-based components).
			hasUnsupportedKeyParts = true
			continue
		}

		spec := indexColumnSpec{
			Name: colName.String,
			Desc: desc == 1,
		}
		if collation.Valid {
			collationName := strings.TrimSpace(collation.String)
			if collationName != "" && !strings.EqualFold(collationName, "BINARY") {
				spec.Collation = collationName
			}
		}
		columns = append(columns, spec)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate columns for index %q: %w", indexName, err)
	}
	return columns, hasUnsupportedKeyParts, nil
}

func isUniqueSingleColumnDMMIndex(index actressIndexMeta) bool {
	return index.Unique == 1 &&
		len(index.Columns) == 1 &&
		strings.EqualFold(strings.TrimSpace(index.Columns[0].Name), "dmm_id")
}

func isCanonicalPositiveDMMIndex(index actressIndexMeta) bool {
	return isUniqueSingleColumnDMMIndex(index) &&
		index.Partial == 1 &&
		strings.EqualFold(index.Name, canonicalPositiveDMMIndexName) &&
		isDesiredDMMIDPartialIndexSQL(index.SQL)
}

func rebuildActressesTable(
	ctx context.Context,
	conn *sql.Conn,
	preservedIndexSQL []string,
) error {
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys for actresses rebuild: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	}()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin actresses rebuild transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	createTableSQL, err := buildActressesRebuildCreateTableSQL(ctx, conn)
	if err != nil {
		return err
	}
	insertSQL, err := buildActressesRebuildCopySQL(ctx, conn)
	if err != nil {
		return err
	}

	steps := []string{
		createTableSQL,
		insertSQL,
		`DROP TABLE actresses`,
		`ALTER TABLE actresses_new RENAME TO actresses`,
		`CREATE INDEX idx_actresses_japanese_name ON actresses(japanese_name)`,
		`CREATE UNIQUE INDEX idx_actresses_dmm_id_positive ON actresses(dmm_id) WHERE dmm_id > 0`,
	}

	for _, stmt := range steps {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("execute actresses rebuild step: %w", err)
		}
	}

	for _, createIndexSQL := range preservedIndexSQL {
		if _, err := conn.ExecContext(ctx, createIndexSQL); err != nil {
			return fmt.Errorf("recreate preserved actresses index: %w", err)
		}
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit actresses rebuild transaction: %w", err)
	}
	committed = true

	return nil
}

func buildActressesRebuildCreateTableSQL(ctx context.Context, conn *sql.Conn) (string, error) {
	var createSQL string
	if err := conn.QueryRowContext(
		ctx,
		"SELECT COALESCE(sql, '') FROM sqlite_master WHERE type='table' AND name='actresses'",
	).Scan(&createSQL); err != nil {
		return "", fmt.Errorf("load actresses table SQL for rebuild: %w", err)
	}
	if strings.TrimSpace(createSQL) == "" {
		return "", fmt.Errorf("load actresses table SQL for rebuild: empty SQL definition")
	}

	openIdx := strings.Index(createSQL, "(")
	closeIdx := strings.LastIndex(createSQL, ")")
	if openIdx < 0 || closeIdx <= openIdx {
		return "", fmt.Errorf("parse actresses table SQL for rebuild: malformed CREATE TABLE statement")
	}

	segments := splitSQLTopLevelCommaList(createSQL[openIdx+1 : closeIdx])
	kept := make([]string, 0, len(segments))
	removedDMMUnique := false
	for _, segment := range segments {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		if isUniqueSingleColumnDMMConstraintDefinition(trimmed) {
			removedDMMUnique = true
			continue
		}
		rewrittenInlineSegment, removedInlineUnique := removeInlineUniqueDMMConstraint(trimmed)
		if removedInlineUnique {
			removedDMMUnique = true
			kept = append(kept, rewrittenInlineSegment)
			continue
		}
		kept = append(kept, segment)
	}
	if !removedDMMUnique {
		return "", fmt.Errorf("rebuild actresses schema: expected UNIQUE(dmm_id) constraint was not found in table definition")
	}

	trailing := createSQL[closeIdx+1:]
	return "CREATE TABLE actresses_new (" + strings.Join(kept, ", ") + ")" + trailing, nil
}

func buildActressesRebuildCopySQL(ctx context.Context, conn *sql.Conn) (string, error) {
	rows, err := conn.QueryContext(ctx, "PRAGMA table_info('actresses')")
	if err != nil {
		return "", fmt.Errorf("load actresses columns for rebuild copy: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columnNames := make([]string, 0)
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return "", fmt.Errorf("scan actresses columns for rebuild copy: %w", err)
		}
		columnNames = append(columnNames, name)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate actresses columns for rebuild copy: %w", err)
	}
	if len(columnNames) == 0 {
		return "", fmt.Errorf("load actresses columns for rebuild copy: no columns found")
	}

	insertColumns := make([]string, 0, len(columnNames))
	selectExpressions := make([]string, 0, len(columnNames))
	for _, columnName := range columnNames {
		quotedColumn := quoteSQLiteIdentifier(columnName)
		insertColumns = append(insertColumns, quotedColumn)
		if strings.EqualFold(strings.TrimSpace(columnName), "dmm_id") {
			selectExpressions = append(selectExpressions, "CASE WHEN dmm_id < 0 THEN 0 ELSE dmm_id END")
		} else {
			selectExpressions = append(selectExpressions, quotedColumn)
		}
	}

	return fmt.Sprintf(
		"INSERT INTO actresses_new (%s) SELECT %s FROM actresses",
		strings.Join(insertColumns, ", "),
		strings.Join(selectExpressions, ", "),
	), nil
}

func splitSQLTopLevelCommaList(value string) []string {
	parts := make([]string, 0)
	start := 0
	depth := 0
	inSingleQuote := false
	inDoubleQuote := false
	inBacktickQuote := false

	for i, r := range value {
		switch r {
		case '\'':
			if !inDoubleQuote && !inBacktickQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote && !inBacktickQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '`':
			if !inSingleQuote && !inDoubleQuote {
				inBacktickQuote = !inBacktickQuote
			}
		case '(':
			if !inSingleQuote && !inDoubleQuote && !inBacktickQuote {
				depth++
			}
		case ')':
			if !inSingleQuote && !inDoubleQuote && !inBacktickQuote && depth > 0 {
				depth--
			}
		case ',':
			if !inSingleQuote && !inDoubleQuote && !inBacktickQuote && depth == 0 {
				parts = append(parts, value[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, value[start:])
	return parts
}

func isUniqueSingleColumnDMMConstraintDefinition(segment string) bool {
	normalized := strings.TrimSpace(strings.ToLower(segment))
	if strings.HasPrefix(normalized, "constraint ") {
		afterConstraint := strings.TrimSpace(segment[len("constraint "):])
		spaceIdx := strings.IndexAny(afterConstraint, " \t\r\n")
		if spaceIdx <= 0 || spaceIdx >= len(afterConstraint)-1 {
			return false
		}
		segment = strings.TrimSpace(afterConstraint[spaceIdx+1:])
		normalized = strings.TrimSpace(strings.ToLower(segment))
	}
	if !strings.HasPrefix(normalized, "unique") {
		return false
	}
	openIdx := strings.Index(segment, "(")
	closeIdx := strings.LastIndex(segment, ")")
	if openIdx < 0 || closeIdx <= openIdx {
		return false
	}
	entries := splitSQLTopLevelCommaList(segment[openIdx+1 : closeIdx])
	if len(entries) != 1 {
		return false
	}
	columnName, ok := extractLeadingIdentifier(entries[0])
	return ok && strings.EqualFold(columnName, "dmm_id")
}

func removeInlineUniqueDMMConstraint(segment string) (string, bool) {
	columnName, ok := extractLeadingIdentifier(segment)
	if !ok || !strings.EqualFold(columnName, "dmm_id") {
		return segment, false
	}

	segmentLen := len(segment)
	removeStart := -1
	removeEnd := -1

	inSingleQuote := false
	inDoubleQuote := false
	inBacktickQuote := false
	depth := 0

	for i := 0; i < segmentLen; i++ {
		r := rune(segment[i])

		switch r {
		case '\'':
			if !inDoubleQuote && !inBacktickQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote && !inBacktickQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '`':
			if !inSingleQuote && !inDoubleQuote {
				inBacktickQuote = !inBacktickQuote
			}
		case '(':
			if !inSingleQuote && !inDoubleQuote && !inBacktickQuote {
				depth++
			}
		case ')':
			if !inSingleQuote && !inDoubleQuote && !inBacktickQuote && depth > 0 {
				depth--
			}
		}

		if inSingleQuote || inDoubleQuote || inBacktickQuote || depth != 0 {
			continue
		}
		if !hasKeywordAt(segment, i, "unique") {
			continue
		}

		removeStart = i
		if constraintStart, ok := findConstraintPrefixStart(segment, i); ok {
			removeStart = constraintStart
		}
		removeEnd = i + len("unique")
		j := skipSpaces(segment, removeEnd)
		if hasKeywordAt(segment, j, "on") {
			j = skipSpaces(segment, j+len("on"))
			if hasKeywordAt(segment, j, "conflict") {
				j = skipSpaces(segment, j+len("conflict"))
				algoStart := j
				for j < segmentLen && isIdentifierChar(rune(segment[j])) {
					j++
				}
				if j > algoStart {
					removeEnd = j
				} else {
					removeEnd = skipSpaces(segment, removeEnd)
				}
			}
		}
		break
	}

	if removeStart < 0 {
		return segment, false
	}

	rewritten := strings.TrimSpace(segment[:removeStart] + " " + segment[removeEnd:])
	return rewritten, true
}

func skipSpaces(value string, start int) int {
	for start < len(value) {
		if !unicode.IsSpace(rune(value[start])) {
			break
		}
		start++
	}
	return start
}

func skipSpacesBackward(value string, start int) int {
	for start >= 0 {
		if !unicode.IsSpace(rune(value[start])) {
			break
		}
		start--
	}
	return start
}

func previousIdentifierTokenBounds(value string, before int) (int, int, bool) {
	endIdx := skipSpacesBackward(value, before-1)
	if endIdx < 0 {
		return 0, 0, false
	}
	switch value[endIdx] {
	case '"':
		startIdx := strings.LastIndex(value[:endIdx], `"`)
		if startIdx < 0 {
			return 0, 0, false
		}
		return startIdx, endIdx + 1, true
	case '`':
		startIdx := strings.LastIndex(value[:endIdx], "`")
		if startIdx < 0 {
			return 0, 0, false
		}
		return startIdx, endIdx + 1, true
	case ']':
		startIdx := strings.LastIndex(value[:endIdx], "[")
		if startIdx < 0 {
			return 0, 0, false
		}
		return startIdx, endIdx + 1, true
	}
	if !isIdentifierChar(rune(value[endIdx])) {
		return 0, 0, false
	}
	startIdx := endIdx
	for startIdx >= 0 && isIdentifierChar(rune(value[startIdx])) {
		startIdx--
	}
	startIdx++
	return startIdx, endIdx + 1, true
}

func findConstraintPrefixStart(segment string, uniqueStart int) (int, bool) {
	nameStart, _, ok := previousIdentifierTokenBounds(segment, uniqueStart)
	if !ok {
		return 0, false
	}
	keywordStart, keywordEnd, ok := previousIdentifierTokenBounds(segment, nameStart)
	if !ok {
		return 0, false
	}
	if !strings.EqualFold(segment[keywordStart:keywordEnd], "constraint") {
		return 0, false
	}
	return keywordStart, true
}

func hasKeywordAt(value string, index int, keyword string) bool {
	if index < 0 || index+len(keyword) > len(value) {
		return false
	}
	if !strings.EqualFold(value[index:index+len(keyword)], keyword) {
		return false
	}
	if index > 0 {
		prev := rune(value[index-1])
		if isIdentifierChar(prev) {
			return false
		}
	}
	if index+len(keyword) < len(value) {
		next := rune(value[index+len(keyword)])
		if isIdentifierChar(next) {
			return false
		}
	}
	return true
}

func isIdentifierChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func extractLeadingIdentifier(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	if trimmed[0] == '"' {
		end := strings.Index(trimmed[1:], "\"")
		if end < 0 {
			return "", false
		}
		return strings.ReplaceAll(trimmed[1:1+end], `""`, `"`), true
	}
	if trimmed[0] == '`' {
		end := strings.Index(trimmed[1:], "`")
		if end < 0 {
			return "", false
		}
		return trimmed[1 : 1+end], true
	}
	if trimmed[0] == '[' {
		end := strings.Index(trimmed[1:], "]")
		if end < 0 {
			return "", false
		}
		return trimmed[1 : 1+end], true
	}
	for i, r := range trimmed {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '(' {
			return trimmed[:i], true
		}
	}
	return trimmed, true
}

func quoteSQLiteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func isDesiredDMMIDPartialIndexSQL(sqlText string) bool {
	normalized := strings.ToLower(sqlText)
	replacer := strings.NewReplacer(`"`, "", "`", "", "[", "", "]", "")
	normalized = replacer.Replace(normalized)
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "\n", "")
	normalized = strings.ReplaceAll(normalized, "\t", "")
	normalized = strings.ReplaceAll(normalized, "\r", "")
	return strings.HasPrefix(normalized, "createuniqueindex") &&
		strings.Contains(normalized, "onactresses(dmm_id)") &&
		strings.HasSuffix(normalized, "wheredmm_id>0")
}
