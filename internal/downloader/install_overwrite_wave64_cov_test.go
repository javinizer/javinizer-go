//go:build !windows

package downloader

// POSTER-WRITE-HARDENING wave-64 (codex P2, PR#215) — captureReplacementBackupFacts's
// deferred Chtimes restore stays bound to the opened object: the defer re-Lstats
// the backup name and skips Chtimes unless the CURRENT name still provably names
// the opened bytes (dev/ino via os.SameFile on OsFs; name+size on MemMapFs). A
// mid-open substitution or a re-Lstat fault therefore never chases Chtimes onto
// unrelated bytes, while the unchanged MemMapFs set-aside still gets its pre-read
// mtime restored (the close re-stamp on the non-read-only O_NOFOLLOW handle). The
// three legs below drive the PUBLIC captureReplacementBackupFacts through each
// unclosed branch of that deferred gate.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w64DeferLstatFailFs lets the capture-time Lstat succeed but wedges the
// defer's re-Lstat with a non-ENOENT fault — the defer silently skips Chtimes.
type w64DeferLstatFailFs struct {
	afero.Fs
	victim string
	err    error
	calls  int
}

func (f *w64DeferLstatFailFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if name == f.victim {
		f.calls++
		if f.calls == 2 {
			return nil, false, f.err
		}
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// Leg 1: the defer's re-Lstat faults (non-ENOENT) → Chtimes skipped, facts
// still returned (the read already hashed the opened bytes).
func TestW64_CaptureReplacementBackupFacts_DeferLstatFail(t *testing.T) {
	base := afero.NewMemMapFs()
	backup := "/w64/defer-lstat"
	payload := []byte("w64 defer-lstat payload")
	require.NoError(t, afero.WriteFile(base, backup, payload, 0o644))
	fs := &w64DeferLstatFailFs{Fs: base, victim: backup, err: errors.New("w64 defer lstat wedge")}

	facts, err := captureReplacementBackupFacts(fs, backup)
	require.NoError(t, err, "the defer's Lstat fault silently skips Chtimes; facts still returned")
	require.Equal(t, int64(len(payload)), facts.Size)
	require.Equal(t, w63ShaHex(payload), facts.SHA256, "the read hashed the opened bytes")
	require.Equal(t, 2, fs.calls, "capture Lstat succeeded, the defer's re-Lstat wedged")
}

// w64SwapOnOpenFs swaps the backup path's object the instant the no-follow
// open succeeds — the deterministic form of a directory writer winning the
// open→close window (the open handle still names the original object). Used
// on MemMapFs, where the name/size gate (not the OsFs kernel-identity gate)
// arbitrates the deferred Chtimes.
type w64SwapOnOpenFs struct {
	afero.Fs
	victim string
	swap   func()
	done   atomic.Bool
}

func (f *w64SwapOnOpenFs) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	base, err := f.Fs.OpenFile(name, flags, perm)
	if err != nil {
		return nil, err
	}
	if name == f.victim && f.done.CompareAndSwap(false, true) {
		f.swap()
	}
	return base, nil
}

// Leg 2: OsFs + SameFile mismatch — a foreign writer swaps the backup between
// the Lstat and the close; the strict kernel-identity gate refuses Chtimes
// (the name no longer names the opened inode). The gate keys off
// fsys.(*afero.OsFs), so the swap is injected through the
// restoreOpenReplacementSource seam against a bare OsFs (a wrapper would
// demote the gate onto the name/size branch).
func TestW64_CaptureReplacementBackupFacts_OsFsSameFileMismatch(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak")
	payload := []byte("w64 osfs original backup bytes")
	require.NoError(t, os.WriteFile(backup, payload, 0o644))
	origMtime := time.Unix(1_000_000_000, 0)
	require.NoError(t, os.Chtimes(backup, origMtime, origMtime))

	substitutePath := filepath.Join(dir, "substitute")
	substitute := []byte("w64 osfs substitute of a different length")
	require.NoError(t, os.WriteFile(substitutePath, substitute, 0o644))
	subMtime := time.Unix(2_000_000_000, 0)
	require.NoError(t, os.Chtimes(substitutePath, subMtime, subMtime))

	var swapped atomic.Bool
	fs := afero.NewOsFs() // bare OsFs so fsys.(*afero.OsFs) engages the kernel-identity gate
	prev := restoreOpenReplacementSource
	restoreOpenReplacementSource = func(fsys afero.Fs, backupPath string) (afero.File, error) {
		f, err := prev(fsys, backupPath) // OsFs no-follow open of the original
		if err != nil {
			return nil, err
		}
		if !swapped.Swap(true) {
			// Foreign writer swaps the name mid-read: windows MoveFileW refuses
			// replace, so drive it explicitly through remove+rename.
			if runtime.GOOS == "windows" {
				if rerr := os.Remove(backupPath); rerr != nil {
					return nil, rerr
				}
			}
			if rerr := os.Rename(substitutePath, backupPath); rerr != nil {
				return nil, rerr
			}
		}
		return f, nil
	}
	t.Cleanup(func() { restoreOpenReplacementSource = prev })

	facts, err := captureReplacementBackupFacts(fs, backup)
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), facts.Size, "the open fd hashed the original bytes")
	require.Equal(t, w63ShaHex(payload), facts.SHA256)

	cur, sErr := os.Stat(backup)
	require.NoError(t, sErr)
	require.Equal(t, subMtime.Unix(), cur.ModTime().Unix(),
		"Chtimes never ran — the swapped-in substitute's mtime stands, not the captured original's")
	got, rErr := os.ReadFile(backup)
	require.NoError(t, rErr)
	require.Equal(t, substitute, got, "the substitute now occupies the backup name")
}

