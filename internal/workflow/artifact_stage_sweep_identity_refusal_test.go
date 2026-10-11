package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// codex P1 (PRRT_kwDORn9KaM6qLkJz): every identity gate the sweep owns fails
// closed — a root whose ownership cannot be re-captured, re-authenticated, or
// re-derived is retained, never removed by name alone.

// lstatDenyFS refuses no-follow probes of one specific staging root: ownership
// evidence may open-read fine, while the identity capture keeps landing on
// doubt.
type lstatDenyFS struct {
	afero.Fs
	deny string
}

func (fs *lstatDenyFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Clean(name) == filepath.Clean(fs.deny) {
		return nil, false, errors.New("lstat refused: identity indeterminate")
	}
	if l, ok := fs.Fs.(afero.Lstater); ok {
		return l.LstatIfPossible(name)
	}
	info, err := fs.Fs.Stat(name)
	return info, false, err
}

func TestSweepArtifactStagingRetainsRootWhoseIdentityCannotBeCaptured(t *testing.T) {
	base := afero.NewMemMapFs()
	parent := "/lib"
	require.NoError(t, base.MkdirAll(parent, 0o755))
	root := seedStagingRoot(t, base, parent, nil)
	setSweepSeams(t, fsutil.ProcessDead, nil)

	sweepArtifactStaging(&lstatDenyFS{Fs: base, deny: root}, parent)

	exists, err := afero.DirExists(base, root)
	require.NoError(t, err)
	assert.True(t, exists, "an identity that can never be captured is retained for a later sweep")
}

// A root whose directory identity moves during authentication is retained:
// the tree that passed authentication is not the tree the sweep would remove.
func TestSweepArtifactStagingRetainsRootWhenIdentityMovesMidAuthentication(t *testing.T) {
	base := afero.NewMemMapFs()
	parent := "/lib"
	require.NoError(t, base.MkdirAll(parent, 0o755))
	root := seedStagingRoot(t, base, parent, nil)
	setSweepSeams(t, fsutil.ProcessDead, nil)

	sweepArtifactStaging(&driftedLstatFS{Fs: base, target: root, fireOn: 3}, parent)

	exists, err := afero.DirExists(base, root)
	require.NoError(t, err)
	assert.True(t, exists, "an identity that moved mid-authentication is retained for a later sweep")
	entries, readErr := afero.ReadDir(base, parent)
	require.NoError(t, readErr)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), artifactStageQuarantineMark,
			"refusal happened before any quarantine rename: %s", entry.Name())
	}
}

func TestArtifactStageReclaimableIdentityRefusesUnauthenticatedRoot(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/lib/" + artifactStageDirPrefix + "tok-no-proof"
	require.NoError(t, fs.MkdirAll(root, 0o755))

	id, reclaimable := artifactStageReclaimableIdentity(fs, root)
	assert.False(t, reclaimable, "no manifest claim and no sealed sidecar means no ownership")
	assert.False(t, id.known, "refusal hands back no adoptable identity")
}

// sizedFileInfo skews one leg a weak identity compares on, without touching
// the tree.
type sizedFileInfo struct {
	os.FileInfo
	size int64
}

func (i sizedFileInfo) Size() int64 { return i.size }

func TestArtifactStageDirIdentityMatchesRefusalArms(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/lib/" + artifactStageDirPrefix + "tok-match"
	require.NoError(t, fs.MkdirAll(root, 0o755))
	info, err := fs.Stat(root)
	require.NoError(t, err)

	assert.False(t, (artifactStageDirIdentity{}).matches(fs, root, info),
		"an unknown identity never matches")

	strong := artifactStageDirIdentity{known: true, hasDevIno: true, dev: 9090, ino: 31337}
	assert.False(t, strong.matches(fs, root, info),
		"a strong identity the fs cannot re-derive can never affirm")
}

// driftedLstatFS answers one probe of a staging root with a shifted weak
// leg: the ownership proof already authenticated the tree, its identity was
// captured and re-proven — and the drift shows up exactly at the sweep's
// quarantined-name recheck. The probe index is pinned by the sweep's own
// call order (authenticate, capture, re-prove, quarantine recheck).
type driftedLstatFS struct {
	afero.Fs
	target string
	calls  int
	fireOn int
}

func (fs *driftedLstatFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	l, _ := fs.Fs.(afero.Lstater)
	probe := func(n string) (os.FileInfo, error) {
		if l != nil {
			info, _, err := l.LstatIfPossible(n)
			return info, err
		}
		return fs.Fs.Stat(n)
	}
	if filepath.Clean(name) != filepath.Clean(fs.target) {
		info, err := probe(name)
		return info, false, err
	}
	fs.calls++
	info, err := probe(name)
	if err != nil {
		return info, false, err
	}
	if fs.calls == fs.fireOn && info != nil {
		info = sizedFileInfo{FileInfo: info, size: info.Size() + 4096}
	}
	return info, true, err
}

func TestSweepArtifactStagingQuarantinedRootRefusesDriftedIdentity(t *testing.T) {
	base := afero.NewMemMapFs()
	parent := "/lib"
	require.NoError(t, base.MkdirAll(parent, 0o755))
	root := seedStagingRoot(t, base, parent, nil)
	quarantined := artifactStageQuarantineName(root)
	require.NoError(t, base.Rename(root, quarantined))
	setSweepSeams(t, fsutil.ProcessDead, nil)

	sweepArtifactStaging(&driftedLstatFS{Fs: base, target: quarantined, fireOn: 5}, parent)

	exists, err := afero.DirExists(base, quarantined)
	require.NoError(t, err)
	assert.True(t, exists, "a quarantined root whose identity drifted at the recheck is retained")
}

func TestSweepArtifactStagingRefusesRemovalWhenRootSwappedAtCompletedProof(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the swap-in-open seam relies on POSIX unlink-of-open-file semantics")
	}
	base := afero.NewOsFs()
	parent := t.TempDir()
	root := seedStagingRoot(t, base, parent, nil)
	setSweepSeams(t, fsutil.ProcessDead, nil)

	sweepArtifactStaging(&proofOpenSwapFS{Fs: base}, parent)

	seedToken := artifactStageManifestToken(root)
	var quarantined string
	entries, readErr := afero.ReadDir(base, parent)
	require.NoError(t, readErr)
	for _, entry := range entries {
		if entry.IsDir() && artifactStageManifestToken(filepath.Join(parent, entry.Name())) == seedToken {
			require.Empty(t, quarantined, "one seeded tree, one retained name")
			quarantined = filepath.Join(parent, entry.Name())
		}
	}
	require.NotEmpty(t, quarantined, "the seeded tree is retained, not consumed by RemoveAll")
	assert.Contains(t, filepath.Base(quarantined), artifactStageQuarantineMark, "retained under its quarantine name")
	foreign, err := afero.Exists(base, filepath.Join(quarantined, "foreign.bin"))
	require.NoError(t, err)
	assert.True(t, foreign, "the successor tree swapped in during proof restatement survives byte-intact")
}
