package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactStagingSubtitlePolicy(t *testing.T) {
	for _, policy := range []struct {
		name       string
		enabled    bool
		extensions []string
		subtitle   string
		target     string
		selected   bool
	}{
		{name: "disabled", extensions: []string{".srt"}, subtitle: "ABC-123.srt", target: "movie.srt"},
		{name: "excluded extension", enabled: true, extensions: []string{".ass"}, subtitle: "ABC-123.srt", target: "movie.srt"},
		{name: "empty extensions", enabled: true, subtitle: "ABC-123.srt", target: "movie.srt"},
		{name: "enabled", enabled: true, extensions: []string{".srt"}, subtitle: "ABC-123.srt", target: "movie.srt", selected: true},
		{name: "case insensitive language suffix", enabled: true, extensions: []string{".SRT"}, subtitle: "abc-123.en.SRT", target: "movie.eng.SRT", selected: true},
		{name: "configured custom extension", enabled: true, extensions: []string{".ttml"}, subtitle: "ABC-123.ttml", target: "movie.ttml", selected: true},
	} {
		for _, move := range []bool{true, false} {
			mode := "copy"
			if move {
				mode = "move"
			}
			t.Run(policy.name+"/"+mode, func(t *testing.T) {
				fs := afero.NewOsFs()
				root := t.TempDir()
				srcDir := filepath.Join(root, "incoming")
				src := filepath.Join(srcDir, "ABC-123.mp4")
				sub := filepath.Join(srcDir, policy.subtitle)
				part := filepath.Join(srcDir, "ABC-123-cd2.mp4")
				unrelated := filepath.Join(srcDir, "ABC-1234.srt")
				require.NoError(t, fs.MkdirAll(srcDir, 0o755))
				for path, body := range map[string]string{src: "video", sub: "subtitle", part: "part two", unrelated: "unrelated"} {
					require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
				}
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, MoveSubtitles: policy.enabled, SubtitleExtensions: policy.extensions, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
				orch := &applyOrchImpl{fs: fs, organizer: org}
				cmd := ApplyCmd{Movie: &models.Movie{ContentID: "subtitle-policy", ID: "ABC-123"}, Match: models.FileMatchInfo{Path: src, Name: filepath.Base(src), Extension: ".mp4"}, DestPath: filepath.Join(root, "library"), Organize: OrganizeOptions{MoveFiles: move}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: pr260FailureArtifactFencer{}}

				// Rejected subtitles must never enter staging or its fallback lane.
				stage, _, err := orch.prepareArtifact(context.Background(), cmd)
				require.NoError(t, err)
				require.NotNil(t, stage)
				admitted := make(map[string]bool)
				for _, sibling := range stage.siblings {
					admitted[sibling.sourcePath] = true
				}
				assert.Equal(t, policy.selected, admitted[sub])
				assert.True(t, admitted[part], "multipart selection is independent of subtitle settings")
				assert.False(t, admitted[unrelated])
				stage.cleanup()

				result, err := orch.Execute(context.Background(), cmd)
				require.NoError(t, err)
				require.NotNil(t, result.OrganizeResult)
				targetDir := filepath.Dir(result.OrganizeResult.NewPath)
				targetSub := filepath.Join(targetDir, policy.target)
				sourceExists, err := afero.Exists(fs, sub)
				require.NoError(t, err)
				assert.Equal(t, !policy.selected || !move, sourceExists, "only selected subtitles may be consumed")
				targetExists, err := afero.Exists(fs, targetSub)
				require.NoError(t, err)
				assert.Equal(t, policy.selected, targetExists, "only selected subtitles may be published")
				if policy.selected {
					require.Len(t, result.OrganizeResult.Subtitles, 1)
					assert.Equal(t, move, result.OrganizeResult.Subtitles[0].Moved)
					assert.Equal(t, !move, result.OrganizeResult.Subtitles[0].Copied)
					body, err := afero.ReadFile(fs, targetSub)
					require.NoError(t, err)
					assert.Equal(t, "subtitle", string(body))
				} else {
					assert.Empty(t, result.OrganizeResult.Subtitles)
					body, err := afero.ReadFile(fs, sub)
					require.NoError(t, err)
					assert.Equal(t, "subtitle", string(body))
				}
				partBody, err := afero.ReadFile(fs, filepath.Join(targetDir, "movie-cd2.mp4"))
				require.NoError(t, err)
				assert.Equal(t, "part two", string(partBody))
				partRetained, err := afero.Exists(fs, part)
				require.NoError(t, err)
				assert.Equal(t, !move, partRetained)
				body, err := afero.ReadFile(fs, unrelated)
				require.NoError(t, err)
				assert.Equal(t, "unrelated", string(body))
			})
		}
	}
}

// subtitleStagingOrganizer gives prepare-only fixtures an explicit subtitle
// policy instead of relying on admission without an organizer.
func subtitleStagingOrganizer(fs afero.Fs) *organizer.Organizer {
	return organizer.NewOrganizer(fs, &organizer.Config{MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, nil, nil)
}

// stagedSubtitleFallbackOrganizer models a selected subtitle the execution
// seam leaves to the generic staged-sibling lane. Fault-injection tests use
// this explicitly so their fallback/rollback coverage does not depend on
// disabled subtitles being admitted by the real organizer.
type stagedSubtitleFallbackOrganizer struct {
	deferredOrganizerBase
	selector *organizer.Organizer
}

func (o stagedSubtitleFallbackOrganizer) SubtitleSourcePaths(match models.FileMatchInfo) []string {
	return o.selector.SubtitleSourcePaths(match)
}

func withStagedSubtitleFallback(fs afero.Fs, org organizer.OrganizerInterface) organizer.OrganizerInterface {
	return stagedSubtitleFallbackOrganizer{deferredOrganizerBase: deferredOrganizerBase{org}, selector: subtitleStagingOrganizer(fs)}
}

// Hide the selection capability to verify an unrelated organizer implementation
// cannot accidentally opt in to subtitle publication through the fallback.
type organizerWithoutSubtitleSelection struct {
	organizer.OrganizerInterface
}

func TestArtifactStagingWithoutSubtitleSelectorAdmitsOnlyMultipart(t *testing.T) {
	fs, root, _, subtitle, multipart, _, match := pr260FencedFiles(t, "subtitle-no-selection")
	org := organizerWithoutSubtitleSelection{subtitleStagingOrganizer(fs)}
	orch := &applyOrchImpl{fs: fs, organizer: org}
	cmd := ApplyCmd{Movie: &models.Movie{ContentID: "subtitle-no-selection"}, Match: match, DestPath: filepath.Join(root, "library"), Organize: OrganizeOptions{MoveFiles: true}, PublicationFence: pr260FailureArtifactFencer{}}
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	require.Len(t, stage.siblings, 1)
	assert.Equal(t, multipart, stage.siblings[0].sourcePath)
	exists, err := afero.Exists(fs, filepath.Join(filepath.Dir(stage.stagedSource), filepath.Base(subtitle)))
	require.NoError(t, err)
	assert.False(t, exists, "no subtitle staging without the organizer's selection policy")
}
