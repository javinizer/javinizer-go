package fsutil

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stagingIdentity's defensive legs: a nil handle or a Stat failure yield nil
// (UnlinkVerified then never binds; the keep-both postcondition holds).
func TestStagingIdentity_DefensiveLegs(t *testing.T) {
	assert.Nil(t, stagingIdentity(nil))

	fh := &statFailFile{}
	assert.Nil(t, stagingIdentity(fh))
}

type statFailFile struct{ afero.File }

func (s *statFailFile) Stat() (os.FileInfo, error) { return nil, errors.New("simulated stat failure") }

func TestDiscardBound_NilIdentityKeepsBoth(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/out/a.mp4.nrstg.9", []byte("staged"), 0644))
	discardStagedAfterFailedPublish(fs, "/out/a.mp4.nrstg.9", nil, fsutil_cropErr("boom"))
	content, err := afero.ReadFile(fs, "/out/a.mp4.nrstg.9")
	require.NoError(t, err)
	assert.Equal(t, "staged", string(content))
}
