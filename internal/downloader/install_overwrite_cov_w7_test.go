package downloader

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const covW7DownloaderAttempts = 64

type covW7DownloaderRenameFS struct {
	afero.Fs
	dest        string
	renamedFrom string
}

func (f *covW7DownloaderRenameFS) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) {
		f.renamedFrom = oldname
	}
	return f.Fs.Rename(oldname, newname)
}

func resetDownloaderRestoreCounterForW7(t *testing.T) {
	t.Helper()
	previous := restoreCopyOrdinal.Load()
	restoreCopyOrdinal.Store(0)
	t.Cleanup(func() { restoreCopyOrdinal.Store(previous) })
}

func TestDownloaderCoverageW7_RestoreStageCollisionRetry(t *testing.T) {
	resetDownloaderRestoreCounterForW7(t)
	base := afero.NewMemMapFs()
	dest := "/out/W7-COLLISION/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("restored"), 0o644))
	require.NoError(t, afero.WriteFile(base, dest+".dlrstr.1", []byte("occupied-one"), 0o644))
	require.NoError(t, afero.WriteFile(base, dest+".dlrstr.2", []byte("occupied-two"), 0o644))

	fs := &covW7DownloaderRenameFS{Fs: base, dest: dest}
	require.NoError(t, copyBackupToDest(fs, backup, dest))
	require.Equal(t, dest+".dlrstr.3", fs.renamedFrom, "first free staging slot must be used")
	require.Equal(t, "restored", string(mustReadDownloaderW7(t, base, dest)))
	require.Equal(t, "occupied-one", string(mustReadDownloaderW7(t, base, dest+".dlrstr.1")))
	require.Equal(t, "occupied-two", string(mustReadDownloaderW7(t, base, dest+".dlrstr.2")))
	_, err := base.Stat(dest + ".dlrstr.3")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDownloaderCoverageW7_RestoreStageSymlinkRetry(t *testing.T) {
	baseDir := t.TempDir()
	dest := filepath.Join(baseDir, "poster.jpg")
	backup := dest + ".dlbak.0123456789abcdef"
	victim := filepath.Join(baseDir, "victim.bin")
	link := dest + ".dlrstr.1"
	if err := os.WriteFile(victim, []byte("victim-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("restored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	resetDownloaderRestoreCounterForW7(t)

	fs := &covW7DownloaderRenameFS{Fs: afero.NewOsFs(), dest: dest}
	require.NoError(t, copyBackupToDest(fs, backup, dest))
	require.Equal(t, dest+".dlrstr.2", fs.renamedFrom, "symlink slot must be skipped")
	require.Equal(t, "victim-bytes", string(mustReadDownloaderW7(t, fs, victim)))
	require.Equal(t, "restored", string(mustReadDownloaderW7(t, fs, dest)))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the colliding symlink must remain a symlink")
}

func TestDownloaderCoverageW7_RestoreStageExhaustion(t *testing.T) {
	resetDownloaderRestoreCounterForW7(t)
	fs := afero.NewMemMapFs()
	dest := "/out/W7-EXHAUST/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("restored"), 0o644))
	for i := 1; i <= covW7DownloaderAttempts; i++ {
		name := dest + ".dlrstr." + strconv.FormatUint(uint64(i), 16)
		require.NoError(t, afero.WriteFile(fs, name, []byte("occupied-"+strconv.Itoa(i)), 0o644))
	}

	err := copyBackupToDest(fs, backup, dest)
	require.ErrorContains(t, err, "stage rollback")
	require.ErrorContains(t, err, "exhausted")
	require.Equal(t, "current", string(mustReadDownloaderW7(t, fs, dest)))
	require.Equal(t, "occupied-1", string(mustReadDownloaderW7(t, fs, dest+".dlrstr.1")))
	require.Equal(t, "occupied-64", string(mustReadDownloaderW7(t, fs, dest+".dlrstr.40")))
}

func mustReadDownloaderW7(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}
