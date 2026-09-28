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

func TestValidateSidecar_LooseUniqueNoPKRejected(t *testing.T) {
	// Codex: unique-but-PK-less keys slip past the NULL/dup probes while
	// forcing every content_id lookup to full-scan the table.
	path := looseVideosFixture(t,
		"INSERT INTO videos_loose (content_id, dvd_id_norm) VALUES ('118ipx00535', 'IPX535')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "primary key does not start with content_id")
}

func TestValidateSidecar_MisorderedPKRejected(t *testing.T) {
	// Codex: membership alone allowed PRIMARY KEY(dvd_id_norm, content_id),
	// which is unusable for content_id-prefix lookups; ordered check rejects it.
	path := importFixture(t)
	alterFixture(t, path,
		"CREATE TABLE videos_reordered (content_id TEXT, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT, PRIMARY KEY (dvd_id_norm, content_id))",
		"INSERT INTO videos_reordered (content_id, dvd_id_norm) VALUES ('118ipx00535', 'IPX535')",
		"DROP TABLE videos",
		"ALTER TABLE videos_reordered RENAME TO videos",
		"CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "does not start with content_id")
}

func TestValidateSidecar_WrongNormRejected(t *testing.T) {
	// Codex: installed dumps key every DVD lookup on dvd_id_norm; a norm that
	// disagrees with dvd_id makes the movie unreachable or misrouted.
	path := importFixture(t)
	alterFixture(t, path,
		"INSERT INTO videos (content_id, dvd_id, dvd_id_norm) VALUES ('118abw00013', 'ABW-013', 'WRONGNORM')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "dvd_id_norm")
}

func TestValidateSidecar_BlobKeyRejected(t *testing.T) {
	// Codex: BLOB-valued keys match nothing when the lookups bind TEXT.
	path := looseVideosFixture(t,
		"INSERT INTO videos_loose (content_id, dvd_id_norm) VALUES (CAST('118ipx00535' AS BLOB), 'IPX535')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "non-TEXT")
}

func TestValidateSidecar_EmptyNormOnRealIDRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path,
		"INSERT INTO videos (content_id, dvd_id, dvd_id_norm) VALUES ('118xyz00099', 'XYZ-99', '')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "dvd_id_norm")
}

func TestValidateSidecar_NullNormOnRealIDRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path,
		"INSERT INTO videos (content_id, dvd_id, dvd_id_norm) VALUES ('118xyz00098', 'XYZ-987', NULL)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "dvd_id_norm")
}

func TestValidateSidecar_NullMetaValueRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path,
		"INSERT OR REPLACE INTO dump_meta (key, value) VALUES ('source_url', NULL)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "NULL keys or values")
}

func TestValidateSidecar_OrphanNormRejected(t *testing.T) {
	// Codex: a norm without any dvd_id routes searches at content with no DVD ID.
	path := importFixture(t)
	alterFixture(t, path,
		"INSERT INTO videos (content_id, dvd_id, dvd_id_norm) VALUES ('118xyzo00097', '', 'XYZ097')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "dvd_id_norm")
}

func TestValidateSidecar_UppercaseContentIDRejected(t *testing.T) {
	path := looseTableFixture(t, "videos",
		"content_id TEXT PRIMARY KEY, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT",
		"INSERT INTO videos_loose (content_id, dvd_id, dvd_id_norm) VALUES ('118IPX00666', 'IPX-666', 'IPX666')")
	alterFixture(t, path, "CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "noncanonical")
}

func TestValidateSidecar_DuplicateMetaKeysRejected(t *testing.T) {
	// Codex: duplicate source_url rows make Update's skip decision
	// nondeterministic (loadMeta overwrites map entries unordered).
	path := importFixture(t)
	alterFixture(t, path,
		"DROP TABLE dump_meta",
		"CREATE TABLE dump_meta (key TEXT, value TEXT)",
		"INSERT INTO dump_meta VALUES ('source_url', 'https://a/x.sql.gz'), ('source_url', 'https://b/x.sql.gz'), ('source_date', '2026-09-20'), ('imported_at', '2026-09-27T00:00:00Z')")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "duplicate keys")
}

func TestValidateSidecar_PKWithNoCaseCollationRejected(t *testing.T) {
	// Codex: explicit non-BINARY collations on the primary key make the
	// autoindex unusable for the lookups' binary predicates.
	path := looseTableFixture(t, "videos",
		"content_id TEXT PRIMARY KEY COLLATE NOCASE, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT",
		"INSERT INTO videos_loose (content_id, dvd_id, dvd_id_norm) VALUES ('118iptest007', 'IPT-007', 'IPT007')")
	alterFixture(t, path, "CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "declares COLLATE NOCASE")
}

func TestValidateSidecar_CollatedColumnDDLRejected(t *testing.T) {
	// Codex: TEXT COLLATE NOCASE column declarations are invisible to pragma
	// metadata probes but change predicate collation, defeating BINARY keys.
	path := looseTableFixture(t, "videos",
		"content_id TEXT COLLATE NOCASE PRIMARY KEY, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT",
		"INSERT INTO videos_loose (content_id, dvd_id, dvd_id_norm) VALUES ('118iptest001', 'IPT-001', 'IPT001')")
	alterFixture(t, path, "CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "declares COLLATE NOCASE")
}

func TestValidateSidecar_NotADatabase(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "junk.db")
	require.NoError(t, os.WriteFile(dst, []byte("SQLite format 3\x00 but then garbage"), 0o600))
	_, err := ValidateSidecar(context.Background(), dst)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDumpInvalid))
}

func TestValidateSidecar_TabbedContentIDRejected(t *testing.T) {
	// Codex: SQLite's one-arg TRIM strips only ASCII spaces; 'abc\t' would
	// pass TRIM while the importer's TrimSpace rejects it. The probe strips
	// the full ASCII whitespace set to mirror the importer.
	path := looseTableFixture(t, "videos",
		"content_id TEXT PRIMARY KEY, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT",
		"INSERT INTO videos_loose (content_id, dvd_id, dvd_id_norm) VALUES ('118ipx00001' || char(9), 'IPX-001', 'IPX001')")
	alterFixture(t, path, "CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "noncanonical")
}

func TestValidateSidecar_UppercaseEntityIDAccepted(t *testing.T) {
	// Codex: entity lookups compare exactly — no lowercase contract exists for
	// entity ids, so an uppercase entity key must remain valid.
	path := looseTableFixture(t, "actresses",
		"id TEXT PRIMARY KEY, name_romaji TEXT, image_url TEXT, name_kanji TEXT, name_kana TEXT",
		"INSERT INTO actresses_loose (id, name_romaji) VALUES ('ABC', 'Jane')")
	store, err := ValidateSidecar(context.Background(), path)
	require.NoError(t, err)
	// Windows: temp-dir cleanup unlinks the held DB file, so accept-path
	// callers MUST be closed; production code closes before any swap.
	require.NoError(t, store.Close())
}

func TestValidateSidecar_BlankContentIDRejected(t *testing.T) {
	path := looseTableFixture(t, "videos",
		"content_id TEXT PRIMARY KEY, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT",
		"INSERT INTO videos_loose (content_id, dvd_id, dvd_id_norm) VALUES ('   ', 'IPX-535', 'IPX535')")
	alterFixture(t, path, "CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "noncanonical")
}

func TestValidateSidecar_MissingProvenanceKeyRejected(t *testing.T) {
	// Codex: a dump_meta with rows but no source_url makes Update unable to
	// recognize the installed version, forcing spurious multi-GB redownloads.
	path := importFixture(t)
	alterFixture(t, path, "DELETE FROM dump_meta WHERE key = 'source_url'")
	_, err := ValidateSidecar(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDumpInvalid)
	assert.Contains(t, err.Error(), "missing required provenance keys")
}

func TestDDLHelpers_TableDriven(t *testing.T) {
	assert.Equal(t, []string{"a INTEGER", " b TEXT", " PRIMARY KEY (a, b)"},
		splitColumnsDDL("a INTEGER, b TEXT, PRIMARY KEY (a, b)"))
	assert.Equal(t, []string{"a INTEGER DEFAULT f(1,2)", " b TEXT"},
		splitColumnsDDL("a INTEGER DEFAULT f(1,2), b TEXT"))
	assert.Equal(t, []string{""}, splitColumnsDDL(""))

	n1, c1, ok1 := segmentColumnAndCollation("content_id TEXT PRIMARY KEY COLLATE NOCASE")
	assert.True(t, ok1)
	assert.Equal(t, "content_id", n1)
	assert.Equal(t, "NOCASE", c1)

	n2, c2, ok2 := segmentColumnAndCollation("PRIMARY KEY (content_id)")
	assert.False(t, ok2)
	assert.Equal(t, "PRIMARY", n2)
	assert.Empty(t, c2)

	n3, c3, ok3 := segmentColumnAndCollation("   ")
	assert.Empty(t, n3)
	assert.Empty(t, c3)
	assert.False(t, ok3)

	n4, _, ok4 := segmentColumnAndCollation("notes TEXT COLLATE NOCASE")
	assert.True(t, ok4)
	assert.Equal(t, "notes", n4)
}

func TestDDLBody(t *testing.T) {
	assert.Equal(t, "a, b", ddlBody("CREATE TABLE t (a, b)"))
	assert.Empty(t, ddlBody("CREATE TABLE t"))
	assert.Empty(t, ddlBody("widowed"))
}

func TestValidateSidecar_InternalWhitespaceContentIDRejected(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path,
		`INSERT INTO videos (content_id, dvd_id) VALUES ('118ipx 00535', 'DVD-900')`,
	)
	_, err := ValidateSidecar(context.Background(), path)
	if !errors.Is(err, ErrDumpInvalid) {
		t.Fatalf("expected ErrDumpInvalid for internal-whitespace content_id, got %v", err)
	}
	assert.Contains(t, err.Error(), "noncanonical content_id")
}

func TestCollationScopeColumns(t *testing.T) {
	assert.Equal(t, []string{contentIDColumn, "dvd_id"}, collationScopeColumns(videosTable))
	assert.Equal(t, []string{"id"}, collationScopeColumns("actresses"))
	assert.Equal(t, []string{contentIDColumn, "actress_id"}, collationScopeColumns(videoActressesTable))
	assert.Nil(t, collationScopeColumns("must_never_exist"))
}

func TestValidateSidecar_RequiredNonKeyCollationAccepted(t *testing.T) {
	// Codex: a COLLATE clause on a required-but-never-compared column (e.g.
	// videos.title_en) is decoration — runtime lookups compare only the
	// logical keys, so this kind of dump must validate.
	path := importFixture(t)
	alterFixture(t, path,
		`ALTER TABLE videos RENAME TO videos_old`,
		`CREATE TABLE videos (
			content_id TEXT PRIMARY KEY, dvd_id TEXT, dvd_id_norm TEXT,
			title_en TEXT COLLATE NOCASE, title_ja TEXT,
			comment_en TEXT, comment_ja TEXT,
			runtime_mins INTEGER, release_date TEXT, sample_url TEXT,
			maker_id TEXT, label_id TEXT, series_id TEXT,
			jacket_full_url TEXT, jacket_thumb_url TEXT,
			gallery_full_first TEXT, gallery_full_last TEXT,
			gallery_thumb_first TEXT, gallery_thumb_last TEXT,
			site_id TEXT, service_code TEXT
		)`,
		`INSERT INTO videos SELECT content_id, dvd_id, dvd_id_norm, title_en, title_ja, comment_en, comment_ja, runtime_mins, release_date, sample_url, maker_id, label_id, series_id, jacket_full_url, jacket_thumb_url, gallery_full_first, gallery_full_last, gallery_thumb_first, gallery_thumb_last, site_id, service_code FROM videos_old`,
		`DROP TABLE videos_old`,
		`CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)`,
	)
	store, err := ValidateSidecar(context.Background(), path)
	require.NoError(t, err)
	require.NoError(t, store.Close())
}

func TestValidateSidecar_NonKeyCollatedAdditiveAccepted(t *testing.T) {
	// Codex: additive unrelated columns keep their own collations (the DDL
	// blanket-rejection that preceded the scoped check must not come back).
	path := looseTableFixture(t, "videos",
		"content_id TEXT PRIMARY KEY, dvd_id TEXT, dvd_id_norm TEXT, title_en TEXT, title_ja TEXT, comment_en TEXT, comment_ja TEXT, runtime_mins INTEGER, release_date TEXT, sample_url TEXT, maker_id TEXT, label_id TEXT, series_id TEXT, jacket_full_url TEXT, jacket_thumb_url TEXT, gallery_full_first TEXT, gallery_full_last TEXT, gallery_thumb_first TEXT, gallery_thumb_last TEXT, site_id TEXT, service_code TEXT, notes TEXT COLLATE NOCASE",
		"INSERT INTO videos_loose (content_id, dvd_id, dvd_id_norm, notes) VALUES ('118iptest002', 'IPT-002', 'IPT002', 'abc')")
	alterFixture(t, path, "CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	store, err := ValidateSidecar(context.Background(), path)
	require.NoError(t, err, "additive collated non-key columns must remain acceptable")
	require.NoError(t, store.Close())
}
