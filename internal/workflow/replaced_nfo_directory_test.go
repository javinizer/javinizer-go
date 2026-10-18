package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type replacedArtifactFence struct{ movie *models.Movie }

func (f replacedArtifactFence) WithApplyPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}
func (f replacedArtifactFence) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}

type replacedArtifactDownloader struct{ fs afero.Fs }

func (d replacedArtifactDownloader) Download(_ context.Context, cmd downloader.DownloadCmd) (*downloader.DownloadOutcome, error) {
	p := filepath.Join(cmd.DestDir, "poster.jpg")
	if e := d.fs.MkdirAll(cmd.DestDir, 0755); e != nil {
		return nil, e
	}
	if e := afero.WriteFile(d.fs, p, []byte("new poster"), 0644); e != nil {
		return nil, e
	}
	return &downloader.DownloadOutcome{DownloadedPaths: []string{p}, CreatedPaths: []string{p}}, nil
}
func TestMovedDirectoryReplacementSurvivesCleanup(t *testing.T) {
	for _, kind := range []string{"normal", "exact"} {
		for _, artifact := range []string{"nfo", "poster", "both"} {
			for _, state := range []string{"applied", "failed-terminal", "snapshot", "fresh-nfo"} {
				t.Run(kind+"/"+artifact+"/"+state, func(t *testing.T) {
					root := t.TempDir()
					fs := afero.NewOsFs()
					library := filepath.Join(root, "library")
					dir := filepath.Join(library, "incoming")
					require.NoError(t, fs.MkdirAll(dir, 0755))
					base := "abc-123-source"
					if kind == "exact" {
						base = "ABC-123"
					}
					source := filepath.Join(dir, base+".mp4")
					require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0644))
					before, e := fs.Stat(source)
					require.NoError(t, e)
					di, e := fs.Stat(dir)
					require.NoError(t, e)
					oldNFO := "<movie><title>before</title></movie>"
					if state != "fresh-nfo" {
						require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "movie.nfo"), []byte(oldNFO), 0644))
					}
					require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "poster.jpg"), []byte("old poster"), 0644))
					db, e := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "db.sqlite"), LogLevel: "error"})
					require.NoError(t, e)
					require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
					repo := database.NewBatchFileOperationRepository(db)
					movie := models.Movie{ID: "ABC-123", ContentID: "artifact-provenance", Title: "after"}
					engine := template.NewEngine()
					mt, e := matcher.NewMatcher(&matcher.Config{})
					require.NoError(t, e)
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, engine, mt)
					gen := nfo.NewGenerator(fs, &nfo.Config{FilenameTemplate: "movie.nfo", TemplateEngine: engine})
					log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "artifact-provenance", fs, nil, nil, nil)
					orch := newApplyOrchestrator(fs, org, replacedArtifactDownloader{fs}, gen, nil, ApplyConfig{NFONameCfg: nfo.NFONameConfig{FilenameTemplate: "movie.nfo"}}, engine, log, nil, nil)
					cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeInPlace, GenerateNFO: artifact != "poster", Download: artifact != "nfo", OverwriteExistingMedia: true}
					cmd.PublicationFence = replacedArtifactFence{&movie}
					result, e := orch.Execute(context.Background(), cmd)
					require.NoError(t, e)
					require.True(t, result.OrganizeResult.InPlaceRenamed)
					if state == "failed-terminal" {
						seedFailedHistoryStatus(t, repo, result.OperationID)
					}
					var id uint
					_, e = fmt.Sscanf(result.OperationID, "%d", &id)
					require.NoError(t, e)
					row, e := repo.FindByID(context.Background(), id)
					require.NoError(t, e)
					if state == "snapshot" {
						// Exercise persisted snapshot paths after publication; move-mode snapshots
						// must resolve into the restored directory, never recreate the new one.
						row.NFOSnapshot = oldNFO
						require.NoError(t, repo.UpdateNonJournalFields(context.Background(), row))
					}
					require.NoError(t, db.Close())
					db, e = database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "db.sqlite"), LogLevel: "error"})
					require.NoError(t, e)
					defer db.Close()
					repo = database.NewBatchFileOperationRepository(db)
					undo, e := history.NewReverter(fs, repo).RevertBatch(context.Background(), "artifact-provenance")
					require.NoError(t, e)
					require.Equal(t, 1, undo.Succeeded)
					for name, want := range map[string]string{"movie.nfo": oldNFO, "poster.jpg": "old poster"} {
						got, e := afero.ReadFile(fs, filepath.Join(dir, name))
						if state == "fresh-nfo" && name == "movie.nfo" {
							require.True(t, os.IsNotExist(e))
							continue
						}
						if e != nil {
							t.Errorf("original %s missing: %v", name, e)
						} else if string(got) != want {
							t.Errorf("original %s changed: %q", name, got)
						}
					}
					now, e := fs.Stat(source)
					require.NoError(t, e)
					require.True(t, os.SameFile(before, now))
					now, e = fs.Stat(dir)
					require.NoError(t, e)
					require.True(t, os.SameFile(di, now))
					require.Zero(t, undo.Failed)
					_, e = fs.Stat(filepath.Join(library, "ABC-123"))
					require.True(t, os.IsNotExist(e), "Undo must not recreate the relocated directory for an NFO snapshot")
					_, e = history.NewReverter(fs, repo).RevertBatch(context.Background(), "artifact-provenance")
					require.ErrorIs(t, e, history.ErrBatchAlreadyReverted)
				})
			}
		}
	}
}
