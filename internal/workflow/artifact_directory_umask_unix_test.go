//go:build unix

package workflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Umask is process-global. Each mask runs in a separate test process so no
// concurrent package test can observe a transient mask or cached config value.
func TestArtifactDirectoryUmask(t *testing.T) {
	if raw := os.Getenv("JAVINIZER_TEST_DIRECTORY_UMASK"); raw != "" {
		mask, err := strconv.ParseUint(raw, 8, 9)
		require.NoError(t, err)
		syscall.Umask(int(mask))
		config.StoreUmask(int(mask))
		testArtifactDirectoryUmask(t, os.FileMode(0777&^mask))
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, mask := range []string{"000", "002", "022", "077"} {
		t.Run(mask, func(t *testing.T) {
			cmd := exec.Command(executable, "-test.run=^TestArtifactDirectoryUmask$", "-test.v", "-test.timeout=60s")
			cmd.Env = append(os.Environ(), "JAVINIZER_TEST_DIRECTORY_UMASK="+mask)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}

func requireDirectoryMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Equal(t, want, info.Mode().Perm(), "directory %s", path)
}

func testArtifactDirectoryUmask(t *testing.T, want os.FileMode) {
	t.Run("direct_organizer_control", func(t *testing.T) {
		fs := afero.NewOsFs()
		root := t.TempDir()
		source := filepath.Join(root, "ABC-123.mkv")
		require.NoError(t, os.WriteFile(source, []byte("video fixture"), 0600))
		movie := models.Movie{ID: "ABC-123"}
		dest := filepath.Join(root, "library")
		org := organizer.NewOrganizer(fs, &organizer.Config{SubfolderFormat: []string{"actress"}, FolderFormat: "movie", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
		_, err := org.Organize(context.Background(), organizer.OrganizeCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv"}, DestDir: dest, MoveFiles: true})
		require.NoError(t, err)
		for _, p := range []string{dest, filepath.Join(dest, "actress"), filepath.Join(dest, "actress", "movie")} {
			requireDirectoryMode(t, p, want)
		}
	})
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("workflow/existing=%t", existing), func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			source := filepath.Join(root, "ABC-123.mkv")
			require.NoError(t, os.WriteFile(source, []byte("video fixture"), 0600))
			movie := models.Movie{ID: "ABC-123", ContentID: "directory-umask", RenderGeneration: 1}
			dest := filepath.Join(root, "library")
			actress := filepath.Join(dest, "actress")
			folder := filepath.Join(actress, "movie")
			expected := want
			if existing {
				require.NoError(t, os.MkdirAll(folder, 0700))
				for _, p := range []string{dest, actress, folder} {
					require.NoError(t, os.Chmod(p, 0711))
				}
				expected = 0711
			}
			org := organizer.NewOrganizer(fs, &organizer.Config{SubfolderFormat: []string{"actress"}, FolderFormat: "movie", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
			orch.organizer = org
			stageSeen := false
			orch.artifactPrepared = func() {
				require.NoError(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if info.IsDir() && strings.HasPrefix(info.Name(), artifactStageDirPrefix) {
						stageSeen = true
						requireDirectoryMode(t, p, 0700)
					}
					return nil
				}))
			}
			result, err := orch.Execute(context.Background(), ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv"}, DestPath: dest, Organize: OrganizeOptions{MoveFiles: true}, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize})
			require.NoError(t, err)
			require.True(t, stageSeen)
			require.FileExists(t, result.OrganizeResult.NewPath)
			require.FileExists(t, result.NFOPath)
			videoInfo, err := os.Stat(result.OrganizeResult.NewPath)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), videoInfo.Mode().Perm(), "moving video must preserve its mode")
			nfoInfo, err := os.Stat(result.NFOPath)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0666&^config.UmaskValue()), nfoInfo.Mode().Perm(), "new NFO mode must still follow file creation policy")
			for _, p := range []string{dest, actress, folder} {
				requireDirectoryMode(t, p, expected)
			}
		})
		t.Run(fmt.Sprintf("artifact/existing=%t", existing), func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			staging := filepath.Join(root, "stage")
			dest := filepath.Join(root, "library")
			nested := filepath.Join(dest, "artwork")
			require.NoError(t, os.MkdirAll(filepath.Join(staging, "artwork"), 0700))
			source := filepath.Join(staging, "artwork", "poster.jpg")
			require.NoError(t, os.WriteFile(source, []byte("artwork fixture"), 0600))
			expected := want
			if existing {
				require.NoError(t, os.MkdirAll(nested, 0700))
				require.NoError(t, os.Chmod(dest, 0711))
				require.NoError(t, os.Chmod(nested, 0711))
				expected = 0711
			}
			stage := &artifactStage{fs: fs, root: staging, finalRoot: dest}
			_, err := stage.installPaths([]string{source}, nil, "", "", nil)
			require.NoError(t, err)
			require.FileExists(t, filepath.Join(nested, "poster.jpg"))
			requireDirectoryMode(t, dest, expected)
			requireDirectoryMode(t, nested, expected)
		})
		t.Run(fmt.Sprintf("subtitle/existing=%t", existing), func(t *testing.T) {
			stage, orch, _, _, source, target := stagedSubtitleFixture(t)
			parent := filepath.Dir(target)
			expected := want
			if existing {
				require.NoError(t, os.MkdirAll(parent, 0700))
				require.NoError(t, os.Chmod(parent, 0711))
				expected = 0711
			}
			require.NoError(t, stage.publishStagedSubtitle(context.Background(), orch, "1", source, &stage.stagedSubtitleResults[0]))
			require.FileExists(t, target)
			requireDirectoryMode(t, parent, expected)
		})
		t.Run(fmt.Sprintf("in_place_parent/existing=%t", existing), func(t *testing.T) {
			stage, orch, _, source, _ := directoryMoveFixture(t)
			parent := filepath.Join(filepath.Dir(source), "new-parent")
			target := filepath.Join(parent, "renamed")
			expected := want
			require.NoError(t, os.Chmod(source, 0711))
			if existing {
				require.NoError(t, os.MkdirAll(parent, 0700))
				require.NoError(t, os.Chmod(parent, 0711))
				expected = 0711
			}
			location, err := stage.moveInPlaceDirectory(context.Background(), orch, "", source, target, stage.sourceDirIdentity)
			require.NoError(t, err)
			require.Equal(t, target, location)
			requireDirectoryMode(t, parent, expected)
			requireDirectoryMode(t, target, 0711)
		})
		for _, fallback := range []bool{false, true} {
			t.Run(fmt.Sprintf("allocation/existing=%t/fallback=%t", existing, fallback), func(t *testing.T) {
				fs := afero.NewOsFs()
				root := t.TempDir()
				parent := filepath.Join(root, "new-parent")
				dest := filepath.Join(root, "fallback")
				expected := want
				if existing {
					for _, p := range []string{parent, dest} {
						require.NoError(t, os.MkdirAll(p, 0700))
						require.NoError(t, os.Chmod(p, 0711))
					}
					expected = 0711
				}
				var used afero.Fs = fs
				created := parent
				if fallback {
					used = &allocationFailureFS{Fs: fs, parent: parent, parentTemp: os.ErrPermission}
					created = dest
				}
				stage, err := allocateArtifactStageAt(used, parent, dest)
				require.NoError(t, err)
				require.Equal(t, created, filepath.Dir(stage))
				requireDirectoryMode(t, created, expected)
				requireDirectoryMode(t, stage, 0700)
			})
		}
	}
}
