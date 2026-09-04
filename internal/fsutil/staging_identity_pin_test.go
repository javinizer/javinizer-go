package fsutil

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
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
