package poster

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jpegBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

type createFailSuffixFS struct {
	afero.Fs
	suffix string
}

func (f createFailSuffixFS) Create(n string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(n), f.suffix) {
		return nil, errors.New("create wedged")
	}
	return f.Fs.Create(n)
}

// audit F-R3-2b: a failed download (undecodable body + failing crop-copy)
// must RESTORE the pre-existing canonical pair it parked — never delete it.
func TestDownloadFromURL_FailureRestoresParkedPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("this is not an image"))
	}))
	defer srv.Close()

	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-1-full.jpg", []byte("originalfull"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-1.jpg", []byte("originalcrop"), 0o644))
	fs := createFailSuffixFS{Fs: base, suffix: "BAD-1.jpg"}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "BAD-1", srv.URL+"/img.jpg", "", "")
	require.Error(t, err)
	full, ferr := afero.ReadFile(base, dir+"/BAD-1-full.jpg")
	require.NoError(t, ferr, "pre-existing full leg restored")
	assert.Equal(t, "originalfull", string(full))
	crop, cerr := afero.ReadFile(base, dir+"/BAD-1.jpg")
	require.NoError(t, cerr, "pre-existing cropped leg restored")
	assert.Equal(t, "originalcrop", string(crop))
	_, bakErr := base.Stat(dir + "/BAD-1-full.jpg.dlbak")
	assert.Error(t, bakErr, "no stray parked files")
}

type renameFailWhereFS struct {
	afero.Fs
	fail func(old, new string) bool
}

// Comparison is path-separator-tolerant: MemMapFs paths are always
// "/"...'-separated even on Windows, while the manager joins with
// filepath.Join (backslashes there). Normalize both ends.
func (f renameFailWhereFS) Rename(o, n string) error {
	if f.fail(filepath.ToSlash(o), filepath.ToSlash(n)) {
		return errors.New("rename wedged")
	}
	return f.Fs.Rename(o, n)
}

// Parking failure: an existing full leg whose park rename wedges aborts the
// download BEFORE any byte damage — canon untouched.
func TestDownloadFromURL_ParkFailureAbortsClean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-2-full.jpg", []byte("originalfull"), 0o644))
	fs := renameFailWhereFS{Fs: base, fail: func(_, n string) bool { return strings.HasSuffix(n, ".dlbak") }}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })
	_, err := pm.DownloadFromURL(context.Background(), "job1", "BAD-2", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to park previous full poster")
	got, rerr := afero.ReadFile(base, dir+"/BAD-2-full.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "originalfull", string(got), "untouched when parking fails")
}

// Finalize-then-fail restore: full leg parked, finalize rename wedges, the
// restore rename ALSO wedges — error surfaces, parked bytes retained for
// salvage rather than silently dropped.
func TestDownloadFromURL_FinalizeFailureRestoresPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-3-full.jpg", []byte("originalfull"), 0o644))
	target := dir + "/BAD-3-full.jpg"
	fs := renameFailWhereFS{Fs: base, fail: func(o, n string) bool {
		return n == filepath.ToSlash(target) && !strings.HasSuffix(o, ".dlbak") // finalize (and restore) fail; park succeeds
	}}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })
	_, err := pm.DownloadFromURL(context.Background(), "job1", "BAD-3", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to finalize image download")
	got, rerr := afero.ReadFile(base, target)
	require.NoError(t, rerr, "parked original restored to canonical")
	assert.Equal(t, "originalfull", string(got))
}

// audit F-R4-6: a wedged crop-leg park must abort fail-closed and undo the
// fresh full promote — the old pair comes back whole.
func TestDownloadFromURL_CropParkFailureRestoresFull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-4-full.jpg", []byte("origfull"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-4.jpg", []byte("origcrop"), 0o644))
	fs := renameFailWhereFS{Fs: base, fail: func(_, n string) bool { return strings.HasSuffix(n, "BAD-4.jpg.dlbak") }}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })
	_, err := pm.DownloadFromURL(context.Background(), "job1", "BAD-4", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to park previous cropped poster")
	full, ferr := afero.ReadFile(base, dir+"/BAD-4-full.jpg")
	require.NoError(t, ferr)
	assert.Equal(t, "origfull", string(full), "full leg restored after crop-park failure")
	crop, cerr := afero.ReadFile(base, dir+"/BAD-4.jpg")
	require.NoError(t, cerr)
	assert.Equal(t, "origcrop", string(crop), "crop leg untouched")
}

