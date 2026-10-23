//go:build !windows

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// This native-backed filesystem models restricted ACLs: creation, writes and
// renames work, but every chmod is forbidden. OpenFile deliberately returns the
// real handle, so mode proofs must inspect that inode rather than its name.
type restrictedACLStageFS struct {
	afero.Fs
	chmodCalls      int
	widenMarkerMode bool
	chmodErr        error
}

func (f *restrictedACLStageFS) Chmod(path string, _ os.FileMode) error {
	f.chmodCalls++
	errno := f.chmodErr
	if errno == nil {
		errno = syscall.EPERM
	}
	return &os.PathError{Op: "chmod", Path: path, Err: errno}
}

func (f *restrictedACLStageFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	return info, true, err
}

func (f *restrictedACLStageFS) OpenFile(path string, flags int, mode os.FileMode) (afero.File, error) {
	if f.widenMarkerMode && strings.Contains(path, ".jv-stage.artifact-marker.") {
		mode = 0o644
	}
	return f.Fs.OpenFile(path, flags, mode)
}

func restrictedACLStageFixture(t *testing.T) (*restrictedACLStageFS, string) {
	t.Helper()
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	return &restrictedACLStageFS{Fs: afero.NewOsFs()}, t.TempDir()
}

func TestArtifactStageACLCleanupKeepsAuthenticatedProofFlow(t *testing.T) {
	fs, parent := restrictedACLStageFixture(t)
	root, err := allocateArtifactStage(fs, parent)
	require.NoError(t, err)
	// Existing exclusive staging names remain untouched, even when the mode
	// proof means no chmod is necessary for the fresh replacement marker.
	collision := filepath.Join(root, artifactStageManifestName) + ".jv-stage.artifact-marker.0"
	require.NoError(t, os.WriteFile(collision, []byte("existing marker bytes"), 0o600))
	writeArtifactStageManifest(fs, root)
	manifest, authenticated := readArtifactStageAuthenticatedManifest(fs, root)
	require.True(t, authenticated, "restricted ACLs must not prevent the initial ownership seal")
	require.True(t, manifest.Directory.matchesPath(fs, root))
	body, err := os.ReadFile(collision)
	require.NoError(t, err)
	require.Equal(t, "existing marker bytes", string(body))
	info, err := os.Stat(filepath.Join(root, artifactStageManifestName))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.NoError(t, os.WriteFile(filepath.Join(root, "leftover"), []byte("temporary payload"), 0o600))

	markArtifactStageCompleted(fs, root)
	writeArtifactStageProof(fs, root)
	require.True(t, readArtifactStageProof(fs, root), "completed sidecar proof still gates removal")
	stage := &artifactStage{fs: fs, root: root}
	stage.cleanup()
	pr260AssertStageGone(t, fs, parent)
	require.Zero(t, fs.chmodCalls)
}

func TestArtifactStageACLEarlyFailureCleansOwnedRoot(t *testing.T) {
	fs, parent := restrictedACLStageFixture(t)
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "acl-early-failure"}, models.FileMatchInfo{Path: filepath.Join(parent, "missing.mp4")}, parent)
	cmd.Organize.Skip = false
	_, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
	require.ErrorContains(t, err, "artifact staging source")
	pr260AssertStageGone(t, fs, parent)
	require.Zero(t, fs.chmodCalls)
}

