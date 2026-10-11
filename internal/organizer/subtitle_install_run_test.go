package organizer

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// A lane whose composite carries the verified twin but no bound-install
// override installs identity-bound through the twin itself: the proof must
// reach verified, never the unbound op, and the twin's publish-proven identity
// must come back untouched.
func TestSubtitleInstallRunVerifiedTwinArm(t *testing.T) {
	sentinel := errors.New("verified-twin-sentinel")
	calls := 0
	inst := subtitleInstall{
		op: func(afero.Fs, string, string) error {
			t.Fatal("unbound lane must not run for a proof-bound source")
			return nil
		},
		verified: func(fs afero.Fs, source, dest string, proof fsutil.VerifiedSourceProof) error {
			calls++
			assert.Equal(t, "/source.srt", source)
			assert.Equal(t, "/dest.srt", dest)
			require.NotNil(t, proof)
			return sentinel
		},
	}
	proof := fsutil.VerifiedSourceProof(func(string, os.FileInfo) error { return nil })
	gotID, err := inst.run(afero.NewMemMapFs(), "/source.srt", "/dest.srt", proof)
	assert.Equal(t, 1, calls)
	assert.Nil(t, gotID, "the legacy verified twin produces no publish identity")
	assert.ErrorIs(t, err, sentinel)
}
