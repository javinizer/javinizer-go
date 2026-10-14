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

// Copy-only in-place execution stays private until publication. Its real plan
// may keep a mounted source directory or select a new sibling on the parent's
// filesystem; stage placement must follow that decision, not the mode alone.
func TestArtifactStageInPlacePlannedMount(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, layout := range []string{"mixed unchanged", "no matcher", "dedicated same name", "missing sibling", "no folder rename"} {
			for _, rollback := range []bool{false, true} {
				kind := "simulated bind mount"
				if native {
					kind = "native cross-device"
				}
				suffix := "/publish"
				if rollback {
					suffix = "/rollback"
				}
				t.Run(kind+"/"+layout+suffix, func(t *testing.T) {
					base := afero.NewOsFs()
					root := t.TempDir()
					incoming := filepath.Join(root, "incoming")
					if native {
						mounted := nativeCrossDeviceDestination(t, root)
						if err := os.Symlink(mounted, incoming); err != nil {
							t.Skipf("directory symlinks unavailable: %v", err)
						}
					} else {
						require.NoError(t, base.Mkdir(incoming, 0755))
					}
					fs := &stagingPlacementFS{Fs: base, mountRoot: incoming}
					source := filepath.Join(incoming, "ABC-123 original.mkv")
					subtitle := filepath.Join(incoming, "ABC-123 original.en.srt")
					part := filepath.Join(incoming, "ABC-123 original-cd2.mkv")
					sources := map[string]string{source: "video", subtitle: "subtitle", part: "part two", filepath.Join(incoming, "keep.txt"): "foreign"}
					for path, content := range sources {
						require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0644))
					}
					folder, finalDir := "renamed", filepath.Join(root, "renamed")
					mode := operationmode.OperationModeInPlace
					m, err := matcher.NewMatcher(&matcher.Config{})
					require.NoError(t, err)
					var matchEngine matcher.MatcherInterface = m
					switch layout {
					case "mixed unchanged":
						unrelated := filepath.Join(incoming, "XYZ-999.mkv")
						sources[unrelated] = "unrelated video"
						require.NoError(t, afero.WriteFile(fs, unrelated, []byte(sources[unrelated]), 0644))
						finalDir = incoming
					case "no matcher":
						matchEngine, finalDir = nil, incoming
					case "dedicated same name":
						folder, finalDir = "incoming", incoming
					case "no folder rename":
						mode, finalDir = operationmode.OperationModeInPlaceNoRenameFolder, incoming
					}
					movie := models.Movie{ID: "ABC-123", ContentID: "inplace-planned-mount", Title: "Mounted movie", RenderGeneration: 1}
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: folder, FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, template.NewEngine(), matchEngine)
					match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}
					plan, err := org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: incoming, OperationMode: mode})
					require.NoError(t, err)
					require.Equal(t, finalDir, plan.TargetDir)
					orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
					orch.organizer = org
					fenceErr := errors.New("publication rejected")
					fence := postPublishFence{movie: &movie}
					if rollback {
						fence.err = fenceErr
					}
					result, err := orch.Execute(t.Context(), ApplyCmd{Movie: &movie, PublicationFence: fence, Match: match, DestPath: incoming, Organize: OrganizeOptions{MoveFiles: false}, GenerateNFO: true, OperationMode: mode})
					if rollback {
						require.ErrorIs(t, err, fenceErr)
						for _, leaf := range []string{"ABC-123.mkv", "ABC-123.eng.srt", "ABC-123-cd2.mkv", "ABC-123.nfo"} {
							require.NoFileExists(t, filepath.Join(finalDir, leaf))
						}
					} else {
						require.NoError(t, err)
						require.Equal(t, filepath.Join(finalDir, "ABC-123.mkv"), result.OrganizeResult.NewPath)
						for leaf, content := range map[string]string{"ABC-123.mkv": "video", "ABC-123.eng.srt": "subtitle", "ABC-123-cd2.mkv": "part two"} {
							data, readErr := afero.ReadFile(fs, filepath.Join(finalDir, leaf))
							require.NoError(t, readErr)
							require.Equal(t, content, string(data))
						}
						nfo, readErr := afero.ReadFile(fs, result.NFOPath)
						require.NoError(t, readErr)
						require.Equal(t, finalDir, filepath.Dir(result.NFOPath))
						require.Contains(t, string(nfo), movie.Title)
					}
					for path, content := range sources {
						data, readErr := afero.ReadFile(fs, path)
						require.NoError(t, readErr)
						require.Equal(t, content, string(data))
					}
					require.Zero(t, fs.crossDeviceAttempts)
					pr260AssertStageGone(t, fs, root)
					pr260AssertStageGone(t, fs, incoming)
				})
			}
		}
	}
}

