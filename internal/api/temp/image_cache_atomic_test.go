package temp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type atomicStubFs struct {
	afero.Fs
	renameErrs  []error
	mkdirErr    error
	renameCalls int
}

func (f *atomicStubFs) Rename(oldname, newname string) error {
	idx := f.renameCalls
	f.renameCalls++
	if idx < len(f.renameErrs) && f.renameErrs[idx] != nil {
		return f.renameErrs[idx]
	}
	return f.Fs.Rename(oldname, newname)
}

func (f *atomicStubFs) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	return f.Fs.MkdirAll(path, perm)
}

func TestAtomicRename_NotExistBranch_MkdirsAndRetries(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/tmp/src.bin", []byte("data"), 0o644))
	fs := &atomicStubFs{Fs: base, renameErrs: []error{os.ErrNotExist}}

	err := atomicRename(fs, "/tmp/src.bin", "/new/dir/dst.bin")
	require.NoError(t, err)
	exists, _ := afero.Exists(base, "/new/dir/dst.bin")
	assert.True(t, exists)
}

func TestAtomicRename_MkdirAllFailure_ReturnsError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/tmp/src.bin", []byte("data"), 0o644))
	fs := &atomicStubFs{Fs: base, renameErrs: []error{os.ErrNotExist}, mkdirErr: errors.New("mkdir denied")}

	err := atomicRename(fs, "/tmp/src.bin", "/new/dir/dst.bin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir denied")
}

func TestAtomicRename_IsExistBranch_RemovesAndRetries(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/tmp/src.bin", []byte("data"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/tmp/dst.bin", []byte("old"), 0o644))
	fs := &atomicStubFs{Fs: base, renameErrs: []error{os.ErrExist}}

	err := atomicRename(fs, "/tmp/src.bin", "/tmp/dst.bin")
	require.NoError(t, err)
	data, err := afero.ReadFile(base, "/tmp/dst.bin")
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), data)
}

func TestAtomicRename_GenericError_ReturnsError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/tmp/src.bin", []byte("data"), 0o644))
	fs := &atomicStubFs{Fs: base, renameErrs: []error{errors.New("io error")}}

	err := atomicRename(fs, "/tmp/src.bin", "/tmp/dst.bin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "io error")
}

type renameAlwaysFailFs struct {
	afero.Fs
}

func (f *renameAlwaysFailFs) Rename(oldname, newname string) error {
	return errors.New("cross-device link")
}

func TestFetch_GenericRenameError_ReturnsError(t *testing.T) {
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes("rename-fail-test"))
	}))
	t.Cleanup(upstream.Close)

	fs := &renameAlwaysFailFs{Fs: afero.NewMemMapFs()}
	tempDir := t.TempDir()
	client := ssrf.NewSSRFSafeClient(30 * time.Second)

	result := fetchAndCache(context.Background(), fs, tempDir, upstream.URL+"/img.jpg", upstream.URL+"/img.jpg", client, "test-agent", "", 0)
	assert.NoError(t, result.err)
	assert.Empty(t, result.cachedPath)
	assert.True(t, result.persistFailed)
	assert.Equal(t, jpegBytes("rename-fail-test"), result.body)

	var leftovers []string
	_ = afero.Walk(fs, "/", func(p string, info os.FileInfo, werr error) error {
		if werr == nil && info != nil && !info.IsDir() && strings.Contains(p, ".tmp") {
			leftovers = append(leftovers, p)
		}
		return nil
	})
	assert.Empty(t, leftovers, "temp artifact must be reclaimed when the rename fails")
}
