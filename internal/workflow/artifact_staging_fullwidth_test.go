package workflow

import (
	"context"
	"fmt"
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

func TestArtifactStagingFullwidthSubtitlePolicy(t *testing.T) {
	for _, policy := range []struct {
		name       string
		enabled    bool
		extensions []string
		subtitle   string
		target     string
		selected   bool
	}{
		{name: "disabled", extensions: []string{".srt"}, subtitle: "ＲＣＴ－１５６－ＨＤ．ｓｒｔ", target: "movie.srt"},
		{name: "excluded extension", enabled: true, extensions: []string{".ass"}, subtitle: "ＲＣＴ－１５６－ＨＤ．ｓｒｔ", target: "movie.srt"},
		{name: "empty extensions", enabled: true, subtitle: "ＲＣＴ－１５６－ＨＤ．ｓｒｔ", target: "movie.srt"},
		{name: "enabled", enabled: true, extensions: []string{".srt"}, subtitle: "ＲＣＴ－１５６－ＨＤ．ｓｒｔ", target: "movie.srt", selected: true},
		{name: "case insensitive language suffix", enabled: true, extensions: []string{".SRT"}, subtitle: "ＲＣＴ－１５６－ＨＤ．ｅｎ．ＳＲＴ", target: "movie.eng.SRT", selected: true},
		{name: "configured custom extension", enabled: true, extensions: []string{".ttml"}, subtitle: "ＲＣＴ－１５６－ＨＤ．ｔｔｍｌ", target: "movie.ttml", selected: true},
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
				src := filepath.Join(srcDir, "ＲＣＴ－１５６－ＨＤ．ｍｋｖ")
				sub := filepath.Join(srcDir, policy.subtitle)
				part := filepath.Join(srcDir, "ＲＣＴ－１５６－ＨＤ-cd2.mkv")
				unrelated := filepath.Join(srcDir, "ABC-1234.srt")
				require.NoError(t, fs.MkdirAll(srcDir, 0o755))
				for path, body := range map[string]string{src: "video", sub: "subtitle", part: "part two", unrelated: "unrelated"} {
					require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
				}
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, MoveSubtitles: policy.enabled, SubtitleExtensions: policy.extensions, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
				orch := &applyOrchImpl{fs: fs, organizer: org}
				cmd := ApplyCmd{Movie: &models.Movie{ContentID: "subtitle-policy", ID: "RCT-156H"}, Match: models.FileMatchInfo{Path: src, Name: "ＲＣＴ－１５６－ＨＤ.mkv", Extension: ".mkv"}, DestPath: filepath.Join(root, "library"), Organize: OrganizeOptions{MoveFiles: move}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: pr260FailureArtifactFencer{}}

				// Rejected subtitles must never enter staging or its fallback lane.
				stage, _, err := orch.prepareArtifact(context.Background(), cmd)
				require.NoError(t, err)
				require.NotNil(t, stage)
				admitted := make(map[string]bool)
				for _, sibling := range stage.siblings {
					admitted[sibling.sourcePath] = true
				}
				assert.Equal(t, policy.selected, admitted[sub])
				assert.False(t, admitted[part], "raw fullwidth-extension multipart support is outside this reconciliation")
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
				body, err := afero.ReadFile(fs, unrelated)
				require.NoError(t, err)
				assert.Equal(t, "unrelated", string(body))
			})
		}
	}
}

func TestArtifactMatchName(t *testing.T) {
	for _, tc := range []struct{ raw, name, ext string }{
		{"", "", ""},
		{"ＲＣＴ－１５６", "ＲＣＴ－１５６", ""},
		{"ＲＣＴ－１５６．ｍｋｖ", "ＲＣＴ－１５６.mkv", ".mkv"},
		{"ＲＣＴ－１５６．ＭＫＶ", "ＲＣＴ－１５６.MKV", ".MKV"},
		{"ＲＣＴ－１５６.MkV", "ＲＣＴ－１５６.MkV", ".MkV"},
		{"old．stem.new．ｍｋｖ", "old．stem.new.mkv", ".mkv"},
		{"新しい．字幕", "新しい.字幕", ".字幕"},
		{"movie.ｍ　ｋｖ", "movie.m kv", ".m kv"},
		{"movie．／ｍｋｖ", "movie．／ｍｋｖ", ""},
		{"movie.", "movie.", "."},
		{"movie．", "movie.", "."},
		{".hidden", ".hidden", ".hidden"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			name, ext := artifactMatchName(tc.raw)
			assert.Equal(t, tc.name, name)
			assert.Equal(t, tc.ext, ext)
		})
	}
}

type fullwidthRecordingOrganizer struct {
	*organizer.Organizer
	matches []models.FileMatchInfo
}

func (o *fullwidthRecordingOrganizer) PlanOrganize(ctx context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	o.matches = append(o.matches, cmd.Match)
	return o.Organizer.PlanOrganize(ctx, cmd)
}

