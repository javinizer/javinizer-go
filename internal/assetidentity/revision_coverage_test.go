package assetidentity

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type revisionCoverageErrorReader struct{}

func (revisionCoverageErrorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestRevisionCoverageErrorAndHeaderBranches(t *testing.T) {
	_, err := Measure(nil, "/poster.jpg")
	require.Error(t, err)

	fs := afero.NewMemMapFs()
	_, err = Measure(fs, "/missing.jpg")
	require.Error(t, err)

	_, err = MeasureReader(nil)
	require.Error(t, err)
	_, err = MeasureReader(revisionCoverageErrorReader{})
	require.Error(t, err)

	identity, err := MeasureReader(strings.NewReader("poster"))
	require.NoError(t, err)
	normalized, err := NormalizeFingerprint("  " + strings.ToUpper(identity.Fingerprint) + "  ")
	require.NoError(t, err)
	assert.Equal(t, identity.Fingerprint, normalized)
	_, err = NormalizeFingerprint("")
	require.Error(t, err)
	_, err = NormalizeFingerprint("not-a-fingerprint")
	require.Error(t, err)

	SetHeaders(nil, identity)
	recorder := httptest.NewRecorder()
	SetHeaders(recorder, AssetRevision{})
	assert.Empty(t, recorder.Header())
	SetHeaders(recorder, identity)
	assert.Equal(t, identity.Fingerprint, recorder.Header().Get(FingerprintHeader))
	assert.Equal(t, strconv.FormatUint(identity.Revision, 10), recorder.Header().Get(RevisionHeader))
}
