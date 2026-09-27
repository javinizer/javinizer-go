package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const oneRowDump = "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"

func importFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dump.db")
	_, err := Import(context.Background(), strings.NewReader(oneRowDump), path, ImportOptions{SourceURL: "https://example/dumps/r18dotdev_dump_2026-09-20.sql.gz"})
	require.NoError(t, err)
	return path
}

func alterFixture(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	for _, s := range stmts {
		_, err := db.Exec(s)
		require.NoError(t, err)
	}
}

func TestValidateSidecar_ValidImportPasses(t *testing.T) {
	store, err := ValidateSidecar(context.Background(), importFixture(t))
	require.NoError(t, err)
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.RowCount)
	require.NoError(t, store.Close())
}

func TestValidateSidecar_MissingColumnRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path, "DROP INDEX idx_videos_dvd_id_norm", "ALTER TABLE videos DROP COLUMN dvd_id_norm")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDumpInvalid))
	assert.Contains(t, err.Error(), "videos.dvd_id_norm")
}

func TestValidateSidecar_MissingIndexRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path, "DROP INDEX idx_videos_dvd_id_norm")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDumpInvalid))
	assert.Contains(t, err.Error(), "idx_videos_dvd_id_norm")
}

func TestValidateSidecar_AdditiveAccepted(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path,
		"CREATE TABLE extra_future (id TEXT PRIMARY KEY, payload TEXT)",
		"ALTER TABLE videos ADD COLUMN future_col TEXT",
		"CREATE INDEX idx_extra ON extra_future(id)")
	store, err := ValidateSidecar(context.Background(), path)
	require.NoError(t, err)
	require.NoError(t, store.Close())
}

func TestValidateSidecar_TruncatedRejected(t *testing.T) {
	src := importFixture(t)
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.Greater(t, len(data), 100)
	dst := filepath.Join(t.TempDir(), "trunc.db")
	require.NoError(t, os.WriteFile(dst, data[:len(data)/2], 0o600))
	_, err = ValidateSidecar(context.Background(), dst)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDumpInvalid))
}

func TestValidateSidecar_EmptyProvenanceRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path, "DELETE FROM dump_meta")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provenance")
}

func TestValidateSidecar_ZeroVideosRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path, "DELETE FROM videos")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "zero videos")
}

func TestValidateSidecar_WrongMetaColumnsRejected(t *testing.T) {
	// Codex: a dump_meta with a row count but no key/value columns passes the
	// old count-only check while breaking every provenance read afterward.
	path := importFixture(t)
	alterFixture(t, path,
		"CREATE TABLE dump_meta_new (k TEXT PRIMARY KEY, v TEXT)",
		"INSERT INTO dump_meta_new VALUES ('source_url', 'https://example/x')",
		"DROP TABLE dump_meta",
		"ALTER TABLE dump_meta_new RENAME TO dump_meta")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "dump_meta.key")
}

func TestValidateSidecar_IndexNameSquattingRejected(t *testing.T) {
	// Codex: an index named like ours but defined on the wrong column passes
	// name-only validation while turning every lookup into a full table scan.
	path := importFixture(t)
	alterFixture(t, path,
		"DROP INDEX idx_videos_dvd_id_norm",
		"CREATE INDEX idx_videos_dvd_id_norm ON videos(title_en)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "idx_videos_dvd_id_norm")
	assert.Contains(t, err.Error(), "title_en")
}

func TestValidateSidecar_IndexWrongTableRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path,
		"DROP INDEX idx_videos_dvd_id_norm",
		"CREATE INDEX idx_videos_dvd_id_norm ON video_actresses(content_id)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "wrong table")
}

