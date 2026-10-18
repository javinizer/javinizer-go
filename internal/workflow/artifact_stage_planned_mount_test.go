package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestArtifactStagePlannedMountDestination(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, nested := range []bool{false, true} {
			for _, move := range []bool{false, true} {
				for _, rollback := range []bool{false, true} {
					name := "same-device bind mount"
					if native {
						name = "native cross-device symlink"
					}
					if nested {
						name += "/missing nested movie"
					} else {
						name += "/existing movie"
					}
					if move {
						name += "/move"
					} else {
						name += "/copy"
					}
					if rollback {
						name += "/rollback"
					}
					t.Run(name, func(t *testing.T) {
						base := afero.NewOsFs()
						root := t.TempDir()
						library := filepath.Join(root, "library")
						require.NoError(t, base.Mkdir(library, 0755))
						mountRoot := filepath.Join(library, "movie")
						folder := mountRoot
						subfolders := []string(nil)
						if nested {
							mountRoot = filepath.Join(library, "actor")
							folder = filepath.Join(mountRoot, "new", "movie")
							subfolders = []string{"actor", "new"}
						}
						destination := mountRoot
						if native {
							destination = nativeCrossDeviceDestination(t, root)
							if err := os.Symlink(destination, mountRoot); err != nil {
								t.Skipf("directory symlinks unavailable: %v", err)
							}
						} else {
							require.NoError(t, base.Mkdir(mountRoot, 0755))
						}
						fs := &stagingPlacementFS{Fs: base, mountRoot: mountRoot}
						sentinel := filepath.Join(mountRoot, "keep.txt")
						require.NoError(t, afero.WriteFile(fs, sentinel, []byte("foreign"), 0644))
						source := filepath.Join(root, "incoming", "ABC-123.mkv")
						require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0755))
						sources := map[string]string{source: "video", filepath.Join(filepath.Dir(source), "ABC-123.srt"): "subtitle", filepath.Join(filepath.Dir(source), "ABC-123-cd2.mkv"): "part two"}
						for path, content := range sources {
							require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0644))
						}
						movie := models.Movie{ID: "ABC-123", ContentID: "planned-mount", Title: "Nested destination", RenderGeneration: 1}
						orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
						orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", SubfolderFormat: subfolders, FileFormat: "movie", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
						fenceErr := errors.New("publication rejected")
						fence := postPublishFence{movie: &movie}
						if rollback {
							fence.err = fenceErr
						}
						cmd := ApplyCmd{Movie: &movie, PublicationFence: fence, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: move}, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
						result, err := orch.Execute(context.Background(), cmd)
						if rollback {
							require.ErrorIs(t, err, fenceErr)
						} else {
							require.NoError(t, err)
							require.Equal(t, filepath.Join(folder, "movie.mkv"), result.OrganizeResult.NewPath)
							for file, want := range map[string]string{"movie.mkv": "video", "movie.srt": "subtitle", "movie-cd2.mkv": "part two"} {
								got, readErr := afero.ReadFile(fs, filepath.Join(folder, file))
								require.NoError(t, readErr)
								require.Equal(t, want, string(got))
							}
							nfo, readErr := afero.ReadFile(fs, result.NFOPath)
							require.NoError(t, readErr)
							require.Contains(t, string(nfo), movie.Title)
						}
						for path, want := range sources {
							got, readErr := afero.ReadFile(fs, path)
							if move && !rollback {
								require.ErrorIs(t, readErr, os.ErrNotExist)
							} else {
								require.NoError(t, readErr)
								require.Equal(t, want, string(got))
							}
						}
						got, readErr := afero.ReadFile(fs, sentinel)
						require.NoError(t, readErr)
						require.Equal(t, "foreign", string(got))
						require.Zero(t, fs.crossDeviceAttempts, "stage artifacts must never cross the mount boundary")
						require.NoError(t, afero.Walk(base, destination, func(path string, info os.FileInfo, walkErr error) error {
							require.NoError(t, walkErr)
							require.False(t, strings.HasPrefix(info.Name(), artifactStageDirPrefix), "stage residue: %s", path)
							if rollback && !info.IsDir() {
								require.Equal(t, "keep.txt", info.Name(), "rollback must remove all generated artifacts")
							}
							return nil
						}))
						pr260AssertStageGone(t, fs, root)
						pr260AssertStageGone(t, fs, library)
					})
				}
			}
		}
	}
}