func TestArtifactStageInPlaceUsesSourcePlan(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := filepath.Join(string(filepath.Separator), "source")
	irrelevantDest := filepath.Join(string(filepath.Separator), "configured-library")
	require.NoError(t, fs.MkdirAll(sourceDir, 0755))
	plan := &organizer.OrganizePlan{SourcePath: filepath.Join(sourceDir, "old.mkv"), TargetPath: filepath.Join(sourceDir, "movie.mkv"), TargetDir: sourceDir}
	orch := &applyOrchImpl{fs: fs, organizer: &stagingPlacementPlanner{plan: plan}}
	root, err := orch.allocateArtifactStageForApply(context.Background(), irrelevantDest, ApplyCmd{OperationMode: operationmode.OperationModeInPlace}, nil)
	require.NoError(t, err)
	require.Equal(t, sourceDir, filepath.Dir(root), "in-place targets are relative to the real source, not the configured library")
	exists, err := afero.Exists(fs, irrelevantDest)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestArtifactStageReadonlyInPlaceStationaryBoundary(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, tc := range []struct {
			name, sidecar                            string
			disabled, download, nfo, rename, wantErr bool
		}{
			{name: "no outputs"},
			{name: "disabled subtitle", sidecar: "ABC-123.en.srt", disabled: true},
			{name: "resident canonical subtitle", sidecar: "ABC-123.srt"},
			{name: "pending normalized subtitle", sidecar: "ABC-123.en.srt", wantErr: true},
			{name: "pending custom subtitle", sidecar: "ABC-123.en.custom", wantErr: true},
			{name: "multipart", sidecar: "ABC-123-cd2.mkv", wantErr: true},
			{name: "metadata", nfo: true, wantErr: true},
			{name: "artwork", download: true, wantErr: true},
			{name: "video rename", rename: true, wantErr: true},
		} {
			t.Run(string(mode)+"/"+tc.name, func(t *testing.T) {
				base := afero.NewOsFs()
				parent := t.TempDir()
				folder := filepath.Join(parent, "movie")
				require.NoError(t, base.Mkdir(folder, 0755))
				source := filepath.Join(folder, "ABC-123.mkv")
				require.NoError(t, afero.WriteFile(base, source, []byte("video"), 0644))
				if tc.sidecar != "" {
					require.NoError(t, afero.WriteFile(base, filepath.Join(folder, tc.sidecar), []byte("sidecar"), 0644))
				}
				fs := &stagingPlacementFS{Fs: base, tempErrors: map[string]error{folder: os.ErrPermission}}
				movie := models.Movie{ID: "ABC-123", ContentID: "readonly-resident", RenderGeneration: 1}
				m, err := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, err)
				fileFormat := "<ID>"
				if tc.rename {
					fileFormat = "renamed"
				}
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: fileFormat, RenameFile: true, MoveSubtitles: !tc.disabled, SubtitleExtensions: []string{".srt", ".custom"}, OperationMode: mode}, template.NewEngine(), m)
				orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
				orch.organizer = org
				cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: folder, OperationMode: mode, Download: tc.download, GenerateNFO: tc.nfo}
				result, err := orch.Execute(t.Context(), cmd)
				if tc.wantErr {
					require.ErrorIs(t, err, os.ErrPermission)
				} else {
					require.NoError(t, err)
					require.Equal(t, source, result.OrganizeResult.NewPath)
					for _, sr := range result.OrganizeResult.Subtitles {
						require.False(t, sr.Copied)
						require.False(t, sr.Moved)
					}
				}
				data, err := afero.ReadFile(base, source)
				require.NoError(t, err)
				require.Equal(t, "video", string(data))
				if tc.sidecar != "" {
					data, err = afero.ReadFile(base, filepath.Join(folder, tc.sidecar))
					require.NoError(t, err)
					require.Equal(t, "sidecar", string(data))
				}
				require.NoFileExists(t, filepath.Join(folder, "ABC-123.eng.srt"))
				require.NoFileExists(t, filepath.Join(folder, "ABC-123.eng.custom"))
				require.NoFileExists(t, filepath.Join(folder, "renamed.mkv"))
				pr260AssertStageGone(t, fs, parent)
				pr260AssertStageGone(t, fs, folder)
			})
		}
	}
}
