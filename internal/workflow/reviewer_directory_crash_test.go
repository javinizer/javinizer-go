package workflow

import (
	"context"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/database"
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

type independentCrashDirectoryLog struct {
	RevertLog
	n       int
	at      int
	after   bool
	tripped bool
}

func (l *independentCrashDirectoryLog) Complete(ctx context.Context, id OperationID, r *ApplyResult) error {
	directory := r != nil && r.OrganizeResult != nil && r.OrganizeResult.DirectoryMove != nil
	if directory {
		l.n++
		if l.n == l.at && !l.after {
			l.tripped = true
			panic("synthetic crash before directory checkpoint")
		}
	}
	e := l.RevertLog.Complete(ctx, id, r)
	if directory && l.n == l.at && l.after && e == nil {
		l.tripped = true
		panic("synthetic crash after directory checkpoint")
	}
	return e
}
func TestIndependentResidentDirectoryCheckpointCrash(t *testing.T) {
	for at := 1; at <= 3; at++ {
		for _, after := range []bool{false, true} {
			t.Run(fmt.Sprintf("checkpoint=%d/after=%v", at, after), func(t *testing.T) {
				fs := afero.NewOsFs()
				root := t.TempDir()
				library := filepath.Join(root, "library")
				dir := filepath.Join(library, "incoming")
				require.NoError(t, fs.MkdirAll(dir, 0755))
				source := filepath.Join(dir, "abc-123-source.mp4")
				target := filepath.Join(dir, "ABC-123.mp4")
				require.NoError(t, afero.WriteFile(fs, source, []byte("resident"), 0644))
				require.NoError(t, os.Link(source, target))
				primary, e := fs.Stat(source)
				require.NoError(t, e)
				di, e := fs.Stat(dir)
				require.NoError(t, e)
				dsn := filepath.Join(root, "db.sqlite")
				open := func() *database.DB {
					db, e := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
					require.NoError(t, e)
					require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
					return db
				}
				db := open()
				repo := database.NewBatchFileOperationRepository(db)
				log := &independentCrashDirectoryLog{RevertLog: NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "independent-crash", fs, nil, nil, nil), at: at, after: after}
				movie := models.Movie{ID: "ABC-123", ContentID: "independent-crash"}
				engine := template.NewEngine()
				mt, e := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, e)
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, engine, mt)
				orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, log, nil, nil)
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: operationmode.OperationModeInPlace, PublicationFence: inplaceReviewFence{&movie}}
				func() {
					defer func() {
						if p := recover(); p != nil {
							t.Logf("crash: %v", p)
						}
					}()
					_, e = orch.Execute(context.Background(), cmd)
				}()
				require.True(t, log.tripped)
				require.NoError(t, db.Close())
				db = open()
				defer db.Close()
				repo = database.NewBatchFileOperationRepository(db)
				result, e := history.NewReverter(fs, repo).RevertBatch(context.Background(), "independent-crash")
				require.NoError(t, e)
				t.Logf("Undo %+v", result)
				require.Zero(t, result.Failed)
				if at > 1 || after {
					require.Equal(t, 1, result.Succeeded)
				}
				now, e := fs.Stat(dir)
				require.NoError(t, e)
				require.True(t, os.SameFile(di, now))
				for _, p := range []string{source, target} {
					now, e := fs.Stat(p)
					require.NoError(t, e)
					require.True(t, os.SameFile(primary, now))
				}
			})
		}
	}
}
