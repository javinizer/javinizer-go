package fscase

import (
	"io"
	"os"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestIsCaseInsensitiveFS_FirstWriteFails(t *testing.T) {
	cache := NewFSCaseCache(afero.NewReadOnlyFs(afero.NewMemMapFs()))
	result := cache.isCaseInsensitiveFS("/test")
	assert.False(t, result, "should return false when first write fails")
}

func TestIsCaseInsensitiveFS_SecondWriteFails(t *testing.T) {
	memfs := afero.NewMemMapFs()
	fs := &secondWriteFailsFs{Fs: memfs}
	cache := NewFSCaseCache(fs)
	cache.fs.MkdirAll("/test", 0755)
	result := cache.isCaseInsensitiveFS("/test")
	assert.False(t, result, "should return false when second write fails")
}

func TestIsCaseInsensitiveFS_ReadFails(t *testing.T) {
	memfs := afero.NewMemMapFs()
	fs := &noReadFs{Fs: memfs}
	cache := NewFSCaseCache(fs)
	cache.fs.MkdirAll("/test", 0755)
	result := cache.isCaseInsensitiveFS("/test")
	assert.False(t, result, "should return false when read fails")
}

func TestRandomProbeToken_Normal(t *testing.T) {
	token := randomProbeToken()
	assert.Len(t, token, 16, "normal token should be 16 hex chars")
}

func TestRandomProbeToken_Fallback(t *testing.T) {
	original := randReader
	randReader = &errorReader{}
	defer func() { randReader = original }()

	token := randomProbeToken()
	assert.Equal(t, "fallback", token, "should return fallback when rand fails")
}

func TestIsCaseInsensitive_ConcurrentDoubleCheckCacheHit(t *testing.T) {
	cache := NewFSCaseCache(afero.NewMemMapFs())
	tmpDir := t.TempDir()
	cache.fs.MkdirAll(tmpDir, 0755)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cache.IsCaseInsensitive(tmpDir)
		}()
	}
	wg.Wait()

	cache.mu.RLock()
	_, exists := cache.cache[tmpDir]
	cache.mu.RUnlock()
	assert.True(t, exists, "result should be cached after concurrent access")
}

type errorReader struct{}

func (*errorReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

type secondWriteFailsFs struct {
	afero.Fs
	openCount int
}

func (f *secondWriteFailsFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f.openCount++
	if f.openCount >= 2 {
		return nil, os.ErrPermission
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type noReadFs struct {
	afero.Fs
}

func (f *noReadFs) Open(name string) (afero.File, error) {
	return nil, os.ErrPermission
}
