package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestArtifactSubtitleSkippedDanglingSubtitle(t *testing.T) {
	for _, move := range []bool{false, true} {
		name := "copy"
		if move {
			name = "move"
		}
		t.Run(name, func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			source := filepath.Join(root, "incoming", "ABC-123.mkv")
			sub := filepath.Join(root, "incoming", "ABC-123.en.srt")
			require.NoError(t, os.MkdirAll(filepath.Dir(source), 0755))
			require.NoError(t, os.WriteFile(source, []byte("video"), 0644))
			require.NoError(t, os.WriteFile(sub, []byte("subtitle"), 0644))
			if !pr260LinkSupported(t, organizer.LinkModeSoft, source) {
				return
			}
			library := filepath.Join(root, "library")
			movieDir := filepath.Join(library, "movie")
			require.NoError(t, os.MkdirAll(movieDir, 0755))
			target := filepath.Join(movieDir, "movie.eng.srt")
			require.NoError(t, os.Symlink(filepath.Join(root, "missing"), target))
			movie := models.Movie{ID: "ABC-123", ContentID: "dangling-sub", RenderGeneration: 1}
			org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			orch := &applyOrchImpl{fs: fs, organizer: org}
			cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: move}, OperationMode: operationmode.OperationModeOrganize}
			result, err := orch.Execute(context.Background(), cmd)
			require.NoError(t, err)
			require.Len(t, result.OrganizeResult.Subtitles, 1)
			require.True(t, result.OrganizeResult.Subtitles[0].Skipped)
			require.FileExists(t, sub)
			_, err = os.Lstat(target)
			require.NoError(t, err)
			_, err = os.Stat(filepath.Join(movieDir, "movie.en.srt"))
			require.True(t, os.IsNotExist(err), "refused subtitle must not get installed under unnormalized alternate name")
		})
	}
}

func TestArtifactSubtitleSkippedDanglingSubtitleForeignFallback(t *testing.T) {
	for _, move := range []bool{false, true} {
		name := "copy"
		if move {
			name = "move"
		}
		t.Run(name, func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			source := filepath.Join(root, "incoming", "ABC-123.mkv")
			sub := filepath.Join(root, "incoming", "ABC-123.en.srt")
			require.NoError(t, os.MkdirAll(filepath.Dir(source), 0755))
			require.NoError(t, os.WriteFile(source, []byte("video"), 0644))
			require.NoError(t, os.WriteFile(sub, []byte("subtitle"), 0644))
			if !pr260LinkSupported(t, organizer.LinkModeSoft, source) {
				return
			}
			library := filepath.Join(root, "library")
			movieDir := filepath.Join(library, "movie")
			require.NoError(t, os.MkdirAll(movieDir, 0755))
			target := filepath.Join(movieDir, "movie.eng.srt")
			require.NoError(t, os.Symlink(filepath.Join(root, "missing"), target))
			fallback := filepath.Join(movieDir, "movie.en.srt")
			require.NoError(t, os.WriteFile(fallback, []byte("foreign subtitle must survive"), 0644))
			movie := models.Movie{ID: "ABC-123", ContentID: "dangling-sub", RenderGeneration: 1}
			org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
			orch := &applyOrchImpl{fs: fs, organizer: org}
			cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: move}, OperationMode: operationmode.OperationModeOrganize}
			result, err := orch.Execute(context.Background(), cmd)
			require.NoError(t, err)
			require.Len(t, result.OrganizeResult.Subtitles, 1)
			require.True(t, result.OrganizeResult.Subtitles[0].Skipped)
			require.FileExists(t, sub)
			_, err = os.Lstat(target)
			require.NoError(t, err)
			body, err := os.ReadFile(fallback)
			require.NoError(t, err)
			require.Equal(t, "foreign subtitle must survive", string(body), "refused subtitle must not overwrite a foreign unnormalized destination")
		})
	}
}

func TestArtifactSubtitleDanglingSubtitleDurableForeignFallback(t *testing.T) {
	env := setupCopyIntentE2E(t, "adversarial-dangling-foreign")
	if !pr260LinkSupported(t, organizer.LinkModeSoft, env.source) {
		return
	}
	old := env.subtitle
	env.subtitle = old[:len(old)-len(".srt")] + ".en.srt"
	require.NoError(t, os.Rename(old, env.subtitle))
	plan, err := env.org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	subs := env.org.PlanSubtitleMoves(plan)
	require.Len(t, subs, 1)
	target := subs[0].NewPath
	fallback := target[:len(target)-len(".eng.srt")] + ".en.srt"
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.Symlink(filepath.Join(filepath.Dir(env.dest), "missing"), target))
	require.NoError(t, os.WriteFile(fallback, []byte("foreign subtitle must survive"), 0644))
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.Len(t, result.OrganizeResult.Subtitles, 1)
	require.True(t, result.OrganizeResult.Subtitles[0].Skipped)
	body, err := os.ReadFile(fallback)
	require.NoError(t, err)
	require.Equal(t, "foreign subtitle must survive", string(body), "skipped source cannot replace foreign fallback")
	ledger := p3Ledger(t, env.repo, result.OperationID)
	for _, path := range []string{target, fallback} {
		require.NotContains(t, ledger.Delete, path)
		for _, pin := range ledger.PlannedDeletes {
			require.NotEqual(t, path, pin.Path)
		}
		for _, replacement := range ledger.Replacements {
			require.NotEqual(t, path, replacement.Destination)
		}
	}
	reverted, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	require.Equal(t, 1, reverted.Succeeded)
	body, err = os.ReadFile(fallback)
	require.NoError(t, err)
	require.Equal(t, "foreign subtitle must survive", string(body), "revert must retain the foreign fallback")
	require.FileExists(t, env.subtitle)

}

