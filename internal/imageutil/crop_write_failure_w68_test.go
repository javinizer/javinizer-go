package imageutil

// POSTER-WRITE-HARDENING codex PR#215 wave-68 (P2) — the crop producer's
// write-leg error paths: the jpeg.Encode failure leg (pre-existing, never
// exercised) and the wave-68 pre-close fstat failure leg (F1 introduced the
// pre-close posterFile.Stat — a producer that cannot prove its own record
// hands nothing down). Both fail the write leg closed: the error surfaces
// and no FileInfo rides out.

import (
	"errors"
	"image/color"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w68FailingWriteFile wraps an afero file whose Write always fails, so
// jpeg.Encode returns an error and cropAndWritePoster's encode error leg
// fires (Close + return nil, fmt.Errorf("failed to encode poster image")).
type w68FailingWriteFile struct {
	afero.File
	writeErr error
}

func (f *w68FailingWriteFile) Write(p []byte) (int, error) { return 0, f.writeErr }

type w68FailingWriteFs struct {
	afero.Fs
	writeErr error
}

func (f *w68FailingWriteFs) Create(name string) (afero.File, error) {
	inner, err := f.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &w68FailingWriteFile{File: inner, writeErr: f.writeErr}, nil
}

func TestCropPosterW68_EncodeFailureFailsWriteLegClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w68-enc", 0o755))
	cover := "/w68-enc/cover.jpg"
	poster := "/w68-enc/poster.jpg"
	createTestImage(t, base, cover, 200, 100, color.White) // landscape → right crop

	wedge := errors.New("w68 jpeg write wedged")
	fs := &w68FailingWriteFs{Fs: base, writeErr: wedge}

	info, err := CropPosterFromCover(fs, cover, poster, 0)
	require.ErrorIs(t, err, wedge)
	require.ErrorContains(t, err, "failed to encode poster image")
	require.Nil(t, info, "a producer that cannot prove its own record hands nothing down")
}

// w68FailingStatFile wraps an afero file whose Stat always fails, so the
// wave-68 pre-close posterFile.Stat() (F1) fails and cropAndWritePoster's
// stat error leg fires (return nil, fmt.Errorf("failed to stat written
// poster file")). Write delegates to the underlying file so jpeg.Encode
// succeeds and the failure localizes to the identity capture.
type w68FailingStatFile struct {
	afero.File
	statErr error
}

func (f *w68FailingStatFile) Stat() (os.FileInfo, error) { return nil, f.statErr }

type w68FailingStatFs struct {
	afero.Fs
	statErr error
}

func (f *w68FailingStatFs) Create(name string) (afero.File, error) {
	inner, err := f.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &w68FailingStatFile{File: inner, statErr: f.statErr}, nil
}

func TestCropPosterW68_PreCloseStatFailureFailsWriteLegClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w68-stat", 0o755))
	cover := "/w68-stat/cover.jpg"
	poster := "/w68-stat/poster.jpg"
	createTestImage(t, base, cover, 200, 100, color.White)

	wedge := errors.New("w68 pre-close stat wedged")
	fs := &w68FailingStatFs{Fs: base, statErr: wedge}

	info, err := CropPosterFromCover(fs, cover, poster, 0)
	require.ErrorIs(t, err, wedge)
	require.ErrorContains(t, err, "failed to stat written poster file")
	require.Nil(t, info)

	// The genuine bytes landed (jpeg.Encode succeeded before the fstat
	// wedge) — only the producer record could not be proven.
	exists, _ := afero.Exists(base, poster)
	require.True(t, exists)
}

// w68FailingCreateFs fails Create outright, so cropAndWritePoster's create
// error leg fires (return nil, fmt.Errorf("failed to create poster file")).
type w68FailingCreateFs struct {
	afero.Fs
	createErr error
}

func (f *w68FailingCreateFs) Create(name string) (afero.File, error) {
	return nil, f.createErr
}

func TestCropPosterW68_CreateFailureFailsWriteLegClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w68-create", 0o755))
	cover := "/w68-create/cover.jpg"
	createTestImage(t, base, cover, 200, 100, color.White)

	wedge := errors.New("w68 create wedged")
	fs := &w68FailingCreateFs{Fs: base, createErr: wedge}

	info, err := CropPosterFromCover(fs, cover, "/w68-create/poster.jpg", 0)
	require.ErrorIs(t, err, wedge)
	require.ErrorContains(t, err, "failed to create poster file")
	require.Nil(t, info)
}