func TestValidateSidecar_NullMetaKeysRejected(t *testing.T) {
	// Codex: NULL-key dump_meta rows pass count+column checks but break every
	// Stats scan and make clearDump refuse to delete the installed dump.
	path := importFixture(t)
	alterFixture(t, path,
		"DROP TABLE dump_meta",
		"CREATE TABLE dump_meta (key TEXT, value TEXT)",
		"INSERT INTO dump_meta VALUES ('source_url', 'https://example/x'), (NULL, 'orphan')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "NULL keys")
}

func TestValidateSidecar_NullableContentIDRejected(t *testing.T) {
	// Codex: a rebuilt videos table without NOT NULL/PK passes index,
	// nonempty, and integrity checks while NULL content_id rows break every
	// lookup scan afterward.
	path := importFixture(t)
	alterFixture(t, path,
		"CREATE TABLE videos_loose (content_id TEXT, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT)",
		"INSERT INTO videos_loose (content_id, dvd_id_norm) VALUES (NULL, 'IPX535')",
		"DROP TABLE videos",
		"ALTER TABLE videos_loose RENAME TO videos",
		"CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "NULL content_id")
}

// looseVideosFixture rebuilds the videos table without its primary key (all
// columns present, content_id nullable) then inserts givenRows; the required
// index is recreated so only the target invariant differs from a valid dump.
func looseVideosFixture(t *testing.T, rowsSQL string) string {
	t.Helper()
	path := importFixture(t)
	alterFixture(t, path,
		"CREATE TABLE videos_loose (content_id TEXT, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT)",
		rowsSQL,
		"DROP TABLE videos",
		"ALTER TABLE videos_loose RENAME TO videos",
		"CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	return path
}

func TestValidateSidecar_DuplicateContentIDsRejected(t *testing.T) {
	// Codex: duplicate content_ids on a PK-less rebuild pass the NULL probe
	// but make content-id lookups nondeterministic (LIMIT 1 without order).
	path := looseVideosFixture(t,
		"INSERT INTO videos_loose (content_id, dvd_id_norm) VALUES ('118ipx00535', 'IPX535'), ('118ipx00535', 'IPX535')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "duplicate content_id")
}

func TestValidateSidecar_PartialIndexPredicateRejected(t *testing.T) {
	// Codex: the required name with a WHERE predicate satisfies name/table/
	// column probes but is unusable for unconditional lookups.
	path := importFixture(t)
	alterFixture(t, path,
		"DROP INDEX idx_videos_dvd_id_norm",
		"CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm) WHERE dvd_id_norm IS NOT NULL")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "partial")
}

// looseTableFixture rebuilds a required table without constraints, inserting
// the given row SQL, so logical-key probes observe exactly the crafted shape.
// looseAssocFixture rebuilds the video_actresses association without its
// composite PK (and recreates its required index, which DROP TABLE cascades).
func looseAssocFixture(t *testing.T, rowsSQL string) string {
	t.Helper()
	path := importFixture(t)
	alterFixture(t, path,
		"CREATE TABLE video_actresses_loose (content_id TEXT, actress_id TEXT, ordinality INTEGER, release_date TEXT)",
		rowsSQL,
		"DROP TABLE video_actresses",
		"ALTER TABLE video_actresses_loose RENAME TO video_actresses",
		"CREATE INDEX idx_video_actresses_cid ON video_actresses(content_id)")
	return path
}

func looseTableFixture(t *testing.T, table, colsDDL, rowsSQL string) string {
	t.Helper()
	path := importFixture(t)
	alterFixture(t, path,
		"CREATE TABLE "+table+"_loose ("+colsDDL+")",
		rowsSQL,
		"DROP TABLE "+table,
		"ALTER TABLE "+table+"_loose RENAME TO "+table)
	return path
}

func TestValidateSidecar_DuplicateEntityIDRejected(t *testing.T) {
	path := looseTableFixture(t, "actresses",
		"id TEXT, name_romaji TEXT, image_url TEXT, name_kanji TEXT, name_kana TEXT",
		"INSERT INTO actresses_loose (id, name_romaji) VALUES ('a1', 'Jane'), ('a1', 'Jane')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "duplicate id")
}

func TestValidateSidecar_NullEntityIDRejected(t *testing.T) {
	path := looseTableFixture(t, "makers",
		"id TEXT, name_en TEXT, name_ja TEXT",
		"INSERT INTO makers_loose (id, name_en) VALUES (NULL, 'x')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "NULL id")
}

func TestValidateSidecar_DuplicateAssociationRejected(t *testing.T) {
	path := looseAssocFixture(t, "INSERT INTO video_actresses_loose (content_id, actress_id) VALUES ('118ipx00535', 'a1'), ('118ipx00535', 'a1')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "duplicate content_id")
}

func TestValidateSidecar_CollationIndexRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path,
		"DROP INDEX idx_videos_dvd_id_norm",
		"CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm COLLATE NOCASE)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "collations")
}

func TestValidateSidecar_TextRuntimeMinsRejected(t *testing.T) {
	// Codex: SQLite affinity doesn't constrain storage, so a loose rebuild may
	// carry 'unknown' where the reads scan sql.NullInt64.
	path := looseTableFixture(t, "videos",
		"content_id TEXT PRIMARY KEY, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT",
		"INSERT INTO videos_loose (content_id, runtime_mins) VALUES ('118ipx00535', 'unknown')")
	alterFixture(t, path, "CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "runtime_mins")
}

func TestValidateSidecar_NotADatabase(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "junk.db")
	require.NoError(t, os.WriteFile(dst, []byte("SQLite format 3\x00 but then garbage"), 0o600))
	_, err := ValidateSidecar(context.Background(), dst)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDumpInvalid))
}
