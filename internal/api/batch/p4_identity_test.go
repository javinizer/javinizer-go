package batch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPosterCropExpectedIdentityMismatchReturns409(t *testing.T) {
	_, job, router := cropJobFixture(t, "P4-ID-001")
	path := filepath.Join("data/temp/posters", job.GetID(), "P4-ID-001-full.jpg")
	identity, err := assetidentity.Measure(afero.NewOsFs(), path)
	require.NoError(t, err)
	stale := []byte("different bytes with a different identity")
	require.NoError(t, os.WriteFile(path, stale, 0o644))

	response := postCrop(t, router, job, "P4-ID-001", contracts.PosterCropRequest{
		X: 0, Y: 0, Width: 100, Height: 100,
		ExpectedPosterRevision:    &identity.Revision,
		ExpectedPosterFingerprint: identity.Fingerprint,
	})
	require.Equal(t, 409, response.Code, response.Body.String())
}

func TestPosterCropMalformedFingerprintInPatchReturns400(t *testing.T) {
	_, job, router := cropJobFixture(t, "P4-ID-002")
	response := patchMovie(t, router, job, "P4-ID-002", `{"movie":{"id":"P4-ID-002","poster_crop_bounds":{"x":0,"y":0,"width":0.4,"height":1,"source_fingerprint":"not-a-sha"}}}`)
	require.Equal(t, 400, response.Code, response.Body.String())
}
