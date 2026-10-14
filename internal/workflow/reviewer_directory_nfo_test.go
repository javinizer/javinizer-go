package workflow

import (
	"context"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/database"
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

type independentNFOFence struct{ movie *models.Movie }

func (f independentNFOFence) WithApplyPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}
func (f independentNFOFence) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}

// Crosses durable DB close/reopen, so recovery cannot rely on the live stage.
// The primary's names are user owned; only the directory and separately moved
// companions can be undone. No expectations require planned-only seats to run.
func TestIndependentResidentDirectoryNFO(t *testing.T) { independentResidentDirectoryNFO(t, false) }
func independentResidentDirectoryNFO(t *testing.T, nativeCase bool) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace} {
		for _, relocate := range []bool{true} {
			for _, failedTerminal := range []bool{false} {
				for _, mutation := range []string{"none"} {
					t.Run(fmt.Sprintf("%s/relocate=%v/failed=%v/%s", mode, relocate, failedTerminal, mutation), func(t *testing.T) {
						if nativeCase && mutation != "none" && mutation != "replace-directory" && mutation != "occupy-original-directory" {
							t.Skip("case aliases are one directory entry; hardlink mutation expectations do not apply")
						}
						physicalMove := mode == operationmode.OperationModeInPlace && relocate
						if !physicalMove && (mutation == "replace-directory" || mutation == "occupy-original-directory") {
							t.Skip("directory-only boundary")
						}
						fs := afero.NewOsFs()
						root := t.TempDir()
						library := filepath.Join(root, "library")
						name := "ABC-123"
						if relocate {
							name = "incoming"
						}
						dir := filepath.Join(library, name)
						require.NoError(t, fs.MkdirAll(dir, 0755))
						oldNFO := []byte("<movie><title>before</title></movie>")
						require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "movie.nfo"), oldNFO, 0644))
						sourceBase := "abc-123-source"
						if nativeCase {
							sourceBase = "abc-123"
						}
						source := filepath.Join(dir, sourceBase+".mp4")
						target := filepath.Join(dir, "ABC-123.mp4")
						require.NoError(t, afero.WriteFile(fs, source, []byte("resident video"), 0644))
						if nativeCase {
							a, e := fs.Stat(source)
							require.NoError(t, e)
							b, e := fs.Stat(target)
							if e != nil || !os.SameFile(a, b) {
								t.Skip("fixture volume is case-sensitive")
							}
						} else {
							require.NoError(t, os.Link(source, target))
						}
						primaryBefore, err := fs.Stat(source)
						require.NoError(t, err)
						directoryBefore, err := fs.Stat(dir)
						require.NoError(t, err)
						companions := map[string]string{sourceBase + "-cd2.mp4": "part two", sourceBase + ".en.srt": "subtitle"}
						companionBefore := map[string]os.FileInfo{}
						for name, body := range companions {
							p := filepath.Join(dir, name)
							require.NoError(t, afero.WriteFile(fs, p, []byte(body), 0644))
							companionBefore[name], err = fs.Stat(p)
							require.NoError(t, err)
						}
						dsn := filepath.Join(root, "ledger.sqlite")
						openDB := func() *database.DB {
							db, e := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
							require.NoError(t, e)
							require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
							return db
						}
						db := openDB()
						repo := database.NewBatchFileOperationRepository(db)
						movie := models.Movie{ID: "ABC-123", ContentID: "independent-inplace"}
						engine := template.NewEngine()
						mt, err := matcher.NewMatcher(&matcher.Config{})
						require.NoError(t, err)
						org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, engine, mt)
						log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "independent-inplace", fs, nil, nil, nil)
						gen := nfo.NewGenerator(fs, &nfo.Config{FilenameTemplate: "movie.nfo", TemplateEngine: engine})
						orch := newApplyOrchestrator(fs, org, nil, gen, nil, ApplyConfig{NFONameCfg: nfo.NFONameConfig{FilenameTemplate: "movie.nfo"}}, engine, log, nil, nil)
						cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: mode, PublicationFence: independentNFOFence{&movie}}
						cmd.GenerateNFO = true
						cmd.OverwriteExistingMedia = true
						result, err := orch.Execute(context.Background(), cmd)
						require.NoError(t, err)
						require.NotNil(t, result.OrganizeResult)
						require.Equal(t, physicalMove, result.OrganizeResult.InPlaceRenamed)
						publishedDir := dir
						if physicalMove {
							publishedDir = filepath.Join(library, "ABC-123")
						}
						di, err := fs.Stat(publishedDir)
						require.NoError(t, err)
						require.True(t, os.SameFile(directoryBefore, di), "directory must physically relocate, not be recreated")
						for _, name := range []string{sourceBase + ".mp4", "ABC-123.mp4"} {
							now, e := fs.Stat(filepath.Join(publishedDir, name))
							require.NoError(t, e)
							require.True(t, os.SameFile(primaryBefore, now))
						}
						// The alias lane has a real renaming plan, so normal companions should run.
						require.FileExists(t, filepath.Join(publishedDir, "ABC-123-cd2.mp4"))
						require.FileExists(t, filepath.Join(publishedDir, "ABC-123.eng.srt"))
						if failedTerminal {
							require.NoError(t, log.CompleteFailed(context.Background(), result.OperationID, result))
						}
						deleted := map[string]bool{}
						foreign := map[string]os.FileInfo{}
						for _, name := range []string{sourceBase + ".mp4", "ABC-123.mp4"} {
							which := "source"
							if name == "ABC-123.mp4" {
								which = "target"
							}
							p := filepath.Join(publishedDir, name)
							if mutation == "delete-"+which || mutation == "delete-both" {
								require.NoError(t, fs.Remove(p))
								deleted[name] = true
							}
							if mutation == "replace-"+which {
								require.NoError(t, fs.Remove(p))
								require.NoError(t, afero.WriteFile(fs, p, []byte("foreign "+which), 0644))
								foreign[name], err = fs.Stat(p)
								require.NoError(t, err)
							}
						}
						savedDir := publishedDir + ".saved"
						if mutation == "replace-directory" {
							require.NoError(t, fs.Rename(publishedDir, savedDir))
							require.NoError(t, fs.MkdirAll(publishedDir, 0755))
							require.NoError(t, afero.WriteFile(fs, filepath.Join(publishedDir, "foreign.txt"), []byte("foreign directory"), 0644))
						}
						if mutation == "occupy-original-directory" {
							require.NoError(t, fs.MkdirAll(dir, 0755))
							require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "foreign.txt"), []byte("old directory occupant"), 0644))
						}
						require.NoError(t, db.Close())
						db = openDB()
						defer db.Close()
						repo = database.NewBatchFileOperationRepository(db)
						undo, err := history.NewReverter(fs, repo).RevertBatch(context.Background(), "independent-inplace")
						require.NoError(t, err)
						require.NotNil(t, undo)
						if mutation == "replace-directory" || mutation == "occupy-original-directory" {
							require.Zero(t, undo.Succeeded)
							require.Greater(t, undo.Failed+undo.Skipped, 0)
							if mutation == "replace-directory" {
								b, e := afero.ReadFile(fs, filepath.Join(publishedDir, "foreign.txt"))
								require.NoError(t, e)
								require.Equal(t, "foreign directory", string(b))
								saved, e := fs.Stat(savedDir)
								require.NoError(t, e)
								require.True(t, os.SameFile(directoryBefore, saved))
							} else {
								b, e := afero.ReadFile(fs, filepath.Join(dir, "foreign.txt"))
								require.NoError(t, e)
								require.Equal(t, "old directory occupant", string(b))
								saved, e := fs.Stat(publishedDir)
								require.NoError(t, e)
								require.True(t, os.SameFile(directoryBefore, saved))
							}
							return
						}
						require.Equal(t, 1, undo.Succeeded)
						require.Zero(t, undo.Failed)
						di, err = fs.Stat(dir)
						require.NoError(t, err)
						require.True(t, os.SameFile(directoryBefore, di))
						for _, name := range []string{sourceBase + ".mp4", "ABC-123.mp4"} {
							p := filepath.Join(dir, name)
							now, e := fs.Stat(p)
							if deleted[name] {
								require.True(t, os.IsNotExist(e), "Undo resurrected deleted alias %s", name)
								continue
							}
							require.NoError(t, e)
							want := primaryBefore
							if f, ok := foreign[name]; ok {
								want = f
							}
							require.True(t, os.SameFile(want, now), "Undo touched user-owned primary %s", name)
						}
						for name, body := range companions {
							p := filepath.Join(dir, name)
							b, e := afero.ReadFile(fs, p)
							require.NoError(t, e)
							require.Equal(t, body, string(b))
							now, e := fs.Stat(p)
							require.NoError(t, e)
							require.True(t, os.SameFile(companionBefore[name], now))
						}
						b, e := afero.ReadFile(fs, filepath.Join(dir, "movie.nfo"))
						require.NoError(t, e)
						require.Equal(t, oldNFO, b)
						if physicalMove {
							_, e = fs.Stat(publishedDir)
							require.True(t, os.IsNotExist(e), "Undo recreated moved directory for NFO")
						}
						_, err = history.NewReverter(fs, repo).RevertBatch(context.Background(), "independent-inplace")
						require.ErrorIs(t, err, history.ErrBatchAlreadyReverted)
					})
				}
			}
		}
	}
}
