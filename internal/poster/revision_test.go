package poster

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevisionCompatibilityWrappers(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/poster.jpg", []byte("poster"), 0o644))
	got, err := MeasureAsset(fs, "/poster.jpg")
	require.NoError(t, err)
	assert.Equal(t, assetidentity.FromBytes([]byte("poster")), got)
	_, err = MeasureAsset(fs, "/missing.jpg")
	require.Error(t, err)
	assert.True(t, ValidFingerprint(got.Fingerprint))
	assert.True(t, ValidFingerprint(""))
	assert.False(t, ValidFingerprint("invalid"))
}
