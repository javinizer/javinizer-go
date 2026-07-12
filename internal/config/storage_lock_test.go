package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withRetryEnabled(t *testing.T) bool {
	t.Helper()
	prev := lockRetryEnabled
	lockRetryEnabled = true
	t.Cleanup(func() { lockRetryEnabled = prev })
	return prev
}

func TestRemoveWithRetry_SucceedsFirstTry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "to-remove")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

	var calls int32
	prev := osRemoveFunc
	osRemoveFunc = func(p string) error {
		atomic.AddInt32(&calls, 1)
		return prev(p)
	}
	t.Cleanup(func() { osRemoveFunc = prev })

	err := removeWithRetry(path)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "should not retry on success")

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRemoveWithRetry_RetriesOnFailure(t *testing.T) {
	withRetryEnabled(t)

	var calls int32
	transient := errors.New("sharing violation")
	prev := osRemoveFunc
	osRemoveFunc = func(p string) error {
		atomic.AddInt32(&calls, 1)
		if atomic.LoadInt32(&calls) < int32(lockRetryAttempts) {
			return transient
		}
		return prev(p)
	}
	t.Cleanup(func() { osRemoveFunc = prev })

	dir := t.TempDir()
	path := filepath.Join(dir, "to-remove")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

	err := removeWithRetry(path)
	require.NoError(t, err)
	assert.Equal(t, int32(lockRetryAttempts), atomic.LoadInt32(&calls),
		"should retry until success")
}

func TestRemoveWithRetry_ExhaustsAttempts(t *testing.T) {
	withRetryEnabled(t)

	var calls int32
	permanent := errors.New("permission denied")
	prev := osRemoveFunc
	osRemoveFunc = func(p string) error {
		atomic.AddInt32(&calls, 1)
		return permanent
	}
	t.Cleanup(func() { osRemoveFunc = prev })

	err := removeWithRetry(filepath.Join(t.TempDir(), "missing"))
	assert.ErrorIs(t, err, permanent)
	assert.Equal(t, int32(lockRetryAttempts), atomic.LoadInt32(&calls),
		"should attempt exactly lockRetryAttempts times")
}

func TestReadFileWithRetry_RetriesThenSucceeds(t *testing.T) {
	withRetryEnabled(t)

	var calls int32
	transient := errors.New("temporarily locked")
	prev := osReadFileFunc
	osReadFileFunc = func(p string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		if atomic.LoadInt32(&calls) < int32(lockRetryAttempts) {
			return nil, transient
		}
		return prev(p)
	}
	t.Cleanup(func() { osReadFileFunc = prev })

	dir := t.TempDir()
	path := filepath.Join(dir, "lock")
	require.NoError(t, os.WriteFile(path, []byte("pid=1,time=2"), 0o600))

	data, err := readFileWithRetry(path)
	require.NoError(t, err)
	assert.Equal(t, "pid=1,time=2", string(data))
	assert.Equal(t, int32(lockRetryAttempts), atomic.LoadInt32(&calls))
}

func TestReadFileWithRetry_DoesNotRetryMissingFile(t *testing.T) {
	withRetryEnabled(t)

	var calls int32
	prev := osReadFileFunc
	osReadFileFunc = func(p string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return prev(p)
	}
	t.Cleanup(func() { osReadFileFunc = prev })

	_, err := readFileWithRetry(filepath.Join(t.TempDir(), "absent"))
	assert.True(t, os.IsNotExist(err))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls),
		"IsNotExist should short-circuit without retry")
}

func TestReleaseConfigFileLock_RetriesRemoveOnWindows(t *testing.T) {
	wasWindows := withRetryEnabled(t)

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "config.yaml.lock")
	token := "pid=42,time=99"
	require.NoError(t, os.WriteFile(lockPath, []byte(token), 0o600))

	var removeCalls int32
	prevRemove := osRemoveFunc
	osRemoveFunc = func(p string) error {
		atomic.AddInt32(&removeCalls, 1)
		if atomic.LoadInt32(&removeCalls) < int32(lockRetryAttempts) {
			return errors.New("sharing violation")
		}
		return prevRemove(p)
	}
	t.Cleanup(func() { osRemoveFunc = prevRemove })

	releaseConfigFileLock(lockPath, token)

	assert.GreaterOrEqual(t, atomic.LoadInt32(&removeCalls), int32(2),
		"release should retry remove on Windows")
	_, statErr := os.Stat(lockPath)
	assert.True(t, os.IsNotExist(statErr), "lock should be removed after retries")

	if !wasWindows {
		t.Logf("ran retry path on non-Windows via seam (lockRetryEnabled override)")
	}
}

func TestReleaseConfigFileLock_TokenMismatchNoRemove(t *testing.T) {
	withRetryEnabled(t)

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "config.yaml.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte("other-token"), 0o600))

	var calls int32
	prev := osRemoveFunc
	osRemoveFunc = func(p string) error {
		atomic.AddInt32(&calls, 1)
		return prev(p)
	}
	t.Cleanup(func() { osRemoveFunc = prev })

	releaseConfigFileLock(lockPath, "my-token")
	assert.Equal(t, int32(0), atomic.LoadInt32(&calls), "should not remove on token mismatch")

	_, err := os.Stat(lockPath)
	assert.NoError(t, err)
}
