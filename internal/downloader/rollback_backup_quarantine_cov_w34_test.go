package downloader

// POSTER-WRITE-HARDENING codex local review round 4 (PR#215) — coverage pin
// for quarantineRollbackBackupForRemoval's remaining defensive legs (the
// patch gate runs at 100%): the no-follow reopen's non-ENOENT failure, the
// opened-handle Stat failure, the opened-object shape refusal, the OsFs
// dev/inode mismatch between the Lstat'd and the opened object, the
// post-move re-verify refusal (hold.restore() moves the verified bytes back
// onto the journaled name), and the Windows-posture handle close in
// moveVerifiedRollbackBackupToQuarantine (fsutil.PathBackslashesAreSeparators
// flipped, exactly like install_overwrite_w12_test.go decides the Windows
// seam on any host).

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w34StatFailFile fails Stat deterministically (the opened-handle leg).
type w34StatFailFile struct {
	afero.File
	err error
}

func (f w34StatFailFile) Stat() (os.FileInfo, error) { return nil, f.err }

// w34NilStatFile answers Stat with (nil, nil) — the shape-refusal leg.
type w34NilStatFile struct{ afero.File }

func (f w34NilStatFile) Stat() (os.FileInfo, error) { return nil, nil }

// w34CloseTrackFile observes the Windows-posture close in
// moveVerifiedRollbackBackupToQuarantine.
type w34CloseTrackFile struct {
	afero.File
	closed *bool
}

func (f w34CloseTrackFile) Close() error {
	*f.closed = true
	return f.File.Close()
}

// The post-Lstat reopen of the backup failing with a NON-ENOENT error keeps
// the entry armed: plain error out, backup intact, no quarantine debris.
func TestRollbackBackupQuarantineCovW34_ReopenFailureKeepsBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	backup := "/w34dl-ro/poster.jpg.dlbak.0123456789abcdef"
	w32RollbackBackup(t, base, backup, "old bytes")
	openErr := errors.New("w34 backup reopen wedged")
	fs := &w32RollbackQuarFs{Fs: base, openFn: func(name string) (afero.File, bool, error) {
		if filepath.Clean(name) == filepath.Clean(backup) {
			return nil, true, openErr
		}
		return nil, false, nil
	}}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w34 leg")
	require.Nil(t, hold)
	require.ErrorIs(t, err, openErr)
	require.Equal(t, "old bytes", string(mustReadDownloaderW7(t, base, backup)))
	require.Empty(t, w32RollbackQuarNames(t, base, "/w34dl-ro"))
}

// A failed Stat on the opened handle takes the same conservative leg.
func TestRollbackBackupQuarantineCovW34_OpenedStatFailureKeepsBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	backup := "/w34dl-sf/poster.jpg.dlbak.0123456789abcdef"
	w32RollbackBackup(t, base, backup, "old bytes")
	statErr := errors.New("w34 opened handle stat wedged")
	fs := &w32RollbackQuarFs{Fs: base, openFn: func(name string) (afero.File, bool, error) {
		if filepath.Clean(name) == filepath.Clean(backup) {
			real, oerr := base.OpenFile(name, os.O_RDONLY, 0)
			if oerr != nil {
				return nil, true, oerr
			}
			return w34StatFailFile{File: real, err: statErr}, true, nil
		}
		return nil, false, nil
	}}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w34 leg")
	require.Nil(t, hold)
	require.ErrorIs(t, err, statErr)
	require.Equal(t, "old bytes", string(mustReadDownloaderW7(t, base, backup)))
	require.Empty(t, w32RollbackQuarNames(t, base, "/w34dl-sf"))
}

