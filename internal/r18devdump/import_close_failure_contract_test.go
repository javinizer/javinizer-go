package r18devdump

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestImportCloseFailurePreservesPublishedDumpAndRetries(t *testing.T) {
	path := seedDump(t, "old00001\tOLD-001")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	// File.Stat captures the Windows file ID immediately; path-based Stat
	// would resolve it lazily after the import and could mask replacement.
	originalFile, err := os.Open(path)
	require.NoError(t, err)
	originalInfo, statErr := originalFile.Stat()
	closeErr := originalFile.Close()
	require.NoError(t, statErr)
	require.NoError(t, closeErr)
	failure := errors.New("dump connection close failed")
	calls := 0
	beforeSwap, afterSwap := false, false
	const incoming = "COPY public.derived_video (content_id, dvd_id) FROM stdin;\nnew00001\tNEW-001\n\\.\n"
	result, err := importWithOpenDB(t.Context(), strings.NewReader(incoming), path, ImportOptions{
		BeforeSwap: func() error { beforeSwap = true; return nil },
		AfterSwap:  func() { afterSwap = true },
	}, func(driverName, dsn string) (*sql.DB, error) {
		calls++
		require.Equal(t, "sqlite3", driverName)
		require.Equal(t, path+".tmp?_journal_mode=WAL&_busy_timeout=5000", dsn)
		return sql.OpenDB(&closeFailureConnector{dsn: dsn, failure: failure}), nil
	})
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "close temp dump db:")
	require.Equal(t, ImportResult{}, result)
	require.Equal(t, 1, calls)
	require.False(t, beforeSwap)
	require.False(t, afterSwap)
	remaining, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, remaining)
	remainingInfo, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(originalInfo, remainingInfo))
	for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
		require.NoFileExists(t, path+suffix)
	}
	result, err = Import(t.Context(), strings.NewReader(incoming), path, ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, ImportResult{Rows: 1, Path: path}, result)
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	id, err := store.LookupByContentID(t.Context(), "new00001")
	require.NoError(t, err)
	require.Equal(t, "NEW-001", id)
}

// Keep real SQLite transactions, checkpointing and close behavior. Only the
// driver close verdict is fault-injected, through database/sql's supported
// connector boundary, to verify publication never follows a close failure.
type closeFailureConnector struct {
	dsn     string
	failure error
}

func (c *closeFailureConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := (&sqlite3.SQLiteDriver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &closeFailureSQLiteConn{SQLiteConn: conn.(*sqlite3.SQLiteConn), failure: c.failure}, nil
}
func (c *closeFailureConnector) Driver() driver.Driver { return &sqlite3.SQLiteDriver{} }

type closeFailureSQLiteConn struct {
	*sqlite3.SQLiteConn
	failure error
}

func (c *closeFailureSQLiteConn) Close() error {
	return errors.Join(c.SQLiteConn.Close(), c.failure)
}
