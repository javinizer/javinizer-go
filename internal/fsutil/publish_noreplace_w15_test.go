package fsutil

// POSTER-WRITE-HARDENING wave-15 (codex P2): PublishNoReplace pins the
// no-replace publish contract shared by the downloader's create path and the
// history backup re-arm — an occupied destination is a typed
// ErrPublishCollision with the existing bytes untouched, never a rename-over.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w15RaceAtRenameFs models a foreign writer claiming the destination INSIDE
// the classify→publish window, plus the refusal the platform's no-replace
// primitive gives at the rename itself — renameat2(RENAME_NOREPLACE) on
// Linux, MoveFileExW without MOVEFILE_REPLACE_EXISTING on Windows, link(2)'s
// EEXIST elsewhere. The racer's bytes land first, then the rename refuses
// with an exists-class error, exactly once.
type w15RaceAtRenameFs struct {
	afero.Fs
	dst    string
	racer  []byte
	landed bool
}

func (f *w15RaceAtRenameFs) Rename(oldname, newname string) error {
	if !f.landed && filepath.Clean(newname) == filepath.Clean(f.dst) {
		f.landed = true
		if err := afero.WriteFile(f.Fs, f.dst, f.racer, 0o644); err != nil {
			return err
		}
		return &os.PathError{Op: "rename", Path: newname, Err: os.ErrExist}
	}
	return f.Fs.Rename(oldname, newname)
}

type w15ClassifyWedgeFs struct {
	afero.Fs
	err error
}

func (f *w15ClassifyWedgeFs) LstatIfPossible(string) (os.FileInfo, bool, error) {
	return nil, false, f.err
}

type w15RenameWedgeFs struct {
	afero.Fs
	err error
}

func (f *w15RenameWedgeFs) Rename(_, _ string) error { return f.err }

// An occupied destination on the virtual leg refuses through the typed
// collision class; both files stay byte-identical.
func TestPublishNoReplaceW15_VirtualOccupiedDestinationCollides(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/out/W15-V/staged.tmp", []byte("staged"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/out/W15-V/poster.jpg", []byte("old"), 0o644))

	err := PublishNoReplace(base, "/out/W15-V/staged.tmp", "/out/W15-V/poster.jpg")
	require.ErrorIs(t, err, ErrPublishCollision)

	got, readErr := afero.ReadFile(base, "/out/W15-V/poster.jpg")
	require.NoError(t, readErr)
	require.Equal(t, "old", string(got), "the occupied destination is never clobbered")
	staged, readErr := afero.ReadFile(base, "/out/W15-V/staged.tmp")
	require.NoError(t, readErr)
	require.Equal(t, "staged", string(staged), "a refused publish keeps the staged file for the caller")
}

// A free destination on the virtual leg publishes exactly like a rename.
func TestPublishNoReplaceW15_VirtualFreeDestinationPublishes(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/out/W15-VF/staged.tmp", []byte("staged"), 0o644))

	require.NoError(t, PublishNoReplace(base, "/out/W15-VF/staged.tmp", "/out/W15-VF/poster.jpg"))
	got, err := afero.ReadFile(base, "/out/W15-VF/poster.jpg")
	require.NoError(t, err)
	require.Equal(t, "staged", string(got))
	_, err = base.Stat("/out/W15-VF/staged.tmp")
	require.ErrorIs(t, err, os.ErrNotExist, "the staged file moved onto the destination")
}

// The wave-15 race itself: the racer lands between classification and the
// rename; the refusal at the rename maps into ErrPublishCollision and the
// racer's bytes survive.
func TestPublishNoReplaceW15_VirtualRacerAtRenameCollides(t *testing.T) {
	base := afero.NewMemMapFs()
	dst := "/out/W15-VR/poster.jpg"
	require.NoError(t, base.MkdirAll("/out/W15-VR", 0o755))
	require.NoError(t, afero.WriteFile(base, "/out/W15-VR/staged.tmp", []byte("staged"), 0o644))
	fs := &w15RaceAtRenameFs{Fs: base, dst: dst, racer: []byte("racer-bytes")}

	err := PublishNoReplace(fs, "/out/W15-VR/staged.tmp", dst)
	require.ErrorIs(t, err, ErrPublishCollision, "the exists-class rename refusal maps into the collision class")
	require.True(t, fs.landed, "the injected race fired")

	got, readErr := afero.ReadFile(base, dst)
	require.NoError(t, readErr)
	require.Equal(t, "racer-bytes", string(got), "the racer's bytes are preserved")
	staged, readErr := afero.ReadFile(base, "/out/W15-VR/staged.tmp")
	require.NoError(t, readErr)
	require.Equal(t, "staged", string(staged))
}

