package core

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartImageCacheCleanupCtxCancel(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 168
	cfg.System.TempDir = t.TempDir()
	fs := afero.NewMemMapFs()
	deps := &APIDeps{Fs: fs}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)

	startImageCacheCleanup(rt, imageCacheCleanupOptions{interval: 100 * time.Millisecond})
	time.Sleep(200 * time.Millisecond)

	rt.Shutdown()
	time.Sleep(100 * time.Millisecond)
}

func TestStartImageCacheCleanupNoopOnTTLZero(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = false
	cfg.System.ImageCacheTTLHours = 0
	cfg.System.TempDir = t.TempDir()
	fs := afero.NewMemMapFs()
	deps := &APIDeps{Fs: fs}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)

	startImageCacheCleanup(rt, imageCacheCleanupOptions{interval: 100 * time.Millisecond})
	time.Sleep(150 * time.Millisecond)

	rt.Shutdown()
}

func TestStartImageCacheCleanup_RemovesExpiredEntries(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 168
	cfg.System.TempDir = t.TempDir()
	fs := afero.NewMemMapFs()

	// Pre-populate the cache with an expired entry
	shardDir := cfg.System.TempDir + "/image-cache/ab"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	expired := shardDir + "/deadbeef1234.jpg"
	require.NoError(t, afero.WriteFile(fs, expired, []byte("old"), 0o644))
	pastTime := time.Now().Add(-200 * time.Hour)
	require.NoError(t, fs.Chtimes(expired, pastTime, pastTime))

	deps := &APIDeps{Fs: fs}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)

	startImageCacheCleanup(rt, imageCacheCleanupOptions{interval: 100 * time.Millisecond})
	time.Sleep(250 * time.Millisecond)
	rt.Shutdown()

	exists, _ := afero.Exists(fs, expired)
	assert.False(t, exists, "expired entry should be removed by startup sweep")
}

func TestStartImageCacheCleanup_PreservesFreshEntries(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 168
	cfg.System.TempDir = t.TempDir()
	fs := afero.NewMemMapFs()

	shardDir := cfg.System.TempDir + "/image-cache/cd"
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	fresh := shardDir + "/deadbeef5678.png"
	require.NoError(t, afero.WriteFile(fs, fresh, []byte("new"), 0o644))

	deps := &APIDeps{Fs: fs}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)

	startImageCacheCleanup(rt, imageCacheCleanupOptions{interval: 100 * time.Millisecond})
	time.Sleep(150 * time.Millisecond)
	rt.Shutdown()

	exists, _ := afero.Exists(fs, fresh)
	assert.True(t, exists, "fresh entry should be preserved")
}
