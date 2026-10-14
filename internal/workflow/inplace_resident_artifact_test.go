package workflow

import (
	"context"
	"fmt"
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
	"os"
	"path/filepath"
	"testing"
)

type independentArtifactFence struct{ movie *models.Movie }

func (f independentArtifactFence) WithApplyPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}
func (f independentArtifactFence) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}

type independentArtifactDownloader struct{ fs afero.Fs }

func (d independentArtifactDownloader) Download(_ context.Context, cmd downloader.DownloadCmd) (*downloader.DownloadOutcome, error) {
	p := filepath.Join(cmd.DestDir, "poster.jpg")
	if e := d.fs.MkdirAll(cmd.DestDir, 0755); e != nil {
		return nil, e
	}
	if e := afero.WriteFile(d.fs, p, []byte("new poster"), 0644); e != nil {
		return nil, e
	}
	return &downloader.DownloadOutcome{DownloadedPaths: []string{p}, CreatedPaths: []string{p}}, nil
}
func TestInPlaceResidentDirectoryArtifactUndo(t *testing.T) {
	for _, kind := range []string{"hardlink"} {
		for _, artifact := range []string{"nfo", "poster", "both"} {
			t.Run(kind+"/"+artifact, func(t *testing.T) {
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
				target := filepath.Join(dir, "ABC-123.mp4")
				require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0644))
				if kind == "hardlink" {
					require.NoError(t, os.Link(source, target))
				}
				before, e := fs.Stat(source)
				require.NoError(t, e)
				di, e := fs.Stat(dir)
				require.NoError(t, e)
				oldNFO := "<movie><title>before</title></movie>"
				require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "movie.nfo"), []byte(oldNFO), 0644))
				require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "poster.jpg"), []byte("old poster"), 0644))
				db, e := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "db.sqlite"), LogLevel: "error"})
				require.NoError(t, e)
				defer db.Close()
				require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
				repo := database.NewBatchFileOperationRepository(db)
				movie := models.Movie{ID: "ABC-123", ContentID: "artifact-provenance", Title: "after"}
				engine := template.NewEngine()
				mt, e := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, e)
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, engine, mt)
				gen := nfo.NewGenerator(fs, &nfo.Config{FilenameTemplate: "movie.nfo", TemplateEngine: engine})
				orch := newApplyOrchestrator(fs, org, independentArtifactDownloader{fs}, gen, nil, ApplyConfig{NFONameCfg: nfo.NFONameConfig{FilenameTemplate: "movie.nfo"}}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "artifact-provenance", fs, nil, nil, nil), nil, nil)
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeInPlace, GenerateNFO: artifact != "poster", Download: artifact != "nfo", OverwriteExistingMedia: true}
				cmd.PublicationFence = independentArtifactFence{&movie}
				result, e := orch.Execute(context.Background(), cmd)
				require.NoError(t, e)
				t.Logf("APPLY %+v", result.OrganizeResult)
				var id uint
				fmt.Sscanf(result.OperationID, "%d", &id)
				row, e := repo.FindByID(context.Background(), id)
				require.NoError(t, e)
				t.Logf("LEDGER %+v", row)
				undo, e := history.NewReverter(fs, repo).RevertBatch(context.Background(), "artifact-provenance")
				require.NoError(t, e)
				t.Logf("UNDO %+v", undo)
				for name, want := range map[string]string{"movie.nfo": oldNFO, "poster.jpg": "old poster"} {
					got, e := afero.ReadFile(fs, filepath.Join(dir, name))
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
			})
		}
	}
}
