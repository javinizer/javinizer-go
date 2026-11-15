package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

// Each case uses the actual SQLite driver, read-only URI, transaction and rows.
// synctest.Wait observes database/sql's cancellation cleanup without timing sleeps.
func inspectionDatabase(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "names.db")
	seed, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = seed.Close() })
	_, err = seed.Exec("CREATE TABLE sample (id INTEGER PRIMARY KEY, name TEXT); INSERT INTO sample VALUES (1, 'first'), (2, 'second')")
	require.NoError(t, err)
	require.NoError(t, seed.Close())
	// SQLite URI paths use forward slashes and /C:/... for Windows drives.
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro&_query_only=1&_busy_timeout=10000"}
	db, err := sql.Open("sqlite3", dsn.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func inspectionTransaction(t *testing.T, db *sql.DB, ctx context.Context) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(ctx, "INSERT INTO sample VALUES (3, 'must not write')")
	require.ErrorContains(t, err, "readonly")
	return tx
}

func requireInspectionReleased(t *testing.T, db *sql.DB, tx *sql.Tx) {
	t.Helper()
	require.ErrorIs(t, tx.Rollback(), sql.ErrTxDone)
	require.Zero(t, db.Stats().InUse)
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM sample").Scan(&count))
	require.Equal(t, 2, count, "inspection must preserve the read-only database")
}

// This observer never fabricates a driver result or error. At the selected real
// row, cancellation closes Rows through database/sql before Scan resumes, exactly
// the interleaving possible when inspect's fifteen-second context expires.
type observedInspectionRows struct {
	*sql.Rows
	cancelAtNext int
	cancel       context.CancelFunc
	columnsCalls int
	nextCalls    int
	scanCalls    int
	errCalls     int
	closeCalls   int
}

func (r *observedInspectionRows) Columns() ([]string, error) {
	r.columnsCalls++
	return r.Rows.Columns()
}
func (r *observedInspectionRows) Next() bool {
	r.nextCalls++
	ok := r.Rows.Next()
	if ok && r.nextCalls == r.cancelAtNext {
		r.cancel()
		synctest.Wait()
	}
	return ok
}
func (r *observedInspectionRows) Scan(dest ...any) error {
	r.scanCalls++
	return r.Rows.Scan(dest...)
}
func (r *observedInspectionRows) Err() error {
	r.errCalls++
	return r.Rows.Err()
}
func (r *observedInspectionRows) Close() error {
	r.closeCalls++
	return r.Rows.Close()
}

func TestInspectionRowsCancellationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cancelNext int
	}{{"before-columns", 0}, {"before-first-scan", 1}, {"discard-partial-result", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := inspectionDatabase(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				tx := inspectionTransaction(t, db, ctx)
				rows, err := tx.QueryContext(ctx, "SELECT id, name FROM sample ORDER BY id")
				require.NoError(t, err)
				observed := &observedInspectionRows{Rows: rows, cancelAtNext: tc.cancelNext, cancel: cancel}
				if tc.cancelNext == 0 {
					cancel()
					synctest.Wait()
				}

				result, err := readInspectionRows(observed)

				require.ErrorIs(t, err, context.Canceled)
				require.Nil(t, result, "a canceled snapshot must not return partial rows")
				require.Equal(t, 1, observed.columnsCalls)
				require.Equal(t, tc.cancelNext, observed.nextCalls)
				require.Equal(t, tc.cancelNext, observed.scanCalls)
				require.Zero(t, observed.errCalls, "Columns/Scan failure must return before iteration error handling")
				require.Equal(t, 1, observed.closeCalls, "the reader still owns its idempotent Close")
				requireInspectionReleased(t, db, tx)
			})
		})
	}
}

