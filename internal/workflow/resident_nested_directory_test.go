package workflow

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type residentNestedArtifactDownloader struct{ fs afero.Fs }

func (d residentNestedArtifactDownloader) Download(_ context.Context, cmd downloader.DownloadCmd) (*downloader.DownloadOutcome, error) {
	var paths []string
	for _, rel := range []string{"owned/deep/poster.jpg", "shared/generated.jpg"} {
		p := filepath.Join(cmd.DestDir, rel)
		if err := d.fs.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return nil, err
		}
		if err := afero.WriteFile(d.fs, p, []byte("generated"), 0644); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return &downloader.DownloadOutcome{DownloadedPaths: paths, CreatedPaths: paths}, nil
}
func TestResidentNestedArtifactDirectoryUndo(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, mutation := range []string{"none", "missing-source", "missing-both", "empty-root", "occupied-original"} {
			if mutation == "occupied-original" && mode != operationmode.OperationModeInPlace {
				continue
			}
			t.Run(string(mode)+"/"+mutation, func(t *testing.T) {
				fs := afero.NewOsFs()
				root := t.TempDir()
				dir := filepath.Join(root, "incoming")
				require.NoError(t, fs.MkdirAll(filepath.Join(dir, "shared"), 0755))
				untouched := filepath.Join(dir, "untouched-empty")
				outside := filepath.Join(root, "outside-empty")
				require.NoError(t, fs.Mkdir(untouched, 0755))
				require.NoError(t, fs.Mkdir(outside, 0755))
				source := filepath.Join(dir, "abc-123-source.mp4")
				alias := filepath.Join(dir, "ABC-123.mp4")
				require.NoError(t, afero.WriteFile(fs, source, []byte("resident primary"), 0644))
				require.NoError(t, os.Link(source, alias))
				foreign := filepath.Join(dir, "shared", "foreign.jpg")
				require.NoError(t, afero.WriteFile(fs, foreign, []byte("foreign payload"), 0644))
				primaryBefore, err := statResidentTestIdentity(t, fs, source)
				require.NoError(t, err)
				dirBefore, err := statResidentTestIdentity(t, fs, dir)
				require.NoError(t, err)
				foreignBefore, err := statResidentTestIdentity(t, fs, foreign)
				require.NoError(t, err)
				dsn := filepath.Join(root, "ledger.sqlite")
				db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
				require.NoError(t, err)
				require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
				repo := database.NewBatchFileOperationRepository(db)
				movie := models.Movie{ID: "ABC-123", ContentID: "nested-resident"}
				engine := template.NewEngine()
				mt, err := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, err)
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, engine, mt)
				orch := newApplyOrchestrator(fs, org, residentNestedArtifactDownloader{fs}, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "nested-resident", fs, nil, nil, nil), nil, nil)
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: mode, Download: true, PublicationFence: independentArtifactFence{&movie}}
				result, err := orch.Execute(t.Context(), cmd)
				require.NoError(t, err)
				require.True(t, result.OrganizeResult.PrimaryResident)
				rows, err := repo.FindByBatchJobID(t.Context(), "nested-resident")
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Empty(t, rows[0].NewPath)
				t.Logf("source=%s final=%s newPath=%q ledger=%s", source, result.OrganizeResult.FolderPath, rows[0].NewPath, rows[0].GeneratedFiles)
				require.FileExists(t, filepath.Join(result.OrganizeResult.FolderPath, "owned", "deep", "poster.jpg"))
				publishedDir := result.OrganizeResult.FolderPath
				if mutation == "missing-source" || mutation == "missing-both" || mutation == "empty-root" {
					require.NoError(t, fs.Remove(filepath.Join(publishedDir, filepath.Base(source))))
				}
				if mutation == "missing-both" || mutation == "empty-root" {
					require.NoError(t, fs.Remove(filepath.Join(publishedDir, filepath.Base(alias))))
				}
				if mutation == "empty-root" {
					require.NoError(t, fs.Remove(filepath.Join(publishedDir, "shared", "foreign.jpg")))
					require.NoError(t, fs.Remove(filepath.Join(publishedDir, "untouched-empty")))
				}
				if mutation == "occupied-original" {
					require.NoError(t, fs.Mkdir(dir, 0755))
					require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "occupant"), []byte("foreign directory"), 0644))
				}
				require.NoError(t, db.Close())
				db, err = database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
				require.NoError(t, err)
				defer db.Close()
				repo = database.NewBatchFileOperationRepository(db)
				undo, err := history.NewReverter(fs, repo).RevertBatch(t.Context(), "nested-resident")
				require.NoError(t, err)
				if mutation == "occupied-original" {
					require.Zero(t, undo.Succeeded)
					require.Equal(t, 1, undo.Failed)
					require.FileExists(t, filepath.Join(publishedDir, "owned", "deep", "poster.jpg"))
					body, err := afero.ReadFile(fs, filepath.Join(dir, "occupant"))
					require.NoError(t, err)
					require.Equal(t, "foreign directory", string(body))
					return
				}
				require.Equal(t, 1, undo.Succeeded)
				require.Zero(t, undo.Failed)
				for path, before := range map[string]os.FileInfo{source: primaryBefore, alias: primaryBefore, dir: dirBefore, foreign: foreignBefore} {
					if (path == source && (mutation == "missing-source" || mutation == "missing-both" || mutation == "empty-root")) || (path == alias && (mutation == "missing-both" || mutation == "empty-root")) || (path == foreign && mutation == "empty-root") {
						_, err := fs.Stat(path)
						require.True(t, os.IsNotExist(err), "Undo must not recreate a missing resident primary")
						continue
					}
					now, err := statResidentTestIdentity(t, fs, path)
					require.NoError(t, err)
					require.True(t, sameResidentTestIdentity(before, now), path)
				}
				for _, rel := range []string{"owned/deep/poster.jpg", "shared/generated.jpg"} {
					_, err := fs.Stat(filepath.Join(dir, rel))
					require.True(t, os.IsNotExist(err), "generated artifact retained")
				}
				if mutation != "empty-root" {
					body, err := afero.ReadFile(fs, foreign)
					require.NoError(t, err)
					require.Equal(t, "foreign payload", string(body))
				}
				_, err = fs.Stat(filepath.Join(dir, "owned"))
				require.True(t, os.IsNotExist(err), "successful Undo must prune owned empty nested artifact directories")
				if mutation != "empty-root" {
					require.DirExists(t, untouched, "cleanup must not sweep unrelated empty trees")
				}
				require.DirExists(t, outside, "cleanup must not cross the resident directory boundary")
			})
		}
	}
}
