package r18devdump

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A blocked driver rollback makes the cancellation/cleanup ordering observable
// on every OS, rather than relying on Windows file-sharing timing.
func TestImport_CancellationWaitsForRollback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newImportLifecycleDriver()
	releaseRollback := sync.OnceFunc(func() { close(d.releaseRollback) })
	defer releaseRollback()
	entered := make(chan struct{})
	releaseRead := make(chan struct{})
	releaseReader := sync.OnceFunc(func() { close(releaseRead) })
	defer releaseReader()
	path := filepath.Join(t.TempDir(), "dump.db")
	original := []byte("existing dump must remain untouched")
	require.NoError(t, os.WriteFile(path, original, 0600))
	result := make(chan error, 1)
	go func() {
		_, err := importWithOpenDB(ctx, lifecycleReader(func([]byte) (int, error) {
			close(entered)
			<-releaseRead
			return 0, ctx.Err()
		}), path, ImportOptions{}, d.open)
		result <- err
	}()
	waitLifecycle(t, entered)
	cancel()
	// Force the old implementation's automatic rollback to win before the
	// parser returns. The fixed implementation has no automatic rollback.
	if d.beginCtx.Done() != nil {
		waitLifecycle(t, d.rollbackStarted)
	}
	releaseReader()
	waitLifecycle(t, d.rollbackStarted)
	select {
	case err := <-result:
		t.Fatalf("Import returned before driver rollback finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.FileExists(t, path+".tmp", "cleanup must not run while rollback owns the file")
	select {
	case <-d.closed:
		t.Fatal("database closed before rollback finished")
	default:
	}
	releaseRollback()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Import did not finish after rollback was released")
	}
	waitLifecycle(t, d.closed)
	require.NoFileExists(t, path+".tmp")
	remaining, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, original, remaining)
	require.False(t, d.committed, "canceled imports must never commit")
}

func TestImport_CanceledBeforeCommitRollsBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newImportLifecycleDriver()
	close(d.releaseRollback)
	// Cancel after the final metadata write succeeds, immediately before
	// Import would otherwise commit its transaction.
	writes := 0
	d.exec = func(query string) {
		if strings.HasPrefix(query, "INSERT OR REPLACE INTO dump_meta") {
			writes++
			if writes == 3 {
				cancel()
			}
		}
	}
	path := filepath.Join(t.TempDir(), "dump.db")
	_, err := importWithOpenDB(ctx, strings.NewReader(""), path, ImportOptions{}, d.open)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, d.committed)
	waitLifecycle(t, d.rollbackStarted)
	waitLifecycle(t, d.closed)
	require.NoFileExists(t, path+".tmp")
	require.NoFileExists(t, path)
}

func TestImport_CanceledAfterSchemaDoesNotBegin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newImportLifecycleDriver()
	d.exec = func(string) { cancel() }
	path := filepath.Join(t.TempDir(), "dump.db")
	_, err := importWithOpenDB(ctx, strings.NewReader(""), path, ImportOptions{}, d.open)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, d.beginCtx, "canceled import must not begin a transaction")
	waitLifecycle(t, d.closed)
	require.NoFileExists(t, path+".tmp")
}

func TestImport_CanceledDuringBeginDoesNotRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := newImportLifecycleDriver()
	close(d.releaseRollback)
	d.begin = cancel
	path := filepath.Join(t.TempDir(), "dump.db")
	_, err := importWithOpenDB(ctx, lifecycleReader(func([]byte) (int, error) {
		t.Error("canceled import must not start reading its stream")
		return 0, context.Canceled
	}), path, ImportOptions{}, d.open)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, d.committed)
	waitLifecycle(t, d.rollbackStarted)
	waitLifecycle(t, d.closed)
	require.NoFileExists(t, path+".tmp")
}

// Exercise the real SQLite driver as well as the forced lifecycle interleaving.
func TestImport_CanceledStreamCleansUpAndRetries(t *testing.T) {
	path := seedDump(t, "118ipx00535\tIPX-535")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	data := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00536\tIPX-536\n\\.\n"
	_, err = Import(ctx, &cancelingReader{data: data, cancel: cancel}, path, ImportOptions{})
	require.ErrorIs(t, err, context.Canceled)
	for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
		require.NoFileExists(t, path+suffix)
	}
	remaining, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, remaining)
	result, err := Import(context.Background(), strings.NewReader(data), path, ImportOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Rows)
	for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
		require.NoFileExists(t, path+suffix)
	}
}

func waitLifecycle(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for import lifecycle event")
	}
}

type lifecycleReader func([]byte) (int, error)

func (r lifecycleReader) Read(p []byte) (int, error) { return r(p) }

type importLifecycleDriver struct {
	beginCtx        context.Context
	rollbackStarted chan struct{}
	releaseRollback chan struct{}
	closed          chan struct{}
	committed       bool
	exec            func(string)
	begin           func()
}

func newImportLifecycleDriver() *importLifecycleDriver {
	return &importLifecycleDriver{rollbackStarted: make(chan struct{}), releaseRollback: make(chan struct{}), closed: make(chan struct{})}
}
func (d *importLifecycleDriver) open(_, dsn string) (*sql.DB, error) {
	path, _, _ := strings.Cut(dsn, "?")
	if err := os.WriteFile(path, []byte("open database"), 0600); err != nil {
		return nil, err
	}
	return sql.OpenDB(importLifecycleConnector{d}), nil
}

type importLifecycleConnector struct{ d *importLifecycleDriver }

func (c importLifecycleConnector) Connect(context.Context) (driver.Conn, error) { return c.d, nil }
func (c importLifecycleConnector) Driver() driver.Driver                        { return c.d }
func (d *importLifecycleDriver) Open(string) (driver.Conn, error)               { return d, nil }
func (d *importLifecycleDriver) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (d *importLifecycleDriver) Close() error { close(d.closed); return nil }
func (d *importLifecycleDriver) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected Begin")
}
func (d *importLifecycleDriver) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	d.beginCtx = ctx
	if d.begin != nil {
		d.begin()
	}
	return d, nil
}
func (d *importLifecycleDriver) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.exec != nil {
		d.exec(query)
	}
	return driver.RowsAffected(0), nil
}
func (d *importLifecycleDriver) Commit() error { d.committed = true; return nil }
func (d *importLifecycleDriver) Rollback() error {
	close(d.rollbackStarted)
	<-d.releaseRollback
	return nil
}
