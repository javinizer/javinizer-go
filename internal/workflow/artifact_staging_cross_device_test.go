package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// nativeCrossDeviceDestination finds an actual second filesystem, rather than
// simulating EXDEV in an afero wrapper. CI can supply a mounted writable root
// through JAVINIZER_TEST_CROSS_DEVICE_DIR; otherwise try the checkout and temp
// filesystem. Skip only when none of the available roots supports this test.
func nativeCrossDeviceDestination(t *testing.T, sourceRoot string) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	probe := filepath.Join(sourceRoot, "cross-device-probe")
	require.NoError(t, os.WriteFile(probe, []byte("probe"), 0o600))
	for _, parent := range []string{os.Getenv("JAVINIZER_TEST_CROSS_DEVICE_DIR"), cwd, os.TempDir()} {
		if parent == "" {
			continue
		}
		dest, mkdirErr := os.MkdirTemp(parent, ".javinizer-crossdev-test-")
		if mkdirErr != nil {
			t.Logf("cannot use cross-device test root %s: %v", parent, mkdirErr)
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dest) })
		probeTarget := filepath.Join(dest, "probe")
		renameErr := os.Rename(probe, probeTarget)
		if nativeArtifactCrossDeviceError(renameErr) {
			t.Logf("native cross-device rename confirmed: source root %s; destination root %s; error %v", sourceRoot, dest, renameErr)
			require.NoError(t, os.Remove(probe))
			return dest
		}
		if renameErr == nil {
			t.Logf("probe roots are rename-compatible: source root %s; candidate root %s", sourceRoot, parent)
			require.NoError(t, os.Rename(probeTarget, probe))
		} else {
			t.Logf("cross-device probe %s: %v", parent, renameErr)
		}
	}
	t.Skip("no writable second filesystem; set JAVINIZER_TEST_CROSS_DEVICE_DIR to a mounted test directory")
	return ""
}

func TestArtifactStageNativeCrossDeviceDestination(t *testing.T) {
	for _, tc := range []struct {
		name     string
		move     bool
		rollback bool
	}{
		{name: "move", move: true},
		{name: "copy"},
		{name: "move rollback", move: true, rollback: true},
		{name: "copy rollback", rollback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			destination := nativeCrossDeviceDestination(t, root)
			library := filepath.Join(root, "library")
			if err := os.Symlink(destination, library); err != nil {
				t.Skipf("directory symlinks unavailable: %v", err)
			}
			fs := afero.NewOsFs()
			source := filepath.Join(root, "incoming", "ABC-123.mkv")
			subtitle := filepath.Join(filepath.Dir(source), "ABC-123.srt")
			multipart := filepath.Join(filepath.Dir(source), "ABC-123-cd2.mkv")
			require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
			sources := map[string]string{source: "video", subtitle: "subtitle", multipart: "part two"}
			for path, content := range sources {
				require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))
			}
			movie := models.Movie{ID: "ABC-123", ContentID: "cross-device", Title: "Cross-device movie", RenderGeneration: 1}
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
			orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			fenceErr := errors.New("publication fence rejected")
			fence := postPublishFence{movie: &movie}
			if tc.rollback {
				fence.err = fenceErr
			}
			cmd := ApplyCmd{
				Movie: &movie, PublicationFence: fence,
				Match:    models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID},
				DestPath: library, Organize: OrganizeOptions{MoveFiles: tc.move},
				GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize,
			}
			result, err := orch.Execute(context.Background(), cmd)
			if tc.rollback {
				require.ErrorIs(t, err, fenceErr)
				for path, want := range sources {
					got, readErr := afero.ReadFile(fs, path)
					require.NoError(t, readErr, "rollback retains source %s", path)
					require.Equal(t, want, string(got))
				}
				files := []string{}
				require.NoError(t, afero.Walk(fs, destination, func(path string, info os.FileInfo, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if !info.IsDir() {
						files = append(files, path)
					}
					return nil
				}))
				require.Empty(t, files, "rollback leaves no installed payloads or transaction files")
			} else {
				require.NoError(t, err)
				require.NotNil(t, result.OrganizeResult)
				for name, want := range map[string]string{"movie.mkv": "video", "movie.srt": "subtitle", "movie-cd2.mkv": "part two"} {
					got, readErr := afero.ReadFile(fs, filepath.Join(destination, "movie", name))
					require.NoError(t, readErr)
					require.Equal(t, want, string(got))
				}
				nfoBytes, readErr := afero.ReadFile(fs, result.NFOPath)
				require.NoError(t, readErr)
				require.Contains(t, string(nfoBytes), movie.Title)
				for path, want := range sources {
					got, readErr := afero.ReadFile(fs, path)
					if tc.move {
						require.True(t, os.IsNotExist(readErr), "move consumed %s", path)
					} else {
						require.NoError(t, readErr)
						require.Equal(t, want, string(got))
					}
				}
			}
			pr260AssertStageGone(t, fs, destination)
			pr260AssertStageGone(t, fs, root)
		})
	}
}