func TestArtifactStageStationaryReadonlyDestination(t *testing.T) {
	for _, sibling := range []string{"", "movie.srt", "movie.en.srt", "movie-cd2.mkv", "movie.en.custom"} {
		t.Run("sibling="+sibling, func(t *testing.T) {
			base := afero.NewOsFs()
			root := t.TempDir()
			library := filepath.Join(root, "library")
			folder := filepath.Join(library, "movie")
			require.NoError(t, base.MkdirAll(folder, 0755))
			source := filepath.Join(folder, "movie.mkv")
			require.NoError(t, afero.WriteFile(base, source, []byte("video"), 0644))
			if sibling != "" {
				require.NoError(t, afero.WriteFile(base, filepath.Join(folder, sibling), []byte("sidecar"), 0644))
			}
			fs := &stagingPlacementFS{Fs: base, tempErrors: map[string]error{folder: os.ErrPermission}}
			movie := models.Movie{ID: "ABC-123", ContentID: "stationary-readonly", RenderGeneration: 1}
			org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt", ".custom"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
			orch.organizer = org
			cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeOrganize}
			result, err := orch.Execute(context.Background(), cmd)
			if sibling == "" || sibling == "movie.srt" {
				require.NoError(t, err)
				require.Equal(t, source, result.OrganizeResult.NewPath)
			} else {
				require.ErrorIs(t, err, os.ErrPermission)
			}
			got, readErr := afero.ReadFile(base, source)
			require.NoError(t, readErr)
			require.Equal(t, "video", string(got))
			pr260AssertStageGone(t, fs, library)
			pr260AssertStageGone(t, fs, folder)
		})
	}
}

func TestArtifactStageStationarySourceReadFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	library := filepath.Join(string(filepath.Separator), "library")
	source := filepath.Join(library, "missing", "movie.mkv")
	plan := &organizer.OrganizePlan{SourcePath: source, TargetPath: source, TargetDir: filepath.Dir(source)}
	orch := &applyOrchImpl{fs: fs, organizer: &stagingPlacementPlanner{plan: plan}}
	_, err := orch.allocateArtifactStageForApply(context.Background(), library, ApplyCmd{}, nil)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorContains(t, err, "stationary artifact source")
}

func TestArtifactStageAllocationFailureReleasesDuplicateClaim(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	library := filepath.Join(root, "library")
	folder := filepath.Join(library, "movie")
	require.NoError(t, base.MkdirAll(folder, 0755))
	source := filepath.Join(root, "ABC-123.mkv")
	require.NoError(t, afero.WriteFile(base, source, []byte("video"), 0644))
	fs := &stagingPlacementFS{Fs: base, tempErrors: map[string]error{folder: os.ErrPermission}}
	movie := models.Movie{ID: "ABC-123", ContentID: "allocation-claim", RenderGeneration: 1}
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	tracker := organizer.NewDuplicateTracker(false)
	orch := &applyOrchImpl{fs: fs, organizer: org}
	cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true, DuplicateTracker: tracker}, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.ErrorIs(t, err, os.ErrPermission)
	require.Nil(t, stage)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan bool, 1)
	other := filepath.Join(root, "other.mkv")
	target := filepath.Join(folder, "movie.mkv")
	go func() { _, duplicate := tracker.ObserveClaim(ctx, other, target, true); done <- duplicate }()
	select {
	case duplicate := <-done:
		require.False(t, duplicate, "failed allocation must release claim")
	case <-ctx.Done():
		t.Fatal("duplicate claimant blocked after allocation failed")
	}
	tracker.ReleaseClaim(other, target)
	require.FileExists(t, source)
	pr260AssertStageGone(t, fs, library)
}