// audit F-R4-6: the crop-park failure ALSO wedges the full-leg restore → the
// warn arc fires (no silent swallow) and the parked originals stay on disk.
func TestDownloadFromURL_CropParkFailureRestoreWarns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-5-full.jpg", []byte("origfull"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-5.jpg", []byte("origcrop"), 0o644))
	wedged := func(o, n string) bool {
		return strings.HasSuffix(n, "BAD-5.jpg.dlbak") ||
			(strings.HasSuffix(o, ".dlbak") && n == filepath.ToSlash(dir)+"/BAD-5-full.jpg")
	}
	fs := renameFailWhereFS{Fs: base, fail: wedged}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })
	_, err := pm.DownloadFromURL(context.Background(), "job1", "BAD-5", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to park previous cropped poster")
	_, bakErr := base.Stat(dir + "/BAD-5-full.jpg.dlbak")
	assert.NoError(t, bakErr, "full park survives its wedged restore")
}

// Failure-path restore warns: copy fails AND the deferred restores wedge —
// logged, not swallowed; parked originals stay for salvage.
func TestDownloadFromURL_DeferRestoreWarns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("not an image"))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job2"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-6-full.jpg", []byte("origfull"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/BAD-6.jpg", []byte("origcrop"), 0o644))
	fs := &doubleWedgeFS{
		Fs:               base,
		failCreateSuffix: "BAD-6.jpg",
		failRename: func(o, n string) bool {
			return strings.HasSuffix(o, ".dlbak") && (n == filepath.ToSlash(dir)+"/BAD-6-full.jpg" || n == filepath.ToSlash(dir)+"/BAD-6.jpg")
		},
	}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })
	_, err := pm.DownloadFromURL(context.Background(), "job2", "BAD-6", srv.URL+"/img.jpg", "", "")
	require.Error(t, err)
	for _, p := range []string{dir + "/BAD-6-full.jpg.dlbak", dir + "/BAD-6.jpg.dlbak"} {
		_, stErr := base.Stat(p)
		assert.NoError(t, stErr, "wedged restore keeps the parked original: %s", p)
	}
}

type doubleWedgeFS struct {
	afero.Fs
	failCreateSuffix string
	failRename       func(o, n string) bool
}

func (f *doubleWedgeFS) Create(n string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(n), f.failCreateSuffix) {
		return nil, errors.New("create wedged")
	}
	return f.Fs.Create(n)
}

func (f *doubleWedgeFS) Rename(o, n string) error {
	if f.failRename(filepath.ToSlash(o), filepath.ToSlash(n)) {
		return errors.New("rename wedged")
	}
	return f.Fs.Rename(o, n)
}

// Success path: the new bytes land and the parked copies are discarded.
func TestDownloadFromURL_SuccessDiscardsParkedPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()

	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/GOOD-1-full.jpg", []byte("stalefull"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/GOOD-1.jpg", []byte("stalecrop"), 0o644))
	pm := NewPosterManager(base, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })

	res, err := pm.DownloadFromURL(context.Background(), "job1", "GOOD-1", srv.URL+"/img.jpg", "", "")
	require.NoError(t, err)
	require.NotNil(t, res)
	for _, p := range []string{dir + "/GOOD-1-full.jpg.dlbak", dir + "/GOOD-1.jpg.dlbak"} {
		_, stErr := base.Stat(p)
		assert.Error(t, stErr, "parked copies discarded on success")
	}
	full, _ := afero.ReadFile(base, dir+"/GOOD-1-full.jpg")
	assert.NotEqual(t, "stalefull", string(full), "new bytes landed at canonical")
}

type statFailExactFS struct {
	afero.Fs
	path string
}

func (f statFailExactFS) Stat(n string) (os.FileInfo, error) {
	if filepath.ToSlash(n) == filepath.ToSlash(f.path) {
		return nil, os.ErrPermission
	}
	return f.Fs.Stat(n)
}

