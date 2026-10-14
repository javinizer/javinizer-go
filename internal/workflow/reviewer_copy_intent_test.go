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
	"reflect"
	"testing"
)

type independentCopyFence struct{ movie *models.Movie }

func (f independentCopyFence) WithApplyPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}
func (f independentCopyFence) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}
func TestIndependentCopyIntentBoundary(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, move := range []bool{false} {
			for _, relocate := range []bool{false, true} {
				for _, kind := range []string{"hardlink"} {
					for _, secondary := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/move=%v/relocate=%v/%s/secondary=%v", mode, move, relocate, kind, secondary), func(t *testing.T) {
							fs := afero.NewOsFs()
							root := t.TempDir()
							library := filepath.Join(root, "library")
							name := "ABC-123"
							if relocate {
								name = "incoming"
							}
							dir := filepath.Join(library, name)
							require.NoError(t, fs.MkdirAll(dir, 0755))
							base := "ABC-123"
							if kind != "exact" {
								base = "abc-123-source"
							}
							source := filepath.Join(dir, base+".mp4")
							target := filepath.Join(dir, "ABC-123.mp4")
							require.NoError(t, afero.WriteFile(fs, source, []byte("primary"), 0644))
							sources := []string{source}
							if kind == "hardlink" {
								require.NoError(t, os.Link(source, target))
								sources = append(sources, target)
							}
							if secondary {
								for _, suffix := range []string{"-cd2.mp4", ".en.srt"} {
									p := filepath.Join(dir, base+suffix)
									require.NoError(t, afero.WriteFile(fs, p, []byte(suffix), 0644))
									sources = append(sources, p)
								}
							}
							before := map[string]os.FileInfo{}
							for _, p := range sources {
								before[p], _ = fs.Stat(p)
							}
							db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "ledger.sqlite"), LogLevel: "error"})
							require.NoError(t, err)
							defer db.Close()
							require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
							repo := database.NewBatchFileOperationRepository(db)
							movie := models.Movie{ID: "ABC-123", ContentID: "boundary"}
							engine := template.NewEngine()
							mt, err := matcher.NewMatcher(&matcher.Config{})
							require.NoError(t, err)
							org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, engine, mt)
							orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "boundary", fs, nil, nil, nil), nil, nil)
							cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: move}, OperationMode: mode}
							if f := reflect.ValueOf(&cmd).Elem().FieldByName("PublicationFence"); f.IsValid() {
								f.Set(reflect.ValueOf(independentCopyFence{&movie}))
							}
							result, applyErr := orch.Execute(context.Background(), cmd)
							t.Logf("APPLY err=%v result=%+v", applyErr, result)
							if result != nil {
								t.Logf("ORGANIZE %+v", result.OrganizeResult)
							}
							undo, e := history.NewReverter(fs, repo).RevertBatch(context.Background(), "boundary")
							t.Logf("UNDO err=%v result=%+v", e, undo)
							if undo != nil {
								for _, f := range undo.Outcomes {
									t.Logf("UNDOFILE %+v", f)
								}
							}
							for _, p := range sources {
								now, e := fs.Stat(p)
								same := e == nil && os.SameFile(before[p], now)
								t.Logf("IDENTITY %s exists=%v same=%v", filepath.Base(p), e == nil, same)
							}
							filepath.Walk(library, func(p string, info os.FileInfo, e error) error {
								if e == nil && !info.IsDir() {
									rel, _ := filepath.Rel(library, p)
									b, _ := os.ReadFile(p)
									t.Logf("FINAL %s body=%q", rel, b)
								}
								return nil
							})
							require.NoError(t, applyErr)
							require.NoError(t, e)
							require.Equal(t, 1, undo.Succeeded)
							require.Zero(t, undo.Failed)
							for _, p := range sources {
								now, e := fs.Stat(p)
								require.NoError(t, e)
								require.True(t, os.SameFile(before[p], now))
							}
						})
					}
				}
			}
		}
	}
}
