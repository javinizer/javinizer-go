//go:build windows

package downloader

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestOpenRestoreSourceWindows_RegularFileReadsThroughReparseOpenedHandle(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak")
	want := []byte("original bytes")
	require.NoError(t, os.WriteFile(backup, want, 0o600))

	f, err := openRestoreSourceWindows(afero.NewOsFs(), backup)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	got, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestOpenRestoreSourceWindows_SymlinkSwapRefusedOnHandle(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "decoy.bin")
	require.NoError(t, os.WriteFile(target, []byte("decoy"), 0o600))
	link := filepath.Join(dir, "poster.jpg.dlbak")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation requires privilege/developer mode: %v", err)
	}

	f, err := openRestoreSourceWindows(afero.NewOsFs(), link)
	require.Nil(t, f)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRestoreSourceRefused), "reparse point must surface the restore-refusal class, got %v", err)
}

func TestOpenRestoreSourceWindows_MissingBackupSurfacesOpenError(t *testing.T) {
	f, err := openRestoreSourceWindows(afero.NewOsFs(), filepath.Join(t.TempDir(), "absent.dlbak"))
	require.Nil(t, f)
	require.Error(t, err)
}

func TestOpenRestoreSourceWindows_NonOsFsFallsBackToPlainOpen(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/bk/poster.jpg.dlbak", []byte("mem"), 0o600))

	f, err := openRestoreSourceWindows(fs, "/bk/poster.jpg.dlbak")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	got, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "mem", string(got))
}

func TestRestoreSourceLongPath(t *testing.T) {
	prefixed := `\\?\C:\already\poster.jpg.dlbak`
	require.Equal(t, prefixed, restoreSourceLongPath(prefixed))

	shortPath := `C:\media\poster.jpg.dlbak`
	require.Equal(t, shortPath, restoreSourceLongPath(shortPath))

	longPath := `C:\` + strings.Repeat(`verylongdirectoryname\`, 30) + `poster.jpg.dlbak`
	require.Greater(t, len(longPath), 260)
	require.Equal(t, `\\?\`+longPath, restoreSourceLongPath(longPath))

	longUNC := `\\server\share\` + strings.Repeat(`verylongdirectoryname\`, 30) + `poster.jpg.dlbak`
	require.Equal(t, `\\?\UNC\server\share\`+longUNC[len(`\\server\share\`):], restoreSourceLongPath(longUNC))
}