func TestArtifactStageInPlaceCopyPreservesSiblingLayout(t *testing.T) {
	for _, crossDevice := range []bool{false, true} {
		name := "same filesystem"
		if crossDevice {
			name = "cross-device source symlink"
		}
		t.Run(name, func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			incoming := filepath.Join(root, "incoming")
			if crossDevice {
				target := nativeCrossDeviceDestination(t, root)
				if err := os.Symlink(target, incoming); err != nil {
					t.Skipf("directory symlinks unavailable: %v", err)
				}
			} else {
				require.NoError(t, fs.Mkdir(incoming, 0o755))
			}
			source := filepath.Join(incoming, "ABC-123 original.mkv")
			require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
			movie := models.Movie{ID: "ABC-123", ContentID: "inplace-copy-layout", RenderGeneration: 1}
			match, err := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, err)
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
			orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), match)
			cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: incoming, Organize: OrganizeOptions{MoveFiles: false}, GenerateNFO: true, OperationMode: operationmode.OperationModeInPlace}
			result, err := orch.Execute(context.Background(), cmd)
			require.NoError(t, err)
			require.Equal(t, filepath.Join(root, "renamed", "ABC-123.mkv"), result.OrganizeResult.NewPath)
			require.FileExists(t, result.OrganizeResult.NewPath)
			require.FileExists(t, result.NFOPath)
			got, err := afero.ReadFile(fs, source)
			require.NoError(t, err)
			require.Equal(t, "video", string(got), "in-place copy must retain its source")
			require.NoDirExists(t, filepath.Join(incoming, "renamed"), "copy layout must not become nested inside its source")
			pr260AssertStageGone(t, fs, root)
			pr260AssertStageGone(t, fs, incoming)
		})
	}
}

func TestArtifactStageInPlaceDownloadedArtwork(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, rollback := range []bool{false, true} {
			name := "new artwork"
			if existing {
				name = "preserve existing artwork"
			}
			if rollback {
				name += "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				fs := afero.NewOsFs()
				root := t.TempDir()
				incoming := filepath.Join(root, "incoming")
				require.NoError(t, fs.Mkdir(incoming, 0o755))
				source := filepath.Join(incoming, "ABC-123 original.mkv")
				require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
				if existing {
					require.NoError(t, afero.WriteFile(fs, filepath.Join(incoming, "poster.jpg"), []byte("old poster"), 0o644))
				}
				movie := models.Movie{ID: "ABC-123", ContentID: "inplace-artwork", RenderGeneration: 1}
				match, err := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, err)
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), match)
				orch := &applyOrchImpl{fs: fs, organizer: org}
				fenceErr := errors.New("publication rejected")
				fence := postPublishFence{movie: &movie}
				if rollback {
					fence.err = fenceErr
				}
				cmd := ApplyCmd{Movie: &movie, PublicationFence: fence, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: incoming, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeInPlace}
				stage, _, err := orch.prepareArtifact(context.Background(), cmd)
				require.NoError(t, err)
				defer stage.cleanup()
				poster := filepath.Join(stage.root, "poster.jpg")
				require.NoError(t, afero.WriteFile(fs, poster, []byte("new poster"), 0o644))
				state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}, downloadPaths: []string{poster}}
				err = stage.publish(context.Background(), orch, state, nil)
				expectedDir := filepath.Join(root, "renamed")
				if rollback {
					require.ErrorIs(t, err, fenceErr)
					require.NoDirExists(t, expectedDir)
					require.FileExists(t, source)
					expectedDir = incoming
				} else {
					require.NoError(t, err)
					require.NoDirExists(t, incoming)
				}
				expected := filepath.Join(expectedDir, "poster.jpg")
				require.Equal(t, []string{expected}, state.downloadPaths, "published and rollback paths must track directory relocation")
				if rollback && !existing {
					require.NoFileExists(t, expected)
				} else {
					got, readErr := afero.ReadFile(fs, expected)
					require.NoError(t, readErr)
					want := "new poster"
					if existing {
						want = "old poster"
					}
					require.Equal(t, want, string(got), "OverwriteExistingMedia:false must preserve existing artwork")
				}
			})
		}
	}
}