// local codex review P1 (full leg): an UNDECIDABLE stat on the canonical full
// poster must refuse the download outright — treating it as absence would
// remove/replace healthy bytes nothing parked.
func TestDownloadFromURL_FullStatErrorRefusesDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/FULLST-1-full.jpg", []byte("originalfull"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/FULLST-1.jpg", []byte("originalcrop"), 0o644))
	fs := statFailExactFS{Fs: base, path: dir + "/FULLST-1-full.jpg"}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "FULLST-1", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to inspect previous full poster")
	full, ferr := afero.ReadFile(base, dir+"/FULLST-1-full.jpg")
	require.NoError(t, ferr)
	assert.Equal(t, "originalfull", string(full), "canon untouched on undecidable stat")
	crop, cerr := afero.ReadFile(base, dir+"/FULLST-1.jpg")
	require.NoError(t, cerr)
	assert.Equal(t, "originalcrop", string(crop), "crop leg untouched on undecidable stat")
	entries, _ := afero.ReadDir(base, dir)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".dlbak"), "no stray parked leg: %s", e.Name())
	}
}

// local-review coverage: finalize wedges AND the parked restore wedges too —
// the error surfaces and the original stays parked for salvage.
func TestDownloadFromURL_FinalizeFailureRestoreAlsoFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/FIN-2-full.jpg", []byte("originalfull"), 0o644))
	// Every rename landing ON the canonical full name fails: the finalize and
	// the subsequent restore attempt alike.
	fs := renameFailWhereFS{Fs: base, fail: func(_, n string) bool { return strings.HasSuffix(n, "/FIN-2-full.jpg") }}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "FIN-2", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to finalize image download")
	_, cErr := base.Stat(dir + "/FIN-2-full.jpg")
	assert.Error(t, cErr, "canon removed pre-finalize; restore wedged — absent")
	_, bakErr := base.Stat(dir + "/FIN-2-full.jpg.dlbak")
	assert.NoError(t, bakErr, "parked original retained for salvage")
}

// local-review coverage: the undecidable-crop-stat arm whose restore rename
// ALSO wedges still refuses the download and keeps the healthy crop canon.
func TestDownloadFromURL_CropStatErrorRestoreAlsoFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/CST-2-full.jpg", []byte("originalfull"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/CST-2.jpg", []byte("originalcrop"), 0o644))
	inner := renameFailWhereFS{Fs: base, fail: func(o, _ string) bool { return strings.HasSuffix(o, ".dlbak") }}
	fs := statFailExactFS{Fs: inner, path: dir + "/CST-2.jpg"}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "CST-2", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to inspect previous cropped poster")
	crop, cerr := afero.ReadFile(base, dir+"/CST-2.jpg")
	require.NoError(t, cerr)
	assert.Equal(t, "originalcrop", string(crop), "healthy crop canon never touched")
	_, bakErr := base.Stat(dir + "/CST-2-full.jpg.dlbak")
	assert.NoError(t, bakErr, "restore wedged — parked full leg retained for salvage")
}

// local codex review P1 (crop leg): the undecidable-crop-stat arm must undo
// the fresh full promote and restore the parked original before refusing.
func TestDownloadFromURL_CropStatErrorRestoresFullLeg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBytes(300, 400))
	}))
	defer srv.Close()
	base := afero.NewMemMapFs()
	dir := "/tmp/javinizer-test/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/CROPST-1-full.jpg", []byte("originalfull"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/CROPST-1.jpg", []byte("originalcrop"), 0o644))
	fs := statFailExactFS{Fs: base, path: dir + "/CROPST-1.jpg"}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client(), 0).WithSSRFCheck(func(_ string) error { return nil })

	_, err := pm.DownloadFromURL(context.Background(), "job1", "CROPST-1", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to inspect previous cropped poster")
	full, ferr := afero.ReadFile(base, dir+"/CROPST-1-full.jpg")
	require.NoError(t, ferr)
	assert.Equal(t, "originalfull", string(full), "full leg restored after undo")
	crop, cerr := afero.ReadFile(base, dir+"/CROPST-1.jpg")
	require.NoError(t, cerr)
	assert.Equal(t, "originalcrop", string(crop), "crop leg untouched")
	entries, _ := afero.ReadDir(base, dir)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".dlbak"), "no stray parked leg: %s", e.Name())
	}
}