func TestArtifactSubtitleVacatedCanonicalForeignFallback(t *testing.T) {
	env := setupCopyIntentE2E(t, "adversarial-vacated-foreign")
	old := env.subtitle
	env.subtitle = old[:len(old)-len(".srt")] + ".en.srt"
	require.NoError(t, os.Rename(old, env.subtitle))
	plan, err := env.org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	subs := env.org.PlanSubtitleMoves(plan)
	require.Len(t, subs, 1)
	target := subs[0].NewPath
	fallback := target[:len(target)-len(".eng.srt")] + ".en.srt"
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, []byte("canonical foreign"), 0644))
	require.NoError(t, os.WriteFile(fallback, []byte("alternate foreign must survive"), 0644))
	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: env.org, preExecute: func(*organizer.OrganizePlan) { require.NoError(t, os.Remove(target)) }}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.Len(t, result.OrganizeResult.Subtitles, 1)
	require.True(t, result.OrganizeResult.Subtitles[0].Skipped)
	body, err := os.ReadFile(fallback)
	require.NoError(t, err)
	require.Equal(t, "alternate foreign must survive", string(body), "vacated normalized destination cannot license overwriting a different occupied destination")
	ledger := p3Ledger(t, env.repo, result.OperationID)
	for _, path := range []string{target, fallback} {
		require.NotContains(t, ledger.Delete, path)
		for _, pin := range ledger.PlannedDeletes {
			require.NotEqual(t, path, pin.Path)
		}
		for _, replacement := range ledger.Replacements {
			require.NotEqual(t, path, replacement.Destination)
		}
	}
	reverted, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	require.Equal(t, 1, reverted.Succeeded)
	body, err = os.ReadFile(fallback)
	require.NoError(t, err)
	require.Equal(t, "alternate foreign must survive", string(body), "revert must retain the foreign fallback")
	require.FileExists(t, env.subtitle)

}

func TestArtifactSubtitleInPlaceSubtitlePolicies(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, move := range []bool{false, true} {
			for _, policy := range []struct {
				name, ext string
				enabled   bool
			}{{"disabled", ".SRT", false}, {"case-language", ".SRT", true}, {"custom", ".ttml", true}} {
				name := string(mode) + "/copy/" + policy.name
				if move {
					name = string(mode) + "/move/" + policy.name
				}
				t.Run(name, func(t *testing.T) {
					fs := afero.NewOsFs()
					root := t.TempDir()
					incoming := filepath.Join(root, "incoming")
					require.NoError(t, os.Mkdir(incoming, 0755))
					source := filepath.Join(incoming, "ABC-123 original.mkv")
					sub := filepath.Join(incoming, "ABC-123 original.en"+policy.ext)
					require.NoError(t, os.WriteFile(source, []byte("video"), 0644))
					require.NoError(t, os.WriteFile(sub, []byte("sub"), 0644))
					movie := models.Movie{ID: "ABC-123", ContentID: "inplace-policy", RenderGeneration: 1}
					m, err := matcher.NewMatcher(&matcher.Config{})
					require.NoError(t, err)
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: policy.enabled, SubtitleExtensions: []string{policy.ext}, OperationMode: mode}, template.NewEngine(), m)
					orch := &applyOrchImpl{fs: fs, organizer: org}
					cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: incoming, Organize: OrganizeOptions{MoveFiles: move}, OperationMode: mode}
					result, err := orch.Execute(t.Context(), cmd)
					require.NoError(t, err)
					dir := filepath.Dir(result.OrganizeResult.NewPath)
					require.FileExists(t, result.OrganizeResult.NewPath)
					if !move {
						require.FileExists(t, source)
						b, e := os.ReadFile(sub)
						require.NoError(t, e)
						require.Equal(t, "sub", string(b))
					}
					if policy.enabled {
						require.Len(t, result.OrganizeResult.Subtitles, 1)
						b, e := os.ReadFile(filepath.Join(dir, "ABC-123.eng"+policy.ext))
						require.NoError(t, e)
						require.Equal(t, "sub", string(b))
						require.NoFileExists(t, filepath.Join(dir, "ABC-123.en"+policy.ext))
						if filepath.Clean(dir) != filepath.Clean(incoming) {
							require.NoFileExists(t, filepath.Join(dir, filepath.Base(sub)), "the old subtitle leaf is not an extra output either")
						}
					} else {
						require.Empty(t, result.OrganizeResult.Subtitles)
						require.NoFileExists(t, filepath.Join(dir, "ABC-123.eng"+policy.ext))
					}
				})
			}
		}
	}
}

