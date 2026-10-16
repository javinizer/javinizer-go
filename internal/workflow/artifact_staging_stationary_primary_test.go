package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestStationaryPrimaryMoveUndo(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeOrganize, operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, secondary := range []bool{false, true} {
			for _, terminal := range []string{"complete", "failed", "checkpoint only"} {
				for _, mutation := range []string{"none", "missing", "foreign", "symlink"} {
					t.Run(fmt.Sprintf("%s/secondary=%v/%s/%s", mode, secondary, terminal, mutation), func(t *testing.T) {
						fs := afero.NewOsFs()
						root := t.TempDir()
						library := filepath.Join(root, "library")
						dir := filepath.Join(library, "ABC-123")
						source := filepath.Join(dir, "ABC-123.mp4")
						require.NoError(t, fs.MkdirAll(dir, 0755))
						require.NoError(t, afero.WriteFile(fs, source, []byte("primary"), 0644))
						before, err := fs.Stat(source)
						require.NoError(t, err)
						sub := filepath.Join(dir, "ABC-123.en.srt")
						part := filepath.Join(dir, "ABC-123-cd2.mp4")
						if secondary {
							require.NoError(t, afero.WriteFile(fs, sub, []byte("subtitle"), 0644))
							require.NoError(t, afero.WriteFile(fs, part, []byte("part"), 0644))
						}
						db, path := pr260ArtifactDB(t)
						repo := database.NewBatchFileOperationRepository(db)
						movie := models.Movie{ID: "ABC-123", ContentID: "stationary"}
						engine := template.NewEngine()
						mt, err := matcher.NewMatcher(&matcher.Config{})
						require.NoError(t, err)
						org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, engine, mt)
						orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "stationary", fs, nil, nil, nil), nil, nil)
						cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: true}, OperationMode: mode, PublicationFence: postPublishFence{movie: &movie}}
						var terminalResults []*ApplyResult
						for attempt := 0; attempt < 2; attempt++ {
							if terminal == "checkpoint only" {
								orch.revertLog = &residentCheckpointOnlyLog{RevertLog: NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "stationary", fs, nil, nil, nil)}
							}
							result, err := orch.Execute(context.Background(), cmd)
							require.NoError(t, err)
							require.True(t, result.OrganizeResult.PrimaryResident)
							require.False(t, result.OrganizeResult.Moved)
							if terminal == "failed" {
								require.NoError(t, orch.revertLog.CompleteFailed(context.Background(), result.OperationID, result))
							}
							ledger := p3Ledger(t, repo, result.OperationID)
							if terminal == "checkpoint only" {
								require.True(t, ledger.PublicationPending)
								captured := orch.revertLog.(*residentCheckpointOnlyLog).terminal
								require.NotNil(t, captured, "capture the successful terminal completion that the fixture withheld")
								require.True(t, captured.PublicationFinalized)
								require.Equal(t, result.OperationID, captured.OperationID)
								require.Empty(t, captured.FailedStep)
								terminalResults = append(terminalResults, captured)
							} else {
								require.False(t, ledger.PublicationPending)
							}
							require.True(t, ledger.PrimaryResident)
							require.Empty(t, ledger.MoveBack)
							require.Empty(t, ledger.Delete)
							require.Empty(t, ledger.PlannedDeletes)
							require.Empty(t, ledger.Replacements)
							if secondary {
								body, e := afero.ReadFile(fs, sub)
								require.NoError(t, e)
								require.Equal(t, "subtitle", string(body))
								_, e = fs.Stat(filepath.Join(dir, "ABC-123.eng.srt"))
								require.True(t, os.IsNotExist(e))
								body, e = afero.ReadFile(fs, part)
								require.NoError(t, e)
								require.Equal(t, "part", string(body))
							}
						}
						switch mutation {
						case "missing":
							require.NoError(t, fs.Remove(source))
						case "foreign":
							require.NoError(t, fs.Rename(source, source+".saved"))
							require.NoError(t, afero.WriteFile(fs, source, []byte("foreign"), 0644))
						case "symlink":
							require.NoError(t, fs.Rename(source, source+".saved"))
							if err := os.Symlink(source+".saved", source); err != nil {
								t.Skipf("symlink unavailable: %v", err)
							}
						}
						// Recovery reads durable SQLite state through a fresh connection.
						recovered, e := database.New(&database.Config{Type: "sqlite", DSN: path, LogLevel: "error"})
						require.NoError(t, e)
						defer recovered.Close()
						recoveredRepo := database.NewBatchFileOperationRepository(recovered)
						if terminal == "checkpoint only" {
							untouched := &publicationNoMutationFS{Fs: fs}
							heldReverter := history.NewReverter(untouched, recoveredRepo)
							for retry := 0; retry < 2; retry++ {
								held, err := heldReverter.RevertBatch(context.Background(), "stationary")
								require.NoError(t, err)
								require.Equal(t, 2, held.Failed)
								require.Zero(t, held.Succeeded)
								require.Zero(t, held.Skipped)
								require.Zero(t, untouched.writes, "checkpoint-only Undo must not touch any endpoint")
								for _, outcome := range held.Outcomes {
									require.Contains(t, outcome.Error, "publication reconciliation is pending")
								}
								rows, err := recoveredRepo.FindByBatchJobID(context.Background(), "stationary")
								require.NoError(t, err)
								require.Len(t, rows, 2)
								for _, row := range rows {
									require.Equal(t, models.RevertStatusApplied, row.RevertStatus, "refusal preserves the held status")
									ledger, err := models.ParseGeneratedFiles(row.GeneratedFiles)
									require.NoError(t, err)
									require.True(t, ledger.PublicationPending)
								}
							}
							// Retry the original successful terminal writes that the fixture
							// withheld. Do not infer ownership from the mutated endpoints or
							// clear the hold directly; Complete commits it with its journal.
							log := NewDBRevertLog(recoveredRepo, NewRevertLogConfig(true, nil), "stationary", fs, nil, nil, nil)
							for _, captured := range terminalResults {
								require.NoError(t, log.Complete(context.Background(), captured.OperationID, captured))
								require.False(t, p3Ledger(t, recoveredRepo, captured.OperationID).PublicationPending)
							}
						}
						reverter := history.NewReverter(fs, recoveredRepo)
						undo, e := reverter.RevertBatch(context.Background(), "stationary")
						require.NoError(t, e)
						require.Zero(t, undo.Failed, "%+v", undo.Outcomes)
						require.Equal(t, 2, undo.Succeeded)
						_, e = reverter.RevertBatch(context.Background(), "stationary")
						require.ErrorContains(t, e, "batch already reverted")
						switch mutation {
						case "missing":
							_, e = fs.Stat(source)
							require.True(t, os.IsNotExist(e))
						case "foreign":
							body, e := afero.ReadFile(fs, source)
							require.NoError(t, e)
							require.Equal(t, "foreign", string(body))
						case "symlink":
							info, e := os.Lstat(source)
							require.NoError(t, e)
							require.NotZero(t, info.Mode()&os.ModeSymlink)
						default:
							info, e := fs.Stat(source)
							require.NoError(t, e)
							require.True(t, os.SameFile(before, info))
						}
						if secondary {
							body, e := afero.ReadFile(fs, sub)
							require.NoError(t, e)
							require.Equal(t, "subtitle", string(body))
							body, e = afero.ReadFile(fs, part)
							require.NoError(t, e)
							require.Equal(t, "part", string(body))
						}
					})
				}
			}
		}
	}
}

