package core

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type cacheRootOpenFailFs struct {
	afero.Fs
	root  string
	calls *int32
}

func (f *cacheRootOpenFailFs) Open(name string) (afero.File, error) {
	if name == f.root {
		atomic.AddInt32(f.calls, 1)
		return nil, errors.New("root unreadable")
	}
	return f.Fs.Open(name)
}

func TestStartImageCacheCleanup_CleanupErrorLogged(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.ImageCacheEnabled = true
	cfg.System.ImageCacheTTLHours = 1
	cfg.System.TempDir = t.TempDir()

	var calls int32
	fs := &cacheRootOpenFailFs{
		Fs:    afero.NewMemMapFs(),
		root:  filepath.Join(cfg.System.TempDir, "image-cache"),
		calls: &calls,
	}

	deps := &APIDeps{Fs: fs}
	rt := NewAPIRuntime(deps)
	rt.SetConfig(cfg)

	startImageCacheCleanup(rt, imageCacheCleanupOptions{interval: time.Hour})
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&calls) > 0
	}, 2*time.Second, 5*time.Millisecond, "startup sweep should attempt to read the image-cache root")
	rt.Shutdown()
}
