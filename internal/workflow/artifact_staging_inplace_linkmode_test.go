package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/require"
)

func TestArtifactInPlaceIgnoresIrrelevantLinkMode(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, link := range []organizer.LinkMode{organizer.LinkModeHard, organizer.LinkModeSoft} {
			for _, move := range []bool{false, true} {
				for _, reject := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/move=%v/reject=%v", mode, link, move, reject), func(t *testing.T) {
						env := setupCopyIntentE2E(t, "inplace-link")
						incoming := filepath.Dir(env.source)
						foreign := map[string]string{env.unrelated: "unrelated", filepath.Join(incoming, "unrelated.nfo"): "existing metadata"}
						require.NoError(t, os.WriteFile(filepath.Join(incoming, "unrelated.nfo"), []byte("existing metadata"), 0644))
						checkForeign := func(dir string) {
							t.Helper()
							for path, want := range foreign {
								body, err := os.ReadFile(filepath.Join(dir, filepath.Base(path)))
								require.NoError(t, err)
								require.Equal(t, want, string(body), "unrelated files and metadata survive apply and Undo")
							}
						}
						sources := []struct{ old, new, body string }{
							{env.source, filepath.Join(incoming, env.movie.ID+" original.mkv"), "video"},
							{env.subtitle, filepath.Join(incoming, env.movie.ID+" original.en.srt"), "subtitle"},
							{env.multipart, filepath.Join(incoming, env.movie.ID+" original-cd2.mkv"), "part two"},
						}
						identities := map[string]artifactSourceIdentity{}
						for _, source := range sources {
							require.NoError(t, env.fs.Rename(source.old, source.new))
							info, err := lstatArtifactSource(env.fs, source.new)
							require.NoError(t, err)
							identities[source.new] = captureArtifactSourceIdentity(env.fs, source.new, info)
							require.True(t, identities[source.new].hasDevIno, "capture native identity before any publication")
						}
						m, err := matcher.NewMatcher(&matcher.Config{})
						require.NoError(t, err)
						env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, template.NewEngine(), m)
						match := models.FileMatchInfo{Path: sources[0].new, Name: filepath.Base(sources[0].new), Extension: ".mkv", MovieID: env.movie.ID}
						cmd := pr260FencedCommand(&env.movie, match, incoming, pr260FencedCounter(t, env.db), mode, false, move, link, false, false)
						if reject {
							cmd.PublicationFence = postPublishFence{movie: &env.movie, err: os.ErrPermission}
						}
						result, applyErr := env.orch.Execute(t.Context(), cmd)
						if reject {
							require.ErrorIs(t, applyErr, os.ErrPermission)
						} else {
							require.NoError(t, applyErr)
						}
						checkSources := func() {
							t.Helper()
							for _, source := range sources {
								body, err := os.ReadFile(source.new)
								require.NoError(t, err, "source preserved: %s", source.new)
								require.Equal(t, source.body, string(body))
								info, err := lstatArtifactSource(env.fs, source.new)
								require.NoError(t, err)
								require.True(t, identities[source.new].matches(env.fs, source.new, info), "original directory entry remains unchanged")
							}
						}
						if !move || reject {
							checkSources()
						} else {
							for _, source := range sources {
								require.NoFileExists(t, source.new)
							}
						}
						finalDir := incoming
						if mode == operationmode.OperationModeInPlace {
							finalDir = filepath.Join(filepath.Dir(incoming), "renamed")
						}
						outputs := []string{filepath.Join(finalDir, env.movie.ID+".mkv"), filepath.Join(finalDir, env.movie.ID+".eng.srt"), filepath.Join(finalDir, env.movie.ID+"-cd2.mkv")}
						if reject {
							for _, output := range outputs {
								_, err := os.Lstat(output)
								require.True(t, os.IsNotExist(err), "rejected publication leaves no copied output")
							}
						} else {
							require.Equal(t, outputs[0], result.OrganizeResult.NewPath)
							require.Len(t, result.OrganizeResult.Subtitles, 1)
							require.Equal(t, !move, result.OrganizeResult.Subtitles[0].Copied)
							require.Equal(t, move, result.OrganizeResult.Subtitles[0].Moved)
							for i, output := range outputs {
								body, err := os.ReadFile(output)
								require.NoError(t, err)
								require.Equal(t, sources[i].body, string(body))
								info, err := os.Lstat(output)
								require.NoError(t, err)
								require.True(t, info.Mode().IsRegular(), "in-place operations publish regular files")
								if !move {
									require.False(t, identities[sources[i].new].matches(env.fs, output, info), "copied output is not a hard link to the original")
								}
							}
						}
						foreignDir := incoming
						if move && !reject && mode == operationmode.OperationModeInPlace {
							// A successful in-place move renames the whole dedicated source folder.
							foreignDir = finalDir
						}
						checkForeign(foreignDir)
						records, err := env.repo.FindByBatchJobID(t.Context(), env.jobID)
						require.NoError(t, err)
						require.Len(t, records, 1)
						expectedOperation := models.OperationTypeCopy
						if move {
							expectedOperation = models.OperationTypeMove
						}
						require.Equal(t, expectedOperation, records[0].OperationType, "history records the effective in-place semantics")
						_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
						require.NoError(t, err)
						checkSources()
						checkForeign(incoming)
						for _, output := range outputs {
							_, err := os.Lstat(output)
							require.True(t, os.IsNotExist(err), "Undo removes only this apply's published output")
						}
					})
				}
			}
		}
	}
}

