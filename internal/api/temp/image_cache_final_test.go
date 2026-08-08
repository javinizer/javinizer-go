package temp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageFinal_ResolveAllEntries_MissingDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	_, _, ok := resolveAllEntries(fs, "/nonexistent-dir", "abcd1234")
	assert.False(t, ok)
}

func TestCoverageFinal_Get_ReadDirErr(t *testing.T) {
	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	rawURL := "http://example.com/img.jpg"
	shardDir, hashPrefix := pathFor(tempDir, rawURL)
	require.NoError(t, fs.MkdirAll(shardDir, 0o755))
	entryPath := filepath.Join(shardDir, hashPrefix+".jpg")
	require.NoError(t, afero.WriteFile(fs, entryPath, []byte("data"), 0o644))
	require.NoError(t, fs.Remove(entryPath))
	file, _, _, state := get(fs, tempDir, rawURL, time.Hour)
	assert.Nil(t, file)
	assert.Equal(t, CacheAbsent, state)
}

func TestCoverageFinal_FetchAndCache_SVGRejectedNotMislabeled(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(jpegBytes("<svg></svg>"))
	}))
	t.Cleanup(upstream.Close)

	fs := afero.NewMemMapFs()
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(60 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.svg", upstream.URL+"/img.svg", client, "test-agent", "", 0)
	require.Error(t, result.err)
	assert.Contains(t, result.err.Error(), "unsupported image content type")
	assert.Empty(t, result.cachedPath)
}

func TestCoverageFinal_AtomicRename_IsExistRetry(t *testing.T) {
	fs := &failingRenameFs{Fs: afero.NewMemMapFs(), renameErr: os.ErrExist, maxFails: 1}
	src := "/tmp/src.txt"
	dst := "/tmp/dst.txt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("old"), 0o644))
	err := atomicRename(fs, src, dst)
	require.NoError(t, err)
}
