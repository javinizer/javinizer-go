package imageutil

// POSTER-WRITE-HARDENING wave-67 (codex P2, PR#215): the crop producers hand
// the WRITTEN poster's identity back with the result — the downloader's
// producer-side provenance bind freezes THAT record instead of re-deriving
// the mutable name after the crop returned (a swap in between used to
// authenticate the substitute). The legs below pin the returned record: it
// must name the just-written object on both filesystem flavors (OsFs through
// LstatIfPossible, MemMapFs through the Stat fallback), and a failed
// post-write lookup must fail the write leg closed — a producer that cannot
// prove its own record hands nothing down.

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// errW67StatWedged classifies the w67 test fs's wedged post-write lookup.
var errW67StatWedged = errors.New("w67 wedged post-write stat")

// w67StatFailFs fails lookups of one name while letting the write through,
// so the post-close identity capture is the only broken leg. It deliberately
// stays a non-Lstater (embedding MemMapFs), routing lstatWrittenPoster down
// the Stat fallback.
type w67StatFailFs struct {
	afero.Fs
	fail string
}

func (f *w67StatFailFs) Stat(name string) (os.FileInfo, error) {
	if name == f.fail {
		return nil, errW67StatWedged
	}
	return f.Fs.Stat(name)
}

func TestCropPosterW67_IdentityRidesTheWriteLeg(t *testing.T) {
	t.Run("OsFs — the returned FileInfo names the just-written poster", func(t *testing.T) {
		fs := afero.NewOsFs()
		dir := t.TempDir()
		cover := filepath.Join(dir, "cover.jpg")
		poster := filepath.Join(dir, "poster.jpg")
		createTestImage(t, fs, cover, 1000, 600, color.White)

		info, err := CropPosterFromCover(fs, cover, poster, 0)
		require.NoError(t, err)
		require.NotNil(t, info)

		onDisk, lerr := os.Lstat(poster)
		require.NoError(t, lerr)
		require.True(t, os.SameFile(info, onDisk), "the producer record names the written object itself")
		require.Equal(t, onDisk.Size(), info.Size())
		require.True(t, onDisk.ModTime().Equal(info.ModTime()))
	})

	t.Run("MemMapFs — the Stat fallback records the closed handle's object", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		createTestImage(t, fs, "/cover.jpg", 1000, 600, color.White)

		info, err := CropPosterFromCover(fs, "/cover.jpg", "/poster.jpg", 0)
		require.NoError(t, err)
		require.NotNil(t, info)

		onDisk, serr := fs.Stat("/poster.jpg")
		require.NoError(t, serr)
		require.Equal(t, onDisk.Size(), info.Size())
		require.True(t, onDisk.ModTime().Equal(info.ModTime()),
			"the record carries the post-close stamp — a pre-close fstat could never match")
	})

	t.Run("a wedged post-write lookup fails the write leg closed", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &w67StatFailFs{Fs: base, fail: "/poster.jpg"}
		createTestImage(t, fs, "/cover.jpg", 1000, 600, color.White)

		info, err := CropPosterFromCover(fs, "/cover.jpg", "/poster.jpg", 0)
		require.ErrorIs(t, err, errW67StatWedged)
		require.ErrorContains(t, err, "failed to stat written poster file")
		require.Nil(t, info)

		// The write itself landed — only the producer record could not be
		// proven — so the failure surfaces instead of handing nothing down.
		_, serr := base.Stat("/poster.jpg")
		require.NoError(t, serr)
	})
}