// Leg 3: the MemMapFs branch — name+size identical lets Chtimes restore the
// pre-read mtime (the close re-stamp on the non-read-only O_NOFOLLOW handle),
// while a name/size mismatch (a swapped-in substitute) refuses Chtimes.
func TestW64_CaptureReplacementBackupFacts_MemMapFsBranchLegs(t *testing.T) {
	t.Run("name_size_identical_restores_mtime", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		backup := "/w64/mem-ident"
		payload := []byte("w64 mem identical payload")
		require.NoError(t, afero.WriteFile(fs, backup, payload, 0o644))
		captured := time.Unix(1_000_000_000, 0)
		require.NoError(t, fs.Chtimes(backup, captured, captured))

		facts, err := captureReplacementBackupFacts(fs, backup)
		require.NoError(t, err)
		require.Equal(t, int64(len(payload)), facts.Size)
		require.Equal(t, w63ShaHex(payload), facts.SHA256)

		info, sErr := fs.Stat(backup)
		require.NoError(t, sErr)
		require.Equal(t, captured.Unix(), info.ModTime().Unix(),
			"Chtimes ran and restored the pre-read mtime the close re-stamp would have drifted")
	})

	t.Run("name_size_mismatch_skips_chtimes", func(t *testing.T) {
		base := afero.NewMemMapFs()
		backup := "/w64/mem-swap"
		payload := []byte("w64 mem original payload")
		require.NoError(t, afero.WriteFile(base, backup, payload, 0o644))
		captured := time.Unix(1_000_000_000, 0)
		require.NoError(t, base.Chtimes(backup, captured, captured))

		substitute := "/w64/mem-substitute"
		require.NoError(t, afero.WriteFile(base, substitute, []byte("w64 mem substitute of a different length"), 0o644))

		fs := &w64SwapOnOpenFs{Fs: base, victim: backup, swap: func() {
			_ = base.Rename(substitute, backup)
		}}

		facts, err := captureReplacementBackupFacts(fs, backup)
		require.NoError(t, err)
		require.Equal(t, int64(len(payload)), facts.Size, "the open handle hashed the original bytes")
		require.Equal(t, w63ShaHex(payload), facts.SHA256)

		info, sErr := base.Stat(backup)
		require.NoError(t, sErr)
		require.NotEqual(t, captured.Unix(), info.ModTime().Unix(),
			"Chtimes skipped — the name/size mismatch refused to touch the swapped substitute")
		require.Equal(t, "w64 mem substitute of a different length", string(mustReadDownloaderW7(t, base, backup)),
			"the substitute now occupies the backup name")
	})
}
