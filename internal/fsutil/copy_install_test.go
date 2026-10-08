package fsutil

// codex P1 (PR #276, finding ntCe6) — unit coverage for the identity-returning
// verified copy and the bound install observation: the identity produced at
// publish time (never a later name lookup) is what a rollback record may
// adopt, and an affirmative successor divergence surfaces the typed classes
// while retaining the foreign occupant byte-intact.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func admitAll(string, os.FileInfo) error { return nil }

func TestCopyFileNoReplaceVerifiedInstall_ReturnsPublishIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		fs   func(t *testing.T) (afero.Fs, string)
	}{
		{name: "osfs", fs: func(t *testing.T) (afero.Fs, string) {
			dir := t.TempDir()
			return afero.NewOsFs(), dir
		}},
		{name: "memmap virtual leg", fs: func(t *testing.T) (afero.Fs, string) {
			return afero.NewMemMapFs(), "/x"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, dir := tc.fs(t)
			src := filepath.Join(dir, "in.srt")
			dst := filepath.Join(dir, "nested", "out.srt")
			require.NoError(t, afero.WriteFile(fs, src, []byte("admitted bytes"), 0o644))

			identity, err := CopyFileNoReplaceVerifiedInstall(fs, src, dst, admitAll)
			require.NoError(t, err)
			require.NotNil(t, identity, "a clean verified install always yields its publish-time identity (virtual leg: direct post-publish lookup)")

			current, err := asideLstat(fs, dst)
			require.NoError(t, err)
			assert.True(t, asideSameObject(current, identity), "the returned identity names the installed object under the same predicate UnlinkVerified applies")
			got, err := afero.ReadFile(fs, dst)
			require.NoError(t, err)
			assert.Equal(t, "admitted bytes", string(got))
			srcBytes, err := afero.ReadFile(fs, src)
			require.NoError(t, err)
			assert.Equal(t, "admitted bytes", string(srcBytes), "copy lane never consumes the source")

			// The same identity observes cleanly afterwards.
			observed, oerr := ObserveVerifiedInstall(fs, dst, identity)
			require.NoError(t, oerr)
			require.NotNil(t, observed)
		})
	}
}

func TestCopyFileNoReplaceVerifiedInstall_NilProofLegacy(t *testing.T) {
	fs := afero.NewMemMapFs()
	src, dst := "/in.srt", "/out.srt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("x"), 0o644))
	identity, err := CopyFileNoReplaceVerifiedInstall(fs, src, dst, nil)
	require.NoError(t, err)
	assert.Nil(t, identity, "the legacy by-name lane carries no install identity")
	exists, _ := afero.Exists(fs, dst)
	assert.True(t, exists)
}

func TestCopyFileNoReplaceVerifiedInstall_AdmissionRefusalInstallsNothing(t *testing.T) {
	fs := afero.NewMemMapFs()
	src, dst := "/in.srt", "/out.srt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("x"), 0o644))
	sentinel := errors.New("not the admitted object")
	identity, err := CopyFileNoReplaceVerifiedInstall(fs, src, dst, func(string, os.FileInfo) error { return sentinel })
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.ErrorIs(t, err, sentinel)
	assert.Nil(t, identity, "a refusal attributes no identity")
	exists, _ := afero.Exists(fs, dst)
	assert.False(t, exists, "nothing published")
}

func TestCopyFileNoReplaceVerifiedInstall_CollisionKeepsOccupant(t *testing.T) {
	fs := afero.NewMemMapFs()
	src, dst := "/in.srt", "/out.srt"
	require.NoError(t, afero.WriteFile(fs, src, []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("old"), 0o644))
	identity, err := CopyFileNoReplaceVerifiedInstall(fs, src, dst, admitAll)
	require.ErrorIs(t, err, ErrPublishCollision)
	assert.Nil(t, identity)
	got, _ := afero.ReadFile(fs, dst)
	assert.Equal(t, "old", string(got), "the occupied destination is untouched")
}

