package poster

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
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
	if strings.HasSuffix(n, f.suffix) {
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
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

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

func (f renameFailWhereFS) Rename(o, n string) error {
	if f.fail(o, n) {
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
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
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
		return n == target && !strings.HasSuffix(o, ".dlbak") // finalize (and restore) fail; park succeeds
	}}
	pm := NewPosterManager(fs, "/tmp/javinizer-test", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
	_, err := pm.DownloadFromURL(context.Background(), "job1", "BAD-3", srv.URL+"/img.jpg", "", "")
	require.ErrorContains(t, err, "failed to finalize image download")
	got, rerr := afero.ReadFile(base, target)
	require.NoError(t, rerr, "parked original restored to canonical")
	assert.Equal(t, "originalfull", string(got))
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
	pm := NewPosterManager(base, "/tmp/javinizer-test", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })

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