func TestArtifactSubtitleInPlaceCopyForeignFallback(t *testing.T) {
	env := setupCopyIntentE2E(t, "adversarial-inplace-foreign")
	old := env.subtitle
	env.subtitle = old[:len(old)-len(".srt")] + ".en.srt"
	require.NoError(t, os.Rename(old, env.subtitle))
	incoming := filepath.Dir(env.source)
	fallback := filepath.Join(incoming, env.movie.ID+".en.srt")
	require.NoError(t, os.WriteFile(fallback, []byte("foreign subtitle must survive"), 0644))
	env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "movie", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
	cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	require.FileExists(t, env.source)
	require.FileExists(t, env.subtitle)
	b, err := os.ReadFile(fallback)
	require.NoError(t, err)
	require.Equal(t, "foreign subtitle must survive", string(b), "in-place CopyOnly cannot overwrite alternate subtitle")
}

// A failed publisher is terminal for its admitted subtitle source. The
// workflow may finish unrelated work, but cannot retry under a different name.
type errorSubtitlePublisher struct {
	*organizer.Organizer
	source string
	omit   bool
}

func (o errorSubtitlePublisher) SubtitleSourcePaths(models.FileMatchInfo) []string {
	return []string{o.source}
}
func (o errorSubtitlePublisher) PlanSubtitleMoves(p *organizer.OrganizePlan) []models.SubtitleMove {
	stem := strings.TrimSuffix(filepath.Base(p.TargetPath), filepath.Ext(p.TargetPath))
	return []models.SubtitleMove{{OriginalPath: o.source, NewPath: filepath.Join(p.TargetDir, stem+".eng.srt")}}
}
func (o errorSubtitlePublisher) ExecuteOrganizePlan(p *organizer.OrganizePlan, move bool, link organizer.LinkMode) (*organizer.OrganizeResult, error) {
	result, err := o.Organizer.ExecuteOrganizePlan(p, move, link)
	if result != nil && !o.omit {
		result.Subtitles = []organizer.SubtitleResult{{SubtitleMove: o.PlanSubtitleMoves(p)[0], Error: os.ErrPermission}}
	}
	return result, err
}

func TestDeferredSubtitleErrorNeverPublishesFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		move, omit bool
	}{{"copy-error", false, false}, {"move-error", true, false}, {"copy-omitted", false, true}, {"move-omitted", true, true}} {
		move, name := tc.move, tc.name
		t.Run(name, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "failed-subtitle-"+name)
			old := env.subtitle
			env.subtitle = strings.TrimSuffix(old, ".srt") + ".en.srt"
			require.NoError(t, env.fs.Rename(old, env.subtitle))
			org := errorSubtitlePublisher{Organizer: organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "movie", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil), source: env.subtitle, omit: tc.omit}
			env.orch.organizer = org
			plan, err := org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: env.dest, OperationMode: operationmode.OperationModeOrganize})
			require.NoError(t, err)
			target := org.PlanSubtitleMoves(plan)[0].NewPath
			fallback := strings.TrimSuffix(target, ".eng.srt") + ".en.srt"
			require.NoError(t, env.fs.MkdirAll(filepath.Dir(fallback), 0755))
			require.NoError(t, afero.WriteFile(env.fs, fallback, []byte("foreign fallback"), 0644))
			cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, move, organizer.LinkModeNone, false, false)
			result, err := env.orch.Execute(t.Context(), cmd)
			require.NoError(t, err)
			if tc.omit {
				require.Empty(t, result.OrganizeResult.Subtitles)
			} else {
				require.Len(t, result.OrganizeResult.Subtitles, 1)
				require.ErrorIs(t, result.OrganizeResult.Subtitles[0].Error, os.ErrPermission)
			}
			require.NoFileExists(t, target)
			for _, path := range []string{env.subtitle, fallback} {
				require.FileExists(t, path)
			}
			ledger := p3Ledger(t, env.repo, result.OperationID)
			for _, path := range []string{target, fallback} {
				require.NotContains(t, ledger.Delete, path)
				for _, pin := range ledger.PlannedDeletes {
					require.NotEqual(t, path, pin.Path)
				}
				for _, replacement := range ledger.Replacements {
					require.NotEqual(t, path, replacement.Destination)
				}
				for _, m := range ledger.MoveBack {
					require.NotEqual(t, path, m.NewPath)
				}
			}
			_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			require.NoError(t, err)
			body, err := afero.ReadFile(env.fs, fallback)
			require.NoError(t, err)
			require.Equal(t, "foreign fallback", string(body))
			body, err = afero.ReadFile(env.fs, env.subtitle)
			require.NoError(t, err)
			require.Equal(t, "subtitle", string(body))
		})
	}
}