func TestArtifactStageACLDeferredPublicationAndCleanup(t *testing.T) {
	for _, move := range []bool{false, true} {
		name := "copy"
		if move {
			name = "move"
		}
		t.Run(name, func(t *testing.T) {
			fs, _ := restrictedACLStageFixture(t)
			_, root, source, _, _, unrelated, match := pr260FencedFiles(t, "restricted-acl-"+name)
			dest := filepath.Join(root, "library")
			org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			orch := &applyOrchImpl{fs: fs, organizer: org}
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "restricted-acl-" + name}, match, dest)
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = move
			cmd.Download = false
			stage, _, publishErr := verifiedStagePublish(t, orch, org, fs, root, source, dest, match, cmd)
			require.NoError(t, publishErr)
			stage.cleanup()
			pr260AssertStageGone(t, fs, root)
			pr260AssertStageGone(t, fs, dest)
			body, err := os.ReadFile(filepath.Join(dest, "movie", "movie.mp4"))
			require.NoError(t, err)
			require.Equal(t, "video", string(body))
			_, err = os.Stat(source)
			if move {
				require.True(t, os.IsNotExist(err))
			} else {
				require.NoError(t, err)
			}
			body, err = os.ReadFile(unrelated)
			require.NoError(t, err)
			require.Equal(t, "unrelated", string(body))
			require.Zero(t, fs.chmodCalls)
		})
	}
}

func TestArtifactStageACLInheritedWiderModeStillCleansOwnedRoot(t *testing.T) {
	fs, parent := restrictedACLStageFixture(t)
	fs.widenMarkerMode = true
	root, err := allocateArtifactStage(fs, parent)
	require.NoError(t, err)
	writeArtifactStageManifest(fs, root)
	manifest, authenticated := readArtifactStageAuthenticatedManifest(fs, root)
	require.True(t, authenticated, "a refused chmod on an inherited ACL mode must not drop the ownership seal")
	require.True(t, manifest.Directory.matchesPath(fs, root))
	require.Equal(t, 1, fs.chmodCalls)
	require.NoError(t, os.WriteFile(filepath.Join(root, "leftover"), []byte("temporary payload"), 0o600))
	(&artifactStage{fs: fs, root: root}).cleanup()
	pr260AssertStageGone(t, fs, parent)
}

func TestArtifactStageUnexpectedChmodFailureStillRetained(t *testing.T) {
	fs, parent := restrictedACLStageFixture(t)
	fs.widenMarkerMode = true
	fs.chmodErr = syscall.EIO
	root, err := allocateArtifactStage(fs, parent)
	require.NoError(t, err)
	writeArtifactStageManifest(fs, root)
	_, authenticated := readArtifactStageAuthenticatedManifest(fs, root)
	require.False(t, authenticated, "unexpected chmod failures still abort the marker")
	require.Equal(t, 1, fs.chmodCalls)
	require.NoError(t, os.WriteFile(filepath.Join(root, "payload"), []byte("preserve"), 0o600))
	(&artifactStage{fs: fs, root: root}).cleanup()
	sweepArtifactStaging(fs, parent)
	body, err := os.ReadFile(filepath.Join(root, "payload"))
	require.NoError(t, err)
	require.Equal(t, "preserve", string(body), "no manifest means no authority to remove")
}

func TestArtifactStageACLForeignSubstitutionStillRetained(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "symlink"}[symlink], func(t *testing.T) {
			fs, parent := restrictedACLStageFixture(t)
			root, err := allocateArtifactStage(fs, parent)
			require.NoError(t, err)
			writeArtifactStageManifest(fs, root)
			manifest, err := os.ReadFile(filepath.Join(root, artifactStageManifestName))
			require.NoError(t, err)
			require.NoError(t, os.Rename(root, root+".owned"))
			foreign := root
			if symlink {
				foreign = filepath.Join(parent, "foreign")
			}
			require.NoError(t, os.Mkdir(foreign, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(foreign, "payload"), []byte("foreign bytes"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(foreign, artifactStageManifestName), manifest, 0o600))
			if symlink {
				require.NoError(t, os.Symlink(foreign, root))
			}
			(&artifactStage{fs: fs, root: root}).cleanup()
			sweepArtifactStaging(fs, parent)
			body, err := os.ReadFile(filepath.Join(foreign, "payload"))
			require.NoError(t, err)
			require.Equal(t, "foreign bytes", string(body))
		})
	}
}
