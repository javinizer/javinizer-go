package batch

import (
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cropExpectedIdentityFailFs struct {
	afero.Fs
	target string
}

func (f *cropExpectedIdentityFailFs) Open(name string) (afero.File, error) {
	if name == f.target {
		return nil, errors.New("source identity read failed")
	}
	return f.Fs.Open(name)
}

type cropExpectedIdentityRotateFs struct {
	afero.Fs
	target      string
	replacement []byte
	opens       atomic.Int32
}

func (f *cropExpectedIdentityRotateFs) Open(name string) (afero.File, error) {
	if name == f.target && f.opens.Add(1) == 2 {
		_ = afero.WriteFile(f.Fs, name, f.replacement, 0o644)
	}
	return f.Fs.Open(name)
}

func writeDistinctTestPoster(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 220, G: 30, B: 180, A: 255})
		}
	}
	file, err := os.Create(path)
	require.NoError(t, err)
	defer file.Close()
	require.NoError(t, jpeg.Encode(file, img, &jpeg.Options{Quality: 90}))
}

func TestPosterCropExpectedIdentityMissingPairReturns400(t *testing.T) {
	_, job, router := cropJobFixture(t, "P4-COV-001")
	revision := uint64(1)
	response := postCrop(t, router, job, "P4-COV-001", contracts.PosterCropRequest{
		X: 0, Y: 0, Width: 100, Height: 100,
		ExpectedPosterRevision: &revision,
	})
	assert.Equal(t, 400, response.Code, response.Body.String())
}

func TestPosterCropExpectedIdentityReadFailureReturns409(t *testing.T) {
	deps, job, router := cropJobFixture(t, "P4-COV-002")
	path := filepath.Join("data/temp/posters", job.GetID(), "P4-COV-002-full.jpg")
	identity, err := assetidentity.Measure(deps.GetFs(), path)
	require.NoError(t, err)
	deps.Fs = &cropExpectedIdentityFailFs{Fs: deps.GetFs(), target: path}

	response := postCrop(t, router, job, "P4-COV-002", contracts.PosterCropRequest{
		X: 0, Y: 0, Width: 100, Height: 100,
		ExpectedPosterRevision: &identity.Revision, ExpectedPosterFingerprint: identity.Fingerprint,
	})
	assert.Equal(t, 409, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "poster source identity unavailable")
}

func TestPosterCropIdentityChangesDuringCropReturns409(t *testing.T) {
	deps, job, router := cropJobFixture(t, "P4-COV-003")
	path := filepath.Join("data/temp/posters", job.GetID(), "P4-COV-003-full.jpg")
	identity, err := assetidentity.Measure(deps.GetFs(), path)
	require.NoError(t, err)
	replacementPath := path + ".replacement"
	writeDistinctTestPoster(t, replacementPath, 1000, 600)
	replacement, err := os.ReadFile(replacementPath)
	require.NoError(t, err)
	deps.Fs = &cropExpectedIdentityRotateFs{Fs: deps.GetFs(), target: path, replacement: replacement}

	response := postCrop(t, router, job, "P4-COV-003", contracts.PosterCropRequest{
		X: 0, Y: 0, Width: 400, Height: 600,
		ExpectedPosterRevision: &identity.Revision, ExpectedPosterFingerprint: strings.ToLower(identity.Fingerprint),
	})
	assert.Equal(t, 409, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "changed during the crop")
}