// In-place copy physically renames the staged video before publication. Its
// final replan must reconstruct the new basename, not retain the scanner name.
func TestArtifactStagingFullwidthRenamedStage(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	srcDir := filepath.Join(root, "incoming")
	src := filepath.Join(srcDir, "ＲＣＴ－１５６－ＨＤ．ＭＫＶ")
	sub := filepath.Join(srcDir, "ＲＣＴ－１５６－ＨＤ．ｅｎ．ＳＲＴ")
	require.NoError(t, fs.MkdirAll(srcDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, sub, []byte("subtitle"), 0o644))
	org := &fullwidthRecordingOrganizer{Organizer: organizer.NewOrganizer(fs, &organizer.Config{
		FileFormat: "renamed", RenameFile: true, MoveSubtitles: true,
		SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
	}, template.NewEngine(), nil)}
	orch := &applyOrchImpl{fs: fs, organizer: org}
	cmd := ApplyCmd{Movie: &models.Movie{ContentID: "fullwidth-renamed", ID: "RCT-156H"}, Match: models.FileMatchInfo{
		Path: src, Name: "ＲＣＴ－１５６－ＨＤ.MKV", Extension: ".MKV",
	}, DestPath: srcDir, Organize: OrganizeOptions{MoveFiles: false}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder, PublicationFence: pr260FailureArtifactFencer{}}
	stage, staged, err := orch.prepareArtifact(t.Context(), cmd)
	require.NoError(t, err)
	require.Equal(t, "ＲＣＴ－１５６－ＨＤ.MKV", staged.Match.Name)
	require.Equal(t, ".MKV", staged.Match.Extension)
	require.Equal(t, "ＲＣＴ－１５６－ＨＤ．ＭＫＶ", filepath.Base(staged.Match.Path))
	stage.cleanup()
	result, err := orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, org.matches)
	finalMatch := org.matches[len(org.matches)-1]
	assert.Equal(t, "renamed.MKV", filepath.Base(finalMatch.Path))
	assert.Equal(t, "renamed.MKV", finalMatch.Name)
	assert.Equal(t, ".MKV", finalMatch.Extension)
	require.NotNil(t, result.OrganizeResult)
	assert.Equal(t, filepath.Join(srcDir, "renamed.MKV"), result.OrganizeResult.NewPath)
	for path, want := range map[string]string{src: "video", sub: "subtitle", filepath.Join(srcDir, "renamed.MKV"): "video", filepath.Join(srcDir, "renamed.eng.SRT"): "subtitle"} {
		body, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		assert.Equal(t, want, string(body))
	}
}

func TestArtifactStagingFullwidthCaseAndMultipartSafety(t *testing.T) {
	for _, folded := range []bool{false, true} {
		for _, move := range []bool{false, true} {
			for _, occupied := range []bool{false, true} {
				name := fmt.Sprintf("casefold=%t/move=%t/occupied=%t", folded, move, occupied)
				t.Run(name, func(t *testing.T) {
					root := t.TempDir()
					var fs afero.Fs = afero.NewOsFs()
					if folded {
						fs = &stagedCaseFoldFS{Fs: fs, root: root}
					}
					srcDir := filepath.Join(root, "incoming")
					// The existing staging multipart selector supports an ASCII
					// extension with a raw fullwidth stem. Keep that admission.
					src := filepath.Join(srcDir, "ｒｃｔ－１５６－ｈｄ.mkv")
					part := filepath.Join(srcDir, "ｒｃｔ－１５６－ｈｄ-cd2.mkv")
					sub := filepath.Join(srcDir, "ｒｃｔ－１５６－ｈｄ．ｅｎ．ｓｒｔ")
					unrelated := filepath.Join(srcDir, "ｒｃｔ－１５６－ｈｄtrailer．ｓｒｔ")
					targetDir := filepath.Join(root, "library", "movie")
					target := filepath.Join(targetDir, "movie.eng.srt")
					require.NoError(t, fs.MkdirAll(srcDir, 0o755))
					for path, body := range map[string]string{src: "video", part: "part two", sub: "subtitle", unrelated: "unrelated"} {
						require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
					}
					if occupied {
						require.NoError(t, fs.MkdirAll(targetDir, 0o755))
						require.NoError(t, afero.WriteFile(fs, target, []byte("foreign subtitle"), 0o644))
					}
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".SRT"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
					orch := &applyOrchImpl{fs: fs, organizer: org}
					cmd := ApplyCmd{Movie: &models.Movie{ContentID: "fullwidth-multipart", ID: "RCT-156H"}, Match: models.FileMatchInfo{Path: src, Name: filepath.Base(src), Extension: ".mkv"}, DestPath: filepath.Dir(targetDir), Organize: OrganizeOptions{MoveFiles: move}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: pr260FailureArtifactFencer{}}
					stage, _, err := orch.prepareArtifact(t.Context(), cmd)
					require.NoError(t, err)
					admitted := map[string]bool{}
					for _, sibling := range stage.siblings {
						admitted[sibling.sourcePath] = true
					}
					require.True(t, admitted[part])
					require.True(t, admitted[sub])
					require.False(t, admitted[unrelated])
					stage.cleanup()
					_, err = orch.Execute(t.Context(), cmd)
					require.NoError(t, err)
					want := "subtitle"
					if occupied {
						want = "foreign subtitle"
					}
					body, err := afero.ReadFile(fs, target)
					require.NoError(t, err)
					assert.Equal(t, want, string(body))
					for path, want := range map[string]string{filepath.Join(targetDir, "movie-cd2.mkv"): "part two", unrelated: "unrelated"} {
						body, err := afero.ReadFile(fs, path)
						require.NoError(t, err)
						assert.Equal(t, want, string(body))
					}
					exists, err := afero.Exists(fs, part)
					require.NoError(t, err)
					assert.Equal(t, !move, exists)
					exists, err = afero.Exists(fs, sub)
					require.NoError(t, err)
					assert.Equal(t, !move || occupied, exists)
				})
			}
		}
	}
}
