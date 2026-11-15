//go:build linux

package file

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

// Linux returns ENOENT when getdents reads an unlinked directory. DirEntry.Info
// also performs a fresh stat rather than returning cached Windows metadata.
// Keep these actual kernel-race contracts Linux-specific, without fake errors.
func newBrowseRaceRouter(t *testing.T, root string, readDir func(*os.File, int) ([]os.DirEntry, error)) *gin.Engine {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	deps := newTestDepsFromConfig(cfg)
	t.Cleanup(func() { testkit.CleanupTestRuntime(t, deps) })
	router := gin.New()
	router.POST("/browse", browseDirectoryWithReadDir(testkit.GetTestRuntime(deps), readDir))
	return router
}

func TestBrowseDirectoryRemovedAfterValidation(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "source")
	require.NoError(t, os.Mkdir(root, 0o700))
	var opened *os.File
	var readErr error
	reads := 0
	router := newBrowseRaceRouter(t, parent, func(f *os.File, n int) ([]os.DirEntry, error) {
		reads++
		opened = f
		require.Equal(t, -1, n)
		require.Equal(t, root, f.Name())
		if reads == 1 {
			require.NoError(t, os.Remove(root))
		}
		entries, err := f.ReadDir(n)
		readErr = err
		return entries, err
	})

	w := doBrowse(t, router, root, "")
	require.ErrorIs(t, readErr, os.ErrNotExist)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	var response contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, readErr.Error(), response.Error)
	require.NotContains(t, w.Body.String(), `"items"`)
	_, err := opened.Stat()
	require.ErrorIs(t, err, os.ErrClosed, "the handler must close its validated handle on error")
	require.Equal(t, 1, reads, "do not reopen or silently retry after a failed read")

	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "recovered.txt"), []byte("safe"), 0o600))
	w = doBrowse(t, router, root, "")
	require.Equal(t, http.StatusOK, w.Code)
	var recovered contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Len(t, recovered.Items, 1)
	require.Equal(t, "recovered.txt", recovered.Items[0].Name)
	require.Equal(t, root, recovered.CurrentPath)
	require.Equal(t, parent, recovered.ParentPath)
	_, err = opened.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestBrowseEntryRemovedAfterDirectoryRead(t *testing.T) {
	root := t.TempDir()
	removed := filepath.Join(root, "removed.txt")
	retained := filepath.Join(root, "retained.txt")
	require.NoError(t, os.WriteFile(removed, []byte("removed"), 0o600))
	require.NoError(t, os.WriteFile(retained, []byte("retained bytes"), 0o600))
	var opened *os.File
	reads := 0
	router := newBrowseRaceRouter(t, root, func(f *os.File, n int) ([]os.DirEntry, error) {
		reads++
		opened = f
		require.Equal(t, -1, n)
		entries, err := f.ReadDir(n)
		require.NoError(t, err)
		if reads == 1 {
			require.Len(t, entries, 2)
			require.NoError(t, os.Remove(removed))
		}
		return entries, err
	})

	w := doBrowse(t, router, root, "")
	require.Equal(t, http.StatusOK, w.Code)
	var response contracts.BrowseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Items, 1, "a vanished entry must be skipped, not returned with partial metadata")
	require.Equal(t, "retained.txt", response.Items[0].Name)
	require.Equal(t, retained, response.Items[0].Path)
	require.False(t, response.Items[0].IsDir)
	require.EqualValues(t, len("retained bytes"), response.Items[0].Size)
	require.Equal(t, root, response.CurrentPath)
	require.Equal(t, filepath.Dir(root), response.ParentPath)
	_, err := opened.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
	data, err := os.ReadFile(retained)
	require.NoError(t, err)
	require.Equal(t, "retained bytes", string(data))

	require.NoError(t, os.WriteFile(removed, []byte("replacement"), 0o600))
	w = doBrowse(t, router, root, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Items, 2, "a later browse must observe a fresh directory snapshot")
	_, err = opened.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
}