func TestStationaryPrimaryOwnershipControls(t *testing.T) {
	for _, kind := range []string{"fresh move", "directory move", "legacy ledger", "late foreign", "late missing", "late symlink", "checkpoint failure", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "stationary-control")
			mode := operationmode.OperationModeOrganize
			if kind == "directory move" {
				require.NoError(t, env.fs.Remove(env.multipart))
				require.NoError(t, env.fs.Remove(env.unrelated))
				mode = operationmode.OperationModeInPlace
			}
			mt, err := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, err)
			org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, template.NewEngine(), mt)
			cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), mode, false, true, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			if kind != "fresh move" {
				// Exact filename; only the directory-move control retains an incoming folder.
				dir := filepath.Join(env.dest, env.movie.ID)
				if kind == "directory move" {
					dir = filepath.Dir(env.source)
				}
				source := filepath.Join(dir, env.movie.ID+".mp4")
				require.NoError(t, env.fs.MkdirAll(dir, 0755))
				require.NoError(t, env.fs.Rename(env.source, source))
				env.source = source
				cmd.Match.Path = source
				cmd.Match.Name = filepath.Base(source)
			}
			env.orch.organizer = org
			if kind == "late foreign" || kind == "late missing" || kind == "late symlink" {
				env.orch.organizer = &residentAliasLateExecutor{Organizer: org, before: func() {
					require.NoError(t, env.fs.Rename(env.source, env.source+".saved"))
					if kind == "late foreign" {
						require.NoError(t, afero.WriteFile(env.fs, env.source, []byte("foreign"), 0644))
					}
					if kind == "late symlink" {
						if e := os.Symlink(env.source+".saved", env.source); e != nil {
							t.Skipf("symlink unavailable: %v", e)
						}
					}
				}}
			}
			ctx := context.Background()
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if kind == "checkpoint failure" {
				env.orch.revertLog = &completeCallFaultLog{RevertLog: env.orch.revertLog, failAt: 1}
			}
			before, err := env.fs.Stat(env.source)
			require.NoError(t, err)
			// Windows path-derived FileInfo loads its file ID lazily in SameFile.
			// Pin the admitted object now, before the late-execute hook vacates or
			// replaces its pathname; comparing that stale FileInfo would reopen
			// the successor instead of identifying the preserved original.
			beforeDev, beforeIno, beforeKnown := fsutil.BoundObjectIdentity(env.fs, env.source, before)
			require.True(t, beforeKnown, "real filesystem fixture requires eager native identity")
			beforeBody, err := afero.ReadFile(env.fs, env.source)
			require.NoError(t, err)
			result, err := env.orch.Execute(ctx, cmd)
			if kind == "cancelled" || kind == "checkpoint failure" {
				require.Error(t, err)
				info, e := env.fs.Stat(env.source)
				require.NoError(t, e)
				require.True(t, os.SameFile(before, info))
				return
			}
			if kind == "late foreign" || kind == "late missing" || kind == "late symlink" {
				require.Error(t, err)
				require.NotNil(t, result)
				saved := env.source + ".saved"
				info, e := env.fs.Stat(saved)
				require.NoError(t, e)
				afterDev, afterIno, afterKnown := fsutil.BoundObjectIdentity(env.fs, saved, info)
				require.True(t, afterKnown, "preserved original requires eager native identity")
				require.Equal(t, beforeDev, afterDev)
				require.Equal(t, beforeIno, afterIno)
				savedBody, e := afero.ReadFile(env.fs, saved)
				require.NoError(t, e)
				require.Equal(t, beforeBody, savedBody)
				ledger := p3Ledger(t, env.repo, result.OperationID)
				require.Empty(t, ledger.MoveBack)
				require.Empty(t, ledger.Delete)
				require.Empty(t, ledger.Replacements)
				if kind == "late foreign" {
					body, e := afero.ReadFile(env.fs, env.source)
					require.NoError(t, e)
					require.Equal(t, "foreign", string(body))
				}
				if kind == "late missing" {
					_, e := env.fs.Stat(env.source)
					require.True(t, os.IsNotExist(e))
				}
				if kind == "late symlink" {
					info, e := os.Lstat(env.source)
					require.NoError(t, e)
					require.NotZero(t, info.Mode()&os.ModeSymlink)
				}
				return
			}
			require.NoError(t, err)
			if kind == "legacy ledger" {
				// No automatic migration can infer ownership for a pre-marker row.
				require.NoError(t, env.db.Model(&models.BatchFileOperation{}).Where("id = ?", result.OperationID).Updates(map[string]interface{}{"generated_files": "", "new_path": env.source}).Error)
			} else {
				require.False(t, result.OrganizeResult.PrimaryResident)
				require.False(t, p3Ledger(t, env.repo, result.OperationID).PrimaryResident)
			}
			undo, e := history.NewReverter(env.fs, env.repo).RevertBatch(context.Background(), "stationary-control")
			require.NoError(t, e)
			if kind == "legacy ledger" {
				require.Equal(t, 1, undo.Failed)
			} else {
				require.Zero(t, undo.Failed, "%+v", undo.Outcomes)
				require.Equal(t, 1, undo.Succeeded)
			}
			info, e := env.fs.Stat(env.source)
			require.NoError(t, e)
			require.True(t, os.SameFile(before, info))
		})
	}
}
