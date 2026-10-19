package r18devdump

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenContext_CancelledBeforeFirstProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "missing", "dump.db")

	store, err := OpenContext(ctx, path)
	require.Nil(t, store)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, err.Error(), "ping dump db")
	require.NoFileExists(t, path)
}

func TestValidateSidecar_CancelledBeforeFirstProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "missing", "dump.db")

	store, err := ValidateSidecar(ctx, path)
	require.Nil(t, store)
	require.ErrorIs(t, err, ErrDumpInvalid)
	require.Contains(t, err.Error(), context.Canceled.Error())
	require.NoFileExists(t, path)
}

func TestOpenContext_FirstReadinessHonorsContext(t *testing.T) {
	for _, phase := range []string{"connect", "ping"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan context.Context, 1)
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			conn := &openProbeConn{}
			connector := &openProbeConnector{conn: conn}
			wait := func(got context.Context) error {
				entered <- got
				select {
				case <-got.Done():
					return got.Err()
				case <-release:
					return errors.New("probe manually released")
				case <-time.After(5 * time.Second):
					return errors.New("probe safety deadline exceeded")
				}
			}
			if phase == "connect" {
				connector.probe = wait
			} else {
				conn.probe = wait
			}
			db := sql.OpenDB(connector)
			defer func() { unblock(); _ = db.Close() }()
			result := make(chan error, 1)
			go func() {
				store, err := openContextWithOpenDB(ctx, "synthetic.db", func(_, _ string) (*sql.DB, error) {
					return db, nil
				})
				if store != nil {
					_ = store.Close()
				}
				result <- err
			}()
			var received context.Context
			select {
			case received = <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("initial readiness probe did not start")
			}
			if received != ctx {
				t.Error("first readiness probe did not receive the supplied context")
				unblock()
			}
			cancel()
			select {
			case err := <-result:
				require.ErrorIs(t, err, context.Canceled)
				require.Contains(t, err.Error(), "ping dump db")
			case <-time.After(5 * time.Second):
				t.Fatal("cancellation did not join the cooperative probe")
			}
			require.ErrorContains(t, db.PingContext(context.Background()), "database is closed")
			require.EqualValues(t, 1, connector.calls.Load())
			if phase == "ping" {
				require.EqualValues(t, 1, conn.pings.Load())
				require.EqualValues(t, 1, conn.closes.Load())
			} else {
				require.Zero(t, conn.pings.Load())
				require.Zero(t, conn.closes.Load())
			}
		})
	}
}

func TestOpenContext_ReadinessFailureClosesDatabase(t *testing.T) {
	failure := errors.New("synthetic readiness failure")
	conn := &openProbeConn{probe: func(context.Context) error { return failure }}
	db := sql.OpenDB(&openProbeConnector{conn: conn})
	defer db.Close()
	store, err := openContextWithOpenDB(context.Background(), "synthetic.db", func(_, _ string) (*sql.DB, error) {
		return db, nil
	})
	require.Nil(t, store)
	require.ErrorIs(t, err, failure)
	require.Contains(t, err.Error(), "ping dump db")
	require.EqualValues(t, 1, conn.closes.Load())
	require.ErrorContains(t, db.PingContext(context.Background()), "database is closed")
}

func TestOpenContext_DoneContextDoesNotConnect(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if expired {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
			}
			defer cancel()
			cancel()
			conn := &openProbeConn{}
			connector := &openProbeConnector{conn: conn}
			db := sql.OpenDB(connector)
			defer db.Close()
			store, err := openContextWithOpenDB(ctx, "synthetic.db", func(_, _ string) (*sql.DB, error) { return db, nil })
			require.Nil(t, store)
			require.ErrorIs(t, err, ctx.Err())
			require.Zero(t, connector.calls.Load(), "a done context must not begin filesystem work")
			require.Zero(t, conn.pings.Load())
			require.ErrorContains(t, db.PingContext(context.Background()), "database is closed")
		})
	}
}

func TestOpenContext_OpenFailureKeepsError(t *testing.T) {
	failure := errors.New("synthetic open failure")
	store, err := openContextWithOpenDB(context.Background(), "synthetic.db", func(driver, dsn string) (*sql.DB, error) {
		require.Equal(t, "sqlite3", driver)
		require.Equal(t, "file:synthetic.db?mode=ro&_busy_timeout=5000", dsn)
		return nil, failure
	})
	require.Nil(t, store)
	require.ErrorIs(t, err, failure)
	require.Contains(t, err.Error(), "open dump db")
}

func TestOpenContext_SingleProbeKeepsStoreAliveAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &openProbeConn{}
	connector := &openProbeConnector{conn: conn}
	db := sql.OpenDB(connector)
	store, err := openContextWithOpenDB(ctx, "synthetic.db", func(_, _ string) (*sql.DB, error) {
		return db, nil
	})
	require.NoError(t, err)
	defer store.Close()
	require.Equal(t, "synthetic.db", store.path)
	require.Same(t, db, store.db)
	require.EqualValues(t, 1, conn.pings.Load(), "open must perform exactly one readiness check")
	cancel()
	require.NoError(t, store.db.PingContext(context.Background()))
	require.Zero(t, conn.closes.Load(), "the opening context must not own the store lifetime")
	require.NoError(t, store.Close())
	require.EqualValues(t, 1, conn.closes.Load())
}

func TestOpenContext_SQLiteStoreOutlivesOpeningContext(t *testing.T) {
	for _, validate := range []bool{false, true} {
		t.Run(fmt.Sprint(validate), func(t *testing.T) {
			path := importFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			open := OpenContext
			if validate {
				open = ValidateSidecar
			}
			store, err := open(ctx, path)
			require.NoError(t, err)
			defer store.Close()
			cancel()
			stats, err := store.Stats(context.Background())
			require.NoError(t, err)
			require.Positive(t, stats.RowCount)
			require.NoError(t, store.Close())
			require.NoError(t, os.Rename(path, path+".closed"))
		})
	}
}

func TestOpenContext_CancellationDuringSuccessfulProbeClosesDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &openProbeConn{probe: func(context.Context) error { cancel(); return nil }}
	db := sql.OpenDB(&openProbeConnector{conn: conn})
	defer db.Close()
	store, err := openContextWithOpenDB(ctx, "synthetic.db", func(_, _ string) (*sql.DB, error) { return db, nil })
	require.Nil(t, store)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, err.Error(), "ping dump db")
	require.EqualValues(t, 1, conn.pings.Load())
	require.EqualValues(t, 1, conn.closes.Load())
	require.ErrorContains(t, db.PingContext(context.Background()), "database is closed")
}

type openProbeConnector struct {
	conn  *openProbeConn
	probe func(context.Context) error
	calls atomic.Int64
}

func (c *openProbeConnector) Connect(ctx context.Context) (driver.Conn, error) {
	c.calls.Add(1)
	if c.probe != nil {
		if err := c.probe(ctx); err != nil {
			return nil, err
		}
	}
	return c.conn, nil
}
func (c *openProbeConnector) Driver() driver.Driver { return c }
func (c *openProbeConnector) Open(string) (driver.Conn, error) {
	return nil, errors.New("unexpected context-free driver open")
}

type openProbeConn struct {
	probe  func(context.Context) error
	pings  atomic.Int64
	closes atomic.Int64
}

func (c *openProbeConn) Ping(ctx context.Context) error {
	c.pings.Add(1)
	if c.probe != nil {
		return c.probe(ctx)
	}
	return ctx.Err()
}
func (c *openProbeConn) Close() error { c.closes.Add(1); return nil }
func (c *openProbeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (c *openProbeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected Begin")
}