func TestArtifactResidentInPlaceCopyIgnoresIrrelevantLinkMode(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, link := range []organizer.LinkMode{organizer.LinkModeHard, organizer.LinkModeSoft} {
			t.Run(fmt.Sprintf("%s/%s", mode, link), func(t *testing.T) {
				env := setupCopyIntentE2E(t, "resident-link")
				incoming := filepath.Dir(env.source)
				foreign := map[string]string{env.unrelated: "unrelated", filepath.Join(incoming, "unrelated.nfo"): "existing metadata"}
				require.NoError(t, os.WriteFile(filepath.Join(incoming, "unrelated.nfo"), []byte("existing metadata"), 0644))
				checkForeign := func(dir string) {
					t.Helper()
					for path, want := range foreign {
						body, err := os.ReadFile(filepath.Join(dir, filepath.Base(path)))
						require.NoError(t, err)
						require.Equal(t, want, string(body), "unrelated files and metadata survive apply and Undo")
					}
				}
				sources := []struct{ old, new, body string }{
					{env.source, filepath.Join(incoming, env.movie.ID+".mkv"), "video"},
					{env.subtitle, filepath.Join(incoming, env.movie.ID+".eng.srt"), "subtitle"},
					{env.multipart, filepath.Join(incoming, env.movie.ID+"-cd2.mkv"), "part two"},
				}
				identities := map[string]artifactSourceIdentity{}
				for _, source := range sources {
					require.NoError(t, os.Rename(source.old, source.new))
					info, err := os.Lstat(source.new)
					require.NoError(t, err)
					identities[source.new] = captureArtifactSourceIdentity(env.fs, source.new, info)
					require.True(t, identities[source.new].hasDevIno)
				}
				m, err := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, err)
				env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: filepath.Base(incoming), FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, template.NewEngine(), m)
				match := models.FileMatchInfo{Path: sources[0].new, Name: filepath.Base(sources[0].new), Extension: ".mkv", MovieID: env.movie.ID}
				cmd := pr260FencedCommand(&env.movie, match, incoming, pr260FencedCounter(t, env.db), mode, false, false, link, false, false)
				cmd.Organize.ForceUpdate = false
				result, err := env.orch.Execute(t.Context(), cmd)
				require.NoError(t, err)
				require.Equal(t, sources[0].new, result.OrganizeResult.NewPath)
				require.Len(t, result.OrganizeResult.Subtitles, 1)
				require.True(t, result.OrganizeResult.Subtitles[0].Skipped)
				ledger := p3Ledger(t, env.repo, result.OperationID)
				require.Empty(t, ledger.Delete)
				require.Empty(t, ledger.PlannedDeletes)
				require.Empty(t, ledger.Replacements)
				require.Empty(t, ledger.MoveBack)
				checkForeign(incoming)
				checkSources := func() {
					t.Helper()
					for _, source := range sources {
						body, err := os.ReadFile(source.new)
						require.NoError(t, err)
						require.Equal(t, source.body, string(body))
						info, err := os.Lstat(source.new)
						require.NoError(t, err)
						require.True(t, identities[source.new].matches(env.fs, source.new, info), "resident source remains unowned")
					}
				}
				checkSources()
				_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
				require.NoError(t, err)
				checkSources()
				checkForeign(incoming)
			})
		}
	}
}
