package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// vacateSwapFs substitutes the staged name's object as our vacate-rename
// begins — the goal: the object arriving at vac is NOT ours; bound removal
// must refuse it and restore it onto the staged name without unlinking.
type vacateSwapFs struct {
	afero.Fs
	on string
}

func (v *vacateSwapFs) Rename(oldname, newname string) error {
	if filepath.Clean(oldname) == filepath.Clean(v.on) {
		// plant a foreign occupant in place of the staged object right before
		// the vacate rename is issued.
		_ = v.Fs.Remove(oldname)
		_ = afero.WriteFile(v.Fs, oldname, []byte("substitute"), 0644)
	}
	return v.Fs.Rename(oldname, newname)
}

func TestDiscardBound_VacateSwapPreservesSubstitution(t *testing.T) {
	fs := &vacateSwapFs{Fs: afero.NewMemMapFs(), on: "/out/a.mp4.nrstg.9"}
	require.NoError(t, fs.Fs.MkdirAll("/out", 0777))
	require.NoError(t, afero.WriteFile(fs.Fs, "/out/a.mp4.nrstg.9", []byte("staged"), 0644))
	info, err := fs.Stat("/out/a.mp4.nrstg.9")
	require.NoError(t, err)

	discardStagedAfterFailedPublish(fs, "/out/a.mp4.nrstg.9", info, fsutil_cropErr("publish failed"))

	content, rerr := afero.ReadFile(fs.Fs, "/out/a.mp4.nrstg.9")
	require.NoError(t, rerr)
	assert.Equal(t, "substitute", string(content))
	entries, derr := afero.ReadDir(fs.Fs, "/out")
	require.NoError(t, derr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".vac.", "no vac residue left")
	}
}

// The three keep-both aborts of the vacate-discard: claim cannot reserve a vac
// name (entropy fail), the vac release refuses (its Remove fails), or the vacate
// rename itself errors — every failure leaves the staged object untouched.
func TestDiscardBound_AbortLegsKeepStaged(t *testing.T) {
	t.Run("claim-fail", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/out/a.mp4.nrstg.9", []byte("staged"), 0644))
		info, err := fs.Stat("/out/a.mp4.nrstg.9")
		require.NoError(t, err)

		prev := takeAsideVacRandReader
		takeAsideVacRandReader = failingReaderR4{}
		t.Cleanup(func() { takeAsideVacRandReader = prev })

		discardStagedAfterFailedPublish(fs, "/out/a.mp4.nrstg.9", info, fsutil_cropErr("pub failed"))
		content, _ := afero.ReadFile(fs, "/out/a.mp4.nrstg.9")
		assert.Equal(t, "staged", string(content))
	})

	t.Run("release-refuses-remove-vac-fails", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/out/a.mp4.nrstg.9", []byte("staged"), 0644))
		info, err := fs.Stat("/out/a.mp4.nrstg.9")
		require.NoError(t, err)

		// Releasing the vacated claim calls fs.Remove(claimName) — refuse it.
		wrapped := &removeVacFailFs{Fs: fs}
		discardStagedAfterFailedPublish(wrapped, "/out/a.mp4.nrstg.9", info, fsutil_cropErr("pub failed"))
		content, _ := afero.ReadFile(fs, "/out/a.mp4.nrstg.9")
		assert.Equal(t, "staged", string(content))
	})

	t.Run("rename-vacate-fail", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/out/a.mp4.nrstg.9", []byte("staged"), 0644))
		info, err := fs.Stat("/out/a.mp4.nrstg.9")
		require.NoError(t, err)

		notRenamed := &renameNeverFs{Fs: fs, failPrefix: "/out/a.mp4.nrstg.9"}
		discardStagedAfterFailedPublish(notRenamed, "/out/a.mp4.nrstg.9", info, fsutil_cropErr("pub failed"))
		content, _ := afero.ReadFile(fs, "/out/a.mp4.nrstg.9")
		assert.Equal(t, "staged", string(content))
	})
}

type failingReaderR4 struct{}

func (f failingReaderR4) Read(p []byte) (int, error) { return 0, errors.New("rand reader disabled") }

// renameNeverFs refuses any rename whose source path starts with the prefix.
type renameNeverFs struct {
	afero.Fs
	failPrefix string
}

func (r *renameNeverFs) Rename(oldname, newname string) error {
	if strings.HasPrefix(oldname, r.failPrefix) {
		return errors.New("simulated rename refusal")
	}
	return r.Fs.Rename(oldname, newname)
}

// removeVacFailFs refuses fs.Remove for any ".vac." name — that refuses the
// claim's release (covered leg: release-fail → keep both).
type removeVacFailFs struct{ afero.Fs }

func (r *removeVacFailFs) Remove(name string) error {
	if strings.Contains(name, ".vac.") {
		return errors.New("simulated release refusal")
	}
	return r.Fs.Remove(name)
}
