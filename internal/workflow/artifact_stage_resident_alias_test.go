package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The writable-parent exception is safe only when every admitted output is
// already resident. Multipart identity belongs to its own source, never to a
// different part that happens to share metadata or an alias at the target.
func TestArtifactStageReadonlyResidentAliases(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, kind := range []string{"primary", "primary and multipart", "wrong multipart", "pending multipart", "pending subtitle", "metadata", "artwork", "same bytes foreign", "move request"} {
			t.Run(string(mode)+"/"+kind, func(t *testing.T) {
				base := afero.NewOsFs()
				parent := t.TempDir()
				folder := filepath.Join(parent, "movie")
				require.NoError(t, base.Mkdir(folder, 0755))
				source, target := filepath.Join(folder, "ABC-123 original.mkv"), filepath.Join(folder, "ABC-123.mkv")
				require.NoError(t, afero.WriteFile(base, source, []byte("video"), 0644))
				if kind == "same bytes foreign" {
					require.NoError(t, afero.WriteFile(base, target, []byte("video"), 0644))
				} else {
					require.NoError(t, os.Link(source, target))
				}
				if kind == "primary and multipart" || kind == "wrong multipart" || kind == "pending multipart" {
					part := filepath.Join(folder, "ABC-123 original-cd2.mkv")
					require.NoError(t, afero.WriteFile(base, part, []byte("part"), 0644))
					if kind == "wrong multipart" {
						part = filepath.Join(folder, "ABC-123 original-cd3.mkv")
						require.NoError(t, afero.WriteFile(base, part, []byte("part"), 0644))
					}
					if kind != "pending multipart" {
						require.NoError(t, os.Link(part, filepath.Join(folder, "ABC-123-cd2.mkv")))
					}
				}
				if kind == "pending subtitle" {
					require.NoError(t, afero.WriteFile(base, filepath.Join(folder, "ABC-123 original.en.srt"), []byte("subtitle"), 0644))
				}
				fs := &stagingPlacementFS{Fs: base, tempErrors: map[string]error{folder: os.ErrPermission}}
				movie := models.Movie{ID: "ABC-123", ContentID: "readonly-alias", RenderGeneration: 1}
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, template.NewEngine(), nil)
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: folder, OperationMode: mode, GenerateNFO: kind == "metadata", Download: kind == "artwork", Organize: OrganizeOptions{MoveFiles: kind == "move request"}}
				plan, err := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: cmd.Match, Movie: &movie, DestDir: folder, OperationMode: mode, MoveFiles: cmd.Organize.MoveFiles})
				require.NoError(t, err)
				require.Equal(t, target, plan.TargetPath)
				orch := &applyOrchImpl{fs: fs, organizer: org}
				root, err := orch.allocateArtifactStageForApply(context.Background(), folder, cmd, plan)
				if kind == "primary" || kind == "primary and multipart" {
					require.NoError(t, err)
					require.Equal(t, parent, filepath.Dir(root))
					require.NoError(t, fs.RemoveAll(root))
					identities := make(map[string]artifactSourceIdentity)
					entries, readErr := afero.ReadDir(base, folder)
					require.NoError(t, readErr)
					for _, entry := range entries {
						path := filepath.Join(folder, entry.Name())
						identities[path] = captureArtifactSourceIdentity(base, path, entry)
						require.True(t, identities[path].hasDevIno)
					}
					for _, reject := range []bool{false, true} {
						apply := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
						apply.organizer = org
						fence := postPublishFence{movie: &movie}
						if reject {
							fence.err = os.ErrPermission
						}
						cmd.PublicationFence = fence
						result, applyErr := apply.Execute(t.Context(), cmd)
						if reject {
							require.ErrorIs(t, applyErr, os.ErrPermission)
						} else {
							require.NoError(t, applyErr)
							require.Equal(t, target, result.OrganizeResult.NewPath)
						}
						for path, identity := range identities {
							current, statErr := os.Lstat(path)
							require.NoError(t, statErr)
							require.True(t, identity.matches(base, path, current), "resident identity changed: %s", path)
						}
					}
				} else {
					require.ErrorIs(t, err, os.ErrPermission)
				}
				pr260AssertStageGone(t, fs, parent)
			})
		}
	}
}
