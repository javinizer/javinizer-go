package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectoryBoundOwnershipRejectsClonedNativeMarkers(t *testing.T) {
	fs := afero.NewOsFs()
	parent := t.TempDir()
	root := seedStagingRoot(t, fs, parent, func(m *artifactStageManifest) { m.CompletedUnixNano = 1 })
	body, err := afero.ReadFile(fs, filepath.Join(root, artifactStageManifestName))
	require.NoError(t, err)
	ownedInfo, err := fs.Stat(root)
	require.NoError(t, err)
	aside := root + ".original"
	require.NoError(t, fs.Rename(root, aside))
	require.NoError(t, fs.MkdirAll(root, 0o700))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "foreign.bin"), []byte("foreign"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, artifactStageManifestName), body, 0o600))
	require.NoError(t, afero.WriteFile(fs, artifactStageProofPath(root), body, 0o600))
	require.NoError(t, fs.Chtimes(root, ownedInfo.ModTime(), ownedInfo.ModTime()))
	var manifest artifactStageManifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	require.True(t, artifactStageSealed(&manifest))
	assert.False(t, artifactStageManifestReclaimable(fs, root))
	assert.False(t, readArtifactStageProof(fs, root))
	stage := &artifactStage{fs: fs, root: root}
	stage.cleanup()
	assert.Equal(t, root, stage.root, "foreign roots are not even quarantined")
	sweepArtifactStaging(fs, parent)
	foreign, err := afero.ReadFile(fs, filepath.Join(stage.root, "foreign.bin"))
	require.NoError(t, err)
	assert.Equal(t, "foreign", string(foreign))
	original, err := afero.ReadFile(fs, filepath.Join(aside, ".source", "MIRD-281.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(original))
}

func TestDirectoryBoundOwnershipRetainsLegacySealedMarkers(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := seedStagingRoot(t, fs, "/library", func(m *artifactStageManifest) {
		m.Directory = nil
		m.CompletedUnixNano = 1
	})
	body, err := afero.ReadFile(fs, filepath.Join(root, artifactStageManifestName))
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, artifactStageProofPath(root), body, 0o600))
	assert.False(t, artifactStageManifestReclaimable(fs, root))
	assert.False(t, readArtifactStageProof(fs, root))
	markArtifactStageCompleted(fs, root)
	unchanged, err := afero.ReadFile(fs, filepath.Join(root, artifactStageManifestName))
	require.NoError(t, err)
	assert.Equal(t, body, unchanged)
	sweepArtifactStaging(fs, "/library")
	payload, err := afero.ReadFile(fs, filepath.Join(root, ".source", "MIRD-281.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(payload))
}

func TestArtifactStageDirectoryProofLegs(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/stage", 0o700))
	identity := artifactStageDirectoryIdentity(fs, "/stage")
	proof := artifactStageDirectoryProofFromIdentity(identity)
	require.NotNil(t, proof)
	assert.True(t, proof.matchesPath(fs, "/stage"))
	assert.True(t, proof.matchesIdentity(proof.identity()))
	assert.Nil(t, artifactStageDirectoryProofFromIdentity(artifactStageDirIdentity{}))
	var absent *artifactStageDirectoryProof
	assert.False(t, absent.matchesIdentity(identity))
	assert.False(t, absent.identity().known)
	assert.False(t, proof.matchesIdentity(artifactStageDirIdentity{}))
	assert.False(t, proof.matchesIdentity(artifactStageDirIdentity{known: true, hasDevIno: true}))
	assert.False(t, artifactStageDirectoryIdentity(fs, "/missing").known)
	require.NoError(t, afero.WriteFile(fs, "/file", []byte("x"), 0o600))
	assert.False(t, artifactStageDirectoryIdentity(fs, "/file").known)
	changed := *proof
	changed.Size++
	assert.False(t, changed.matchesIdentity(identity))
	changed = *proof
	changed.ModTimeUnixNano++
	assert.False(t, changed.matchesIdentity(identity))
	osfs := afero.NewOsFs()
	root := t.TempDir()
	native := artifactStageDirectoryIdentity(osfs, root)
	require.True(t, native.hasDevIno)
	strong := artifactStageDirectoryProofFromIdentity(native)
	require.True(t, strong.Native)
	assert.True(t, strong.matchesIdentity(strong.identity()))
	changed = *strong
	changed.Inode++
	assert.False(t, changed.matchesIdentity(native))
	changed = *strong
	changed.Device++
	assert.False(t, changed.matchesIdentity(native))
	assert.False(t, proof.matchesIdentity(native))
}

func TestArtifactStageMarkerSymlinkTargetsRemainUntouched(t *testing.T) {
	for _, kind := range []string{"manifest", "proof"} {
		t.Run(kind, func(t *testing.T) {
			fs := afero.NewOsFs()
			parent := t.TempDir()
			root := seedStagingRoot(t, fs, parent, func(m *artifactStageManifest) { m.CompletedUnixNano = 1 })
			victim := filepath.Join(parent, "foreign.bin")
			require.NoError(t, afero.WriteFile(fs, victim, []byte("foreign"), 0o600))
			marker := filepath.Join(root, artifactStageManifestName)
			if kind == "proof" {
				marker = artifactStageProofPath(root)
			} else {
				require.NoError(t, fs.Remove(marker))
			}
			if err := os.Symlink(victim, marker); err != nil {
				t.Skipf("symlink creation unavailable: %v", err)
			}
			if kind == "proof" {
				writeArtifactStageProof(fs, root)
				assert.True(t, readArtifactStageProof(fs, root))
			} else {
				writeArtifactStageManifest(fs, root)
				manifest, authenticated := readArtifactStageAuthenticatedManifest(fs, root)
				require.True(t, authenticated)
				assert.True(t, manifest.Directory.matchesPath(fs, root))
			}
			body, err := afero.ReadFile(fs, victim)
			require.NoError(t, err)
			assert.Equal(t, "foreign", string(body))
		})
	}
}