func TestInspectionRowsIterationFailureDiscardsPartialResult(t *testing.T) {
	db := inspectionDatabase(t)
	tx := inspectionTransaction(t, db, t.Context())
	rows, err := tx.QueryContext(t.Context(), "SELECT 'first' AS name UNION ALL SELECT abs(-9223372036854775808)")
	require.NoError(t, err)
	observed := &observedInspectionRows{Rows: rows}

	result, err := readInspectionRows(observed)

	require.ErrorContains(t, err, "integer overflow")
	require.Nil(t, result)
	require.Equal(t, 2, observed.nextCalls)
	require.Equal(t, 1, observed.scanCalls)
	require.Equal(t, 1, observed.errCalls)
	require.Equal(t, 1, observed.closeCalls)
	require.NoError(t, tx.Rollback(), "the reader must leave transaction ownership with the caller")
	requireInspectionReleased(t, db, tx)
}

func TestInspectionRowsPreserveValuesAndEmptyArrays(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "values", true: "empty"}[empty], func(t *testing.T) {
			db := inspectionDatabase(t)
			tx := inspectionTransaction(t, db, t.Context())
			query := "SELECT id, CAST(name AS BLOB) AS name, NULL AS missing FROM sample ORDER BY id"
			if empty {
				query = "SELECT id, name FROM sample WHERE 0"
			}
			rows, err := tx.QueryContext(t.Context(), query)
			require.NoError(t, err)
			observed := &observedInspectionRows{Rows: rows}

			result, err := readInspectionRows(observed)

			require.NoError(t, err)
			if empty {
				require.Equal(t, []map[string]any{}, result)
			} else {
				require.Equal(t, []map[string]any{
					{"id": int64(1), "name": "first", "missing": nil},
					{"id": int64(2), "name": "second", "missing": nil},
				}, result)
			}
			require.Equal(t, 1, observed.errCalls)
			require.Equal(t, 1, observed.closeCalls)
			require.NoError(t, tx.Rollback(), "the reader must not commit or roll back the caller's transaction")
			requireInspectionReleased(t, db, tx)
		})
	}
}

func TestInspectionCommitCancellationWritesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := inspectionDatabase(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		tx := inspectionTransaction(t, db, ctx)
		rows, err := tx.QueryContext(ctx, "SELECT id, name FROM sample ORDER BY id")
		require.NoError(t, err)
		actors, err := readInspectionRows(rows)
		require.NoError(t, err)
		require.Len(t, actors, 2)
		cancel()
		synctest.Wait() // The real transaction has rolled back before Commit.
		var output bytes.Buffer

		err = commitInspection(tx, map[string][]map[string]any{"actors": actors}, &output)

		require.ErrorIs(t, err, sql.ErrTxDone)
		require.Empty(t, output.String(), "do not publish a snapshot from an aborted transaction")
		requireInspectionReleased(t, db, tx)
	})
}

type inspectionCommitWriter struct {
	bytes.Buffer
	tx        *sql.Tx
	writes    int
	commitErr error
	writeErr  error
}

func (w *inspectionCommitWriter) Write(p []byte) (int, error) {
	w.writes++
	_, w.commitErr = w.tx.Exec("SELECT 1")
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.Buffer.Write(p)
}

func TestInspectionCommitPrecedesOutput(t *testing.T) {
	for _, failWriter := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "writer-error"}[failWriter], func(t *testing.T) {
			db := inspectionDatabase(t)
			tx := inspectionTransaction(t, db, t.Context())
			writer := &inspectionCommitWriter{tx: tx}
			if failWriter {
				writer.writeErr = errors.New("inspection output closed")
			}

			err := commitInspection(tx, map[string][]map[string]any{"actors": {}}, writer)

			if failWriter {
				require.ErrorIs(t, err, writer.writeErr)
				require.Empty(t, writer.String())
			} else {
				require.NoError(t, err)
				require.Equal(t, "{\"actors\":[]}\n", writer.String())
			}
			require.Equal(t, 1, writer.writes)
			require.ErrorIs(t, writer.commitErr, sql.ErrTxDone, "the transaction must be finished before the first output write")
			requireInspectionReleased(t, db, tx)
		})
	}
}
