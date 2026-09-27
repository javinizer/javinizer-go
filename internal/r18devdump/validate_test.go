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

func TestValidateSidecar_NotADatabase(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "junk.db")
	require.NoError(t, os.WriteFile(dst, []byte("SQLite format 3\x00 but then garbage"), 0o600))
	_, err := ValidateSidecar(context.Background(), dst)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDumpInvalid))
}