// A nil (shapeless) Stat answer on the opened handle is a refusal, never a
// removal.
func TestRollbackBackupQuarantineCovW34_OpenedShapelessRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	backup := "/w34dl-ns/poster.jpg.dlbak.0123456789abcdef"
	w32RollbackBackup(t, base, backup, "old bytes")
	fs := &w32RollbackQuarFs{Fs: base, openFn: func(name string) (afero.File, bool, error) {
		if filepath.Clean(name) == filepath.Clean(backup) {
			real, oerr := base.OpenFile(name, os.O_RDONLY, 0)
			if oerr != nil {
				return nil, true, oerr
			}
			return w34NilStatFile{File: real}, true, nil
		}
		return nil, false, nil
	}}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w34 leg")
	require.Nil(t, hold)
	require.ErrorContains(t, err, "opened object is not the regular file Lstat verified")
	require.Equal(t, "old bytes", string(mustReadDownloaderW7(t, base, backup)))
	require.Empty(t, w32RollbackQuarNames(t, base, "/w34dl-ns"))
}

// On the real OsFs the opened handle's dev/inode must match the Lstat object;
// an answer for a DIFFERENT object (a swapped-in open) refuses with the
// backup intact.
func TestRollbackBackupQuarantineCovW34_OpenedIdentityMismatchRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode identity assertions are POSIX-shaped; the virtual legs cover the posture")
	}
	osfs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")
	other := filepath.Join(dir, "foreign.bin")
	require.NoError(t, os.WriteFile(backup, []byte("old bytes"), 0o644))
	require.NoError(t, os.WriteFile(other, []byte("foreign bytes"), 0o644))
	fs := &w32RollbackQuarFs{Fs: osfs, openFn: func(name string) (afero.File, bool, error) {
		if filepath.Clean(name) == filepath.Clean(backup) {
			f, oerr := os.Open(other) // answers for a DIFFERENT inode
			return f, true, oerr
		}
		return nil, false, nil
	}}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w34 leg")
	require.Nil(t, hold)
	require.ErrorContains(t, err, "opened object differs from the Lstat object")
	require.Equal(t, "old bytes", string(mustReadDownloaderW7(t, osfs, backup)))
	require.Empty(t, w32RollbackQuarNames(t, osfs, dir))
}

// The post-move re-verify answering a shapeless object restores the verified
// bytes onto the journaled name and refuses.
func TestRollbackBackupQuarantineCovW34_PostMoveShapelessRestores(t *testing.T) {
	base := afero.NewMemMapFs()
	backup := "/w34dl-pm/poster.jpg.dlbak.0123456789abcdef"
	w32RollbackBackup(t, base, backup, "old bytes")
	fs := &w32RollbackQuarFs{Fs: base}
	fs.lstat = func(call int, name string) (os.FileInfo, error) {
		if call == 1 {
			return nil, nil // post-move re-verify answers shapeless
		}
		return w32RealReads(fs)(call, name)
	}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w34 leg")
	require.Nil(t, hold)
	require.ErrorContains(t, err, "is not the verified regular file")
	require.Equal(t, "old bytes", string(mustReadDownloaderW7(t, base, backup)),
		"the compensation moved the verified object back onto the journaled name")
	require.Empty(t, w32RollbackQuarNames(t, base, "/w34dl-pm"))
}

// On the real OsFs the post-move re-verify answers a DIFFERENT inode (a
// foreign plant swapped onto the quarantine name inside the open→rename
// window): the dev/inode mismatch refusal restores the verified object onto
// the journaled name and the foreign bytes stay preserved.
func TestRollbackBackupQuarantineCovW34_PostMoveIdentityMismatchRestores(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode identity assertions are POSIX-shaped; the virtual legs cover the posture")
	}
	osfs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak.0123456789abcdef")
	foreign := filepath.Join(dir, "foreign-plant.bin")
	require.NoError(t, os.WriteFile(backup, []byte("old bytes"), 0o644))
	require.NoError(t, os.WriteFile(foreign, []byte("foreign-even-if-same-size!"), 0o644))
	foreignInfo, err := os.Lstat(foreign)
	require.NoError(t, err)
	fs := &w32RollbackQuarFs{Fs: osfs}
	fs.lstat = func(call int, name string) (os.FileInfo, error) {
		if call == 1 {
			return foreignInfo, nil // the post-move re-verify names the plant's inode
		}
		return w32RealReads(fs)(call, name)
	}

	hold, err := quarantineRollbackBackupForRemoval(fs, backup, nil, "w34 leg")
	require.Nil(t, hold)
	require.ErrorContains(t, err, "is not the verified object (dev/inode mismatch)")
	require.Equal(t, "old bytes", string(mustReadDownloaderW7(t, osfs, backup)),
		"the verified object moved back onto the journaled name")
	require.Equal(t, "foreign-even-if-same-size!", string(mustReadDownloaderW7(t, osfs, foreign)),
		"the foreign plant is preserved")
	require.Empty(t, w32RollbackQuarNames(t, osfs, dir))
}

