package database

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/require"
)

func TestFileMigrationLockerContextContract(t *testing.T) {
	for _, held := range []bool{false, true} {
		for _, deadline := range []bool{false, true} {
			name := "canceled"
			if deadline {
				name = "deadline"
			}
			if held {
				name += "_held"
			} else {
				name += "_available"
			}
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "migration.lock")
				holder := flock.New(path)
				t.Cleanup(func() { require.NoError(t, holder.Close()) })
				if held {
					require.NoError(t, holder.Lock())
				}
				var ctx context.Context
				var cancel context.CancelFunc
				want := context.Canceled
				if deadline {
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					want = context.DeadlineExceeded
				} else {
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
				}
				defer cancel()
				locker := &fileMigrationLocker{fileLock: flock.New(path)}
				t.Cleanup(func() { require.NoError(t, locker.fileLock.Close()) })
				require.ErrorIs(t, locker.Lock(ctx, nil), want)
				require.False(t, locker.fileLock.Locked())
				require.Equal(t, held, holder.Locked())
			})
		}
	}
}

func TestFileMigrationLockerWaitingContract(t *testing.T) {
	for _, outcome := range []string{"release", "cancel", "deadline"} {
		t.Run(outcome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "migration.lock")
			holder := flock.New(path)
			require.NoError(t, holder.Lock())
			t.Cleanup(func() { require.NoError(t, holder.Close()) })
			locker := &fileMigrationLocker{fileLock: flock.New(path)}
			t.Cleanup(func() { require.NoError(t, locker.fileLock.Close()) })
			ctx, cancel := context.WithCancel(context.Background())
			if outcome == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 3*migrationLockRetry)
			}
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- locker.Lock(ctx, nil) }()
			if outcome != "deadline" {
				select {
				case err := <-result:
					t.Fatalf("lock returned while another holder owns it: %v", err)
				case <-time.After(2 * migrationLockRetry):
				}
			}
			switch outcome {
			case "release":
				require.NoError(t, holder.Unlock())
			case "cancel":
				cancel()
			}
			var err error
			select {
			case err = <-result:
			case <-time.After(6 * time.Second):
				t.Fatal("lock did not finish")
			}
			switch outcome {
			case "release":
				require.NoError(t, err)
				require.True(t, locker.fileLock.Locked())
				acquired, probeErr := holder.TryLock()
				require.NoError(t, probeErr)
				require.False(t, acquired)
				require.NoError(t, locker.Unlock(context.Background(), nil))
				acquired, probeErr = holder.TryLock()
				require.NoError(t, probeErr)
				require.True(t, acquired)
			case "cancel":
				require.ErrorIs(t, err, context.Canceled)
			case "deadline":
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			if outcome != "release" {
				require.False(t, locker.fileLock.Locked())
				require.True(t, holder.Locked())
				require.NoError(t, holder.Unlock())
				require.NoError(t, locker.Lock(context.Background(), nil))
				require.NoError(t, locker.Unlock(context.Background(), nil))
			}
		})
	}
}

func TestFileMigrationLockerFilesystemErrorContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "migration.lock")
	locker := &fileMigrationLocker{fileLock: flock.New(path)}
	t.Cleanup(func() { require.NoError(t, locker.fileLock.Close()) })
	err := locker.Lock(context.Background(), nil)
	require.ErrorIs(t, err, fs.ErrNotExist)
	var pathErr *fs.PathError
	require.True(t, errors.As(err, &pathErr))
	require.Equal(t, path, pathErr.Path)
	require.False(t, locker.fileLock.Locked())
}