func TestObserveVerifiedInstall_SuccessorDivergenceTyped(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "in.srt")
	dst := filepath.Join(dir, "out.srt")
	require.NoError(t, afero.WriteFile(fs, src, []byte("admitted"), 0o644))
	identity, err := CopyFileNoReplaceVerifiedInstall(fs, src, dst, admitAll)
	require.NoError(t, err)

	// External writer replaces the install inside the lock-release→observe
	// window: same name, different object.
	require.NoError(t, fs.Remove(dst))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign successor"), 0o644))

	observed, oerr := ObserveVerifiedInstall(fs, dst, identity)
	require.Error(t, oerr)
	assert.Nil(t, observed, "the successor is never adopted as the installed record")
	assert.True(t, errors.Is(oerr, ErrPublishSuccessorUnproven), "the occupant is affirmatively not ours: %v", oerr)
	assert.True(t, errors.Is(oerr, ErrPublishCompleted), "the doubt class rides along — our bytes may stand elsewhere: %v", oerr)
	got, rerr := afero.ReadFile(fs, dst)
	require.NoError(t, rerr)
	assert.Equal(t, "foreign successor", string(got), "the occupant is retained byte-intact")

	// A same-bytes rewrite on POSIX is still a different object: the dev/inode
	// legs catch a same-size successor. (Windows keeps size+mtime legs only;
	// the differing payload moves both.)
	if runtime.GOOS != "windows" {
		require.NoError(t, fs.Remove(dst))
		require.NoError(t, afero.WriteFile(fs, dst, []byte("admitted"), 0o644))
		_, oerr = ObserveVerifiedInstall(fs, dst, identity)
		require.Error(t, oerr, "byte-identical successor is still a different object")
		assert.True(t, errors.Is(oerr, ErrPublishSuccessorUnproven))
	}
}

func TestObserveVerifiedInstall_VacantRefuses(t *testing.T) {
	fs := afero.NewMemMapFs()
	identity := writeTempIdentity(t, fs, "/seed.srt")

	// codex P1, PRRT_kwDORn9KaM6qJY2i: the publish proved an install at this
	// name, so an absent entry cannot be classified as a did-not-install that
	// the unbound confirmation later re-derives from whatever appears.
	observed, err := ObserveVerifiedInstall(fs, "/absent.srt", identity)
	require.Error(t, err, "a vacant name after a proven publish is inconclusive and refused")
	assert.True(t, errors.Is(err, ErrPublishCompleted), "absence is the doubt class")
	assert.False(t, errors.Is(err, ErrPublishSuccessorUnproven), "absence is doubt, never an affirmative divergence")
	assert.Nil(t, observed)
}

func TestObserveVerifiedInstall_NonRegularOccupantDefersToConfirmation(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.srt")
	require.NoError(t, fs.MkdirAll(dst, 0o755))
	identity := writeTempIdentity(t, fs, filepath.Join(dir, "seed.srt"))

	// A directory at the endpoint is not a plausible file successor: the
	// observation stays silent so the legacy confirmation leg keeps its
	// did-not-install classification of the state.
	observed, err := ObserveVerifiedInstall(fs, dst, identity)
	require.NoError(t, err)
	assert.Nil(t, observed)
	assert.NotErrorIs(t, err, ErrPublishSuccessorUnproven)
	entries, rerr := afero.ReadDir(fs, dir)
	require.NoError(t, rerr)
	assert.NotEmpty(t, entries, "the directory occupant was never touched")
}

func TestObserveVerifiedInstall_NilIdentityFailsClosed(t *testing.T) {
	_, err := ObserveVerifiedInstall(afero.NewMemMapFs(), "/x", nil)
	require.Error(t, err, "a bound observation without the publish-produced identity is a caller bug")
}

func writeTempIdentity(t *testing.T, fs afero.Fs, path string) os.FileInfo {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, path, []byte("seed"), 0o644))
	info, err := fs.Stat(path)
	require.NoError(t, err)
	return info
}