// The Windows-posture move closes the caller's handle before the replacing
// rename (MoveFileEx cannot rename an open file); the seam flip decides this
// on any host, exactly like install_overwrite_w12_test.go.
func TestRollbackBackupQuarantineCovW34_WindowsPostureMoveClosesHandle(t *testing.T) {
	previous := fsutil.PathBackslashesAreSeparators
	fsutil.PathBackslashesAreSeparators = true
	t.Cleanup(func() { fsutil.PathBackslashesAreSeparators = previous })

	base := afero.NewMemMapFs()
	backup := "/w34dl-win/poster.jpg.dlbak.0123456789abcdef"
	quarantine := backup + ".dlq.0123456789abcdef0123456789abcdef"
	w32RollbackBackup(t, base, backup, "old bytes")
	// The O_EXCL reservation placeholder the production move displaces.
	require.NoError(t, afero.WriteFile(base, quarantine, nil, 0o600))

	src, err := base.OpenFile(backup, os.O_RDONLY, 0)
	require.NoError(t, err)
	closed := false
	// Wave-42: the conditional handoff wants the claimed reservation's
	// captured identity (the wave-30 production claim hands it over).
	reservation, rerr := lstatBackupCandidate(base, quarantine)
	require.NoError(t, rerr)
	err = moveVerifiedRollbackBackupToQuarantine(base, backup, quarantine, reservation, w34CloseTrackFile{File: src, closed: &closed})
	require.NoError(t, err)
	require.True(t, closed, "the Windows-posture move closes the handle before the publish rename")
	require.Equal(t, "old bytes", string(mustReadDownloaderW7(t, base, quarantine)),
		"the verified object moved onto the reserved quarantine name")
	exists, eerr := afero.Exists(base, backup)
	require.NoError(t, eerr)
	require.False(t, exists, "the journaled name is vacated by the move")
	require.Equal(t, []string{filepath.Base(quarantine)}, w32RollbackQuarNames(t, base, "/w34dl-win"),
		"only the published quarantine name remains — the take-aside placeholder never lingers")
}

// POSIX posture control: the handle stays OPEN through the rename (the
// descriptor pins the inode) — closed only by the caller's defer afterwards.
func TestRollbackBackupQuarantineCovW34_PosixPostureMoveKeepsHandleOpen(t *testing.T) {
	previous := fsutil.PathBackslashesAreSeparators
	fsutil.PathBackslashesAreSeparators = false
	t.Cleanup(func() { fsutil.PathBackslashesAreSeparators = previous })

	base := afero.NewMemMapFs()
	backup := "/w34dl-posix/poster.jpg.dlbak.0123456789abcdef"
	quarantine := backup + ".dlq.0123456789abcdef0123456789abcdef"
	w32RollbackBackup(t, base, backup, "old bytes")
	require.NoError(t, afero.WriteFile(base, quarantine, nil, 0o600))

	src, err := base.OpenFile(backup, os.O_RDONLY, 0)
	require.NoError(t, err)
	closed := false
	reservation, rerr := lstatBackupCandidate(base, quarantine)
	require.NoError(t, rerr)
	err = moveVerifiedRollbackBackupToQuarantine(base, backup, quarantine, reservation, w34CloseTrackFile{File: src, closed: &closed})
	require.NoError(t, err)
	require.False(t, closed, "the POSIX move never closes the handle — the caller owns its lifecycle")
	require.Equal(t, "old bytes", string(mustReadDownloaderW7(t, base, quarantine)))
	require.Equal(t, []string{filepath.Base(quarantine)}, w32RollbackQuarNames(t, base, "/w34dl-posix"),
		"only the published quarantine name remains — the take-aside placeholder never lingers")
	_ = src.Close()
}
