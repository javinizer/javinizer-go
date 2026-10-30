package r18devdump

import (
	"bytes"
	"database/sql"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/stretchr/testify/require"
)

func TestLookupMovieActressIterationFailureDegradesAndRecovers(t *testing.T) {
	path := importFullDump(t)
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	healthy, err := store.LookupMovie(t.Context(), "ABW-013")
	require.NoError(t, err)
	require.NotEmpty(t, healthy.Actresses)
	require.NotNil(t, healthy.Maker)
	require.NotNil(t, healthy.Label)
	require.NotNil(t, healthy.Series)
	require.NotNil(t, healthy.Director)
	require.NotEmpty(t, healthy.Categories)

	writer, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	require.NoError(t, func() error {
		tx, err := writer.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec(`ALTER TABLE actresses RENAME TO iteration_fixture_actresses`); err != nil {
			return err
		}
		// A valid SQLite view prepares normally, but evaluating this integer
		// expression fails in sqlite3_step (Rows.Next), not query preparation
		// or Scan. This models an unreadable non-critical sidecar relation
		// without timing cancellation or injecting a private driver callback.
		if _, err := tx.Exec(`CREATE VIEW actresses AS
			SELECT id, abs(-9223372036854775808) AS name_romaji,
				image_url, name_kanji, name_kana FROM iteration_fixture_actresses`); err != nil {
			return err
		}
		return tx.Commit()
	}())

	var logs bytes.Buffer
	restore := logging.SetOutput(&logs)
	t.Cleanup(restore)
	degraded, err := store.LookupMovie(t.Context(), "ABW-013")
	require.NoError(t, err, "a non-critical join failure must not discard the movie")
	require.Empty(t, degraded.Actresses)
	want := *healthy
	want.Actresses = nil
	require.Equal(t, &want, degraded, "preserve every core field and unaffected join")
	// The iteration-specific wrapper distinguishes rows.Err from a missing
	// table/query error, Scan failure, or a silently swallowed error.
	require.Contains(t, logs.String(), "level=warning")
	require.Contains(t, logs.String(), "actresses lookup failed")
	require.Contains(t, logs.String(), "118abw00013")
	require.Contains(t, logs.String(), "(degraded)")
	require.Contains(t, logs.String(), "iterate actresses")
	require.Contains(t, logs.String(), "integer overflow")
	require.NotContains(t, logs.String(), "lookup cancelled")

	_, err = writer.Exec(`DROP VIEW actresses;
		ALTER TABLE iteration_fixture_actresses RENAME TO actresses`)
	require.NoError(t, err)
	logs.Reset()
	recovered, err := store.LookupMovie(t.Context(), "ABW-013")
	require.NoError(t, err)
	require.Equal(t, healthy, recovered, "the same store recovers when its owned schema fault is removed")
	require.Empty(t, logs.String(), "healthy lookup must not retain the degraded warning")
}
