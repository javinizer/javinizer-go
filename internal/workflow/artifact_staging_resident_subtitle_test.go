package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A resident primary makes execute a no-op, including subtitle normalization.
// Planned sidecars must not acquire publication claims or durable delete intent.
func TestDeferredResidentSubtitleNoOpRepeatAndRevert(t *testing.T) {
	for _, mode := range []struct {
		name string
		link organizer.LinkMode
	}{{"copy", organizer.LinkModeNone}, {"hardlink", organizer.LinkModeHard}, {"symlink", organizer.LinkModeSoft}} {
		for _, policy := range []string{"enabled", "disabled", "excluded"} {
			for _, multipart := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/multipart=%v", mode.name, policy, multipart), func(t *testing.T) {
					fs := afero.NewOsFs()
					root := t.TempDir()
					library := filepath.Join(root, "library")
					dir := filepath.Join(library, "ABC-123")
					source := filepath.Join(dir, "ABC-123.mp4")
					subtitle := filepath.Join(dir, "ABC-123.en.srt")
					target := filepath.Join(dir, "ABC-123.eng.srt")
					require.NoError(t, fs.MkdirAll(dir, 0755))
					bodies := map[string]string{source: "resident video", subtitle: "resident subtitle"}
					if multipart {
						bodies[filepath.Join(dir, "ABC-123-cd2.mp4")] = "resident part two"
					}
					identities := map[string]artifactSourceIdentity{}
					for path, body := range bodies {
						require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0644))
						info, err := fs.Stat(path)
						require.NoError(t, err)
						identities[path] = captureArtifactSourceIdentity(fs, path, info)
						require.True(t, identities[path].hasDevIno)
					}
					db, _ := pr260ArtifactDB(t)
					repo := database.NewBatchFileOperationRepository(db)
					movie := models.Movie{ID: "ABC-123", ContentID: "resident-subtitle"}
					exts := []string{".srt"}
					if policy == "excluded" {
						exts = []string{".ass"}
					}
					engine := template.NewEngine()
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: policy != "disabled", SubtitleExtensions: exts, OperationMode: operationmode.OperationModeOrganize}, engine, nil)
					orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "resident-subtitle", fs, nil, nil, nil), nil, nil)
					cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: false, LinkMode: mode.link}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: postPublishFence{movie: &movie}}
					check := func() {
						t.Helper()
						for path, body := range bodies {
							got, err := afero.ReadFile(fs, path)
							require.NoError(t, err)
							require.Equal(t, body, string(got))
							info, err := fs.Stat(path)
							require.NoError(t, err)
							require.True(t, identities[path].matches(fs, path, info), "resident identity changed: %s", path)
						}
						entries, err := afero.ReadDir(fs, dir)
						require.NoError(t, err)
						assert.Len(t, entries, len(bodies), "no normalization or busy marker may survive")
						_, err = fs.Stat(target)
						require.True(t, os.IsNotExist(err))
						pr260AssertStageGone(t, fs, library)
					}
					for attempt := 0; attempt < 2; attempt++ {
						result, err := orch.Execute(context.Background(), cmd)
						require.NoError(t, err)
						require.NotNil(t, result.OrganizeResult)
						require.False(t, result.OrganizeResult.Moved)
						ledger := p3Ledger(t, repo, result.OperationID)
						assert.Empty(t, ledger.PlannedDeletes)
						require.Empty(t, ledger.Delete)
						require.Empty(t, ledger.MoveBack)
						require.Empty(t, ledger.Replacements)
						check()
					}
					// A same-content foreign output after the no-op must never be owned by Undo.
					require.NoError(t, afero.WriteFile(fs, target, []byte(bodies[subtitle]), 0644))
					foreignInfo, err := fs.Stat(target)
					require.NoError(t, err)
					foreign := captureArtifactSourceIdentity(fs, target, foreignInfo)
					result, err := history.NewReverter(fs, repo).RevertBatch(context.Background(), "resident-subtitle")
					require.NoError(t, err)
					require.Equal(t, 2, result.Succeeded)
					foreignInfo, err = fs.Stat(target)
					require.NoError(t, err)
					require.True(t, foreign.matches(fs, target, foreignInfo))
					got, err := afero.ReadFile(fs, target)
					require.NoError(t, err)
					require.Equal(t, bodies[subtitle], string(got))
					require.NoError(t, fs.Remove(target))
					check()
				})
			}
		}
	}
}