// An indeterminate destination classification fails closed and is NOT a
// collision (callers must not reclassify on an IO wedge).
func TestPublishNoReplaceW15_VirtualClassifyWedgeFailsClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/out/W15-VW/staged.tmp", []byte("staged"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/out/W15-VW/poster.jpg", []byte("old"), 0o644))
	wedge := errors.New("w15 classify wedged")
	fs := &w15ClassifyWedgeFs{Fs: base, err: wedge}

	err := PublishNoReplace(fs, "/out/W15-VW/staged.tmp", "/out/W15-VW/poster.jpg")
	require.ErrorIs(t, err, wedge)
	require.NotErrorIs(t, err, ErrPublishCollision)
	require.Contains(t, err.Error(), "classify no-replace publish destination")
	got, readErr := afero.ReadFile(base, "/out/W15-VW/poster.jpg")
	require.NoError(t, readErr)
	require.Equal(t, "old", string(got), "an indeterminate destination is never clobbered")
}

// A plain rename failure (not exists-class) surfaces verbatim, not as a
// collision.
func TestPublishNoReplaceW15_VirtualRenameWedgeSurfaces(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/out/W15-VX/staged.tmp", []byte("staged"), 0o644))
	wedge := errors.New("w15 rename wedged")
	fs := &w15RenameWedgeFs{Fs: base, err: wedge}

	err := PublishNoReplace(fs, "/out/W15-VX/staged.tmp", "/out/W15-VX/poster.jpg")
	require.ErrorIs(t, err, wedge)
	require.NotErrorIs(t, err, ErrPublishCollision)
}

// The REAL OsFs kernel legs (renameat2 on Linux, hard-link publish elsewhere
// on POSIX, MoveFileExW-without-replace on Windows): a free destination
// publishes atomically.
func TestPublishNoReplaceW15_OsFsFreeDestinationPublishes(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, afero.WriteFile(fs, staged, []byte("staged"), 0o644))

	require.NoError(t, PublishNoReplace(fs, staged, dst))
	got, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	require.Equal(t, "staged", string(got))
	_, err = fs.Stat(staged)
	require.ErrorIs(t, err, os.ErrNotExist, "the staged name is consumed by the publish")
}

// The OsFs kernel legs refuse an occupied destination: the racer's bytes are
// preserved and the staged file stays in place for the caller's reclassify.
func TestPublishNoReplaceW15_OsFsOccupiedDestinationCollides(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged.tmp")
	dst := filepath.Join(dir, "poster.jpg")
	require.NoError(t, afero.WriteFile(fs, staged, []byte("staged"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("racer-bytes"), 0o644))

	err := PublishNoReplace(fs, staged, dst)
	require.ErrorIs(t, err, ErrPublishCollision)

	got, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr)
	require.Equal(t, "racer-bytes", string(got), "the kernel no-replace primitive preserved the racer")
	kept, readErr := afero.ReadFile(fs, staged)
	require.NoError(t, readErr)
	require.Equal(t, "staged", string(kept), "the refused publish keeps the staged file")
}

// A missing staged source on the OsFs leg is an error, never a collision:
// the kernel no-replace primitive or the hard-link fallback fails ENOENT and
// refuses (wave-29: typed ErrPublishNoReplaceLinkFailed on the fallback leg),
// publishing nothing.
func TestPublishNoReplaceW15_OsFsMissingSourceIsNotACollision(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	err := PublishNoReplace(fs, filepath.Join(dir, "never-staged.tmp"), filepath.Join(dir, "poster.jpg"))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrPublishCollision)
}
