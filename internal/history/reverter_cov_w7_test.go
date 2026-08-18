package history

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const covW7HistoryAttempts = 64

type covW7HistoryRenameFS struct {
	afero.Fs
	dest        string
	renamedFrom string
}

func (f *covW7HistoryRenameFS) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) {
		f.renamedFrom = oldname
	}
	return f.Fs.Rename(oldname, newname)
}

func resetHistoryRestoreCounterForW7(t *testing.T) {
	t.Helper()
	previous := restoreCopyNonce.Load()
	restoreCopyNonce.Store(0)
	t.Cleanup(func() { restoreCopyNonce.Store(previous) })
}

func TestReverterCoverageW7_RestoreStageCollisionRetry(t *testing.T) {
	resetHistoryRestoreCounterForW7(t)
	base := afero.NewMemMapFs()
	dest := "/out/W7-COLLISION/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(base, dest, []byte("current"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, backup, []byte("restored"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, dest+".rstr.1", []byte("occupied-one"), config.FilePerm))
	require.NoError(t, afero.WriteFile(base, dest+".rstr.2", []byte("occupied-two"), config.FilePerm))

	fs := &covW7HistoryRenameFS{Fs: base, dest: dest}
	require.NoError(t, copyRestoreBytes(fs, backup, dest))
	// Wave-11's restoreOSPath normalizes dest once at copyRestoreBytes entry,
	// so the staged rename surfaces in PLATFORM-NATIVE spelling on Windows
	// (\out\W7-COLLISION\poster.jpg.rstr.3) while POSIX keeps the journal slash
	// spelling. Compare separator-agnostically; the native spelling is
	// intentional production behavior.
	require.Equal(t, filepath.ToSlash(dest+".rstr.3"), filepath.ToSlash(fs.renamedFrom),
		"first free staging slot must be used")
	require.Equal(t, "restored", string(mustRead2(t, base, dest)))
	require.Equal(t, "occupied-one", string(mustRead2(t, base, dest+".rstr.1")))
	require.Equal(t, "occupied-two", string(mustRead2(t, base, dest+".rstr.2")))
	_, err := base.Stat(dest + ".rstr.3")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReverterCoverageW7_RestoreStageSymlinkRetry(t *testing.T) {
	baseDir := t.TempDir()
	dest := filepath.Join(baseDir, "poster.jpg")
	backup := dest + ".dlbak.0123456789abcdef"
	victim := filepath.Join(baseDir, "victim.bin")
	link := dest + ".rstr.1"
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
	resetHistoryRestoreCounterForW7(t)

	fs := &covW7HistoryRenameFS{Fs: afero.NewOsFs(), dest: dest}
	require.NoError(t, copyRestoreBytes(fs, backup, dest))
	require.Equal(t, dest+".rstr.2", fs.renamedFrom, "symlink slot must be skipped")
	require.Equal(t, "victim-bytes", string(mustRead2(t, fs, victim)))
	require.Equal(t, "restored", string(mustRead2(t, fs, dest)))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the colliding symlink must remain a symlink")
}

func TestReverterCoverageW7_RestoreStageExhaustion(t *testing.T) {
	resetHistoryRestoreCounterForW7(t)
	fs := afero.NewMemMapFs()
	dest := "/out/W7-EXHAUST/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dest), config.DirPerm))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), config.FilePerm))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("restored"), config.FilePerm))
	for i := 1; i <= covW7HistoryAttempts; i++ {
		name := dest + ".rstr." + strconv.FormatUint(uint64(i), 16)
		require.NoError(t, afero.WriteFile(fs, name, []byte("occupied-"+strconv.Itoa(i)), config.FilePerm))
	}

	err := copyRestoreBytes(fs, backup, dest)
	require.ErrorContains(t, err, "stage restore open")
	require.ErrorContains(t, err, "exhausted")
	require.Equal(t, "current", string(mustRead2(t, fs, dest)))
	require.Equal(t, "occupied-1", string(mustRead2(t, fs, dest+".rstr.1")))
	require.Equal(t, "occupied-64", string(mustRead2(t, fs, dest+".rstr.40")))
}
