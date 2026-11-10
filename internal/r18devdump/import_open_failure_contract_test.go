package r18devdump

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImportOpenFailurePreservesPublishedDumpAndRetries(t *testing.T) {
	path := seedDump(t, "old00001\tOLD-001")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	originalInfo, err := os.Stat(path)
	require.NoError(t, err)
	failure := errors.New("dump connection unavailable")
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
		return nil, failure
	})
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "open temp dump db:")
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
