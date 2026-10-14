package workflow

import (
	"errors"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recoveryDenyRollbackFS struct {
	afero.Fs
	armed       bool
	dir         string
	prefix      string
	removeFault bool
	removals    int
}

func (f *recoveryDenyRollbackFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(p)
}
func (f *recoveryDenyRollbackFS) OpenFile(p string, flags int, perm os.FileMode) (afero.File, error) {
	if f.armed && !f.removeFault && flags&os.O_CREATE != 0 && ((f.prefix == "" && filepath.Dir(p) == f.dir) || (f.prefix != "" && strings.HasPrefix(p, f.prefix))) {
		return nil, os.ErrPermission
	}
	return f.Fs.OpenFile(p, flags, perm)
}
func (f *recoveryDenyRollbackFS) Remove(p string) error {
	if f.armed && f.removeFault && strings.Contains(p, ".vac.") {
		if info, err := f.Fs.Stat(p); err == nil && info.Size() > 0 {
			f.removals++
			return os.ErrPermission
		}
	}
	return f.Fs.Remove(p)
}
func TestResidentPublicationArtifactFailureUndo(t *testing.T) {
	for _, kind := range []string{"subtitle", "nfo", "poster"} {
		for _, move := range []bool{false, true} {
			for _, scenario := range []string{"owned refusal", "foreign successor", "successful rollback"} {
				foreign := scenario == "foreign successor"
				blocked := scenario == "owned refusal"
				t.Run(fmt.Sprint(kind, "/move=", move, "/", scenario), func(t *testing.T) {
					env := setupCopyIntentE2E(t, "review-artifact-failure")
					require.NoError(t, env.fs.Remove(env.multipart))
					if kind != "subtitle" {
						require.NoError(t, env.fs.Remove(env.subtitle))
					}
					targetDir := filepath.Join(env.dest, env.movie.ID)
					require.NoError(t, env.fs.MkdirAll(targetDir, 0755))
					target := filepath.Join(targetDir, env.movie.ID+".mp4")
					require.NoError(t, os.Link(env.source, target))
					fs := &recoveryDenyRollbackFS{Fs: env.fs, dir: targetDir}
					bytes := pr260PosterBytes(t)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "image/jpeg")
						_, _ = w.Write(bytes)
					}))
					defer server.Close()
					env.movie.Poster.PosterURL = server.URL + "/poster.jpg"
					orch := pr260RealApply(fs, &env.movie, organizer.MediaFormatConfig{PosterFormat: "poster.jpg"}, server.Client(), true)
					orch.revertLog = env.orch.revertLog
					orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
					output := ""
					foreignBody := []byte("foreign bytes")
					fence := postPublishFence{movie: &env.movie, err: errors.New("fence failure"), after: func() {
						entries, e := afero.ReadDir(env.fs, targetDir)
						require.NoError(t, e)
						for _, entry := range entries {
							ext := filepath.Ext(entry.Name())
							if (kind == "subtitle" && ext == ".srt") || (kind == "nfo" && ext == ".nfo") || (kind == "poster" && ext == ".jpg") {
								output = filepath.Join(targetDir, entry.Name())
							}
						}
						require.NotEmpty(t, output)
						if foreign {
							foreignBody, e = afero.ReadFile(env.fs, output)
							require.NoError(t, e)
							require.NoError(t, env.fs.Rename(output, output+".saved"))
							require.NoError(t, afero.WriteFile(env.fs, output, foreignBody, 0644))
						} else if blocked {
							fs.armed = true
						}
					}}
					cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, move, organizer.LinkModeNone, false, false)
					cmd.PublicationFence = fence
					cmd.GenerateNFO = kind == "nfo"
					cmd.Download = kind == "poster"
					result, err := orch.Execute(t.Context(), cmd)
					require.Error(t, err)
					require.NotNil(t, result)
					fs.armed = false
					if blocked {
						require.False(t, result.PrePublication, "owned artifact rollback failure is not a no-op")
					}
					reopenPublicationRecoveryDB(t, env)
					_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
					require.NoError(t, err)
					if foreign {
						body, e := afero.ReadFile(env.fs, output)
						require.NoError(t, e, "foreign successor must survive Undo")
						require.Equal(t, string(foreignBody), string(body))
					} else {
						_, e := env.fs.Stat(output)
						require.True(t, os.IsNotExist(e), "owned output should be compensated")
					}
					require.FileExists(t, target)
				})
			}
		}
	}
}

func TestResidentPublicationMixedOwnForeignUndo(t *testing.T) {
	for _, kind := range []string{"subtitle"} {
		for _, move := range []bool{false, true} {
			for _, foreign := range []bool{true} {
				t.Run(fmt.Sprint(kind, "/move=", move, "/foreign=", foreign), func(t *testing.T) {
					env := setupCopyIntentE2E(t, "review-artifact-failure")
					require.NoError(t, env.fs.Remove(env.multipart))
					if kind != "subtitle" {
						require.NoError(t, env.fs.Remove(env.subtitle))
					}
					targetDir := filepath.Join(env.dest, env.movie.ID)
					require.NoError(t, env.fs.MkdirAll(targetDir, 0755))
					target := filepath.Join(targetDir, env.movie.ID+".mp4")
					require.NoError(t, os.Link(env.source, target))
					fs := &recoveryDenyRollbackFS{Fs: env.fs, dir: targetDir}
					bytes := pr260PosterBytes(t)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "image/jpeg")
						_, _ = w.Write(bytes)
					}))
					defer server.Close()
					env.movie.Poster.PosterURL = server.URL + "/poster.jpg"
					orch := pr260RealApply(fs, &env.movie, organizer.MediaFormatConfig{PosterFormat: "poster.jpg"}, server.Client(), true)
					orch.revertLog = env.orch.revertLog
					orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
					output := ""
					nfoPath := ""
					foreignBody := []byte("foreign bytes")
					fence := postPublishFence{movie: &env.movie, err: errors.New("fence failure"), after: func() {
						entries, e := afero.ReadDir(env.fs, targetDir)
						require.NoError(t, e)
						for _, entry := range entries {
							ext := filepath.Ext(entry.Name())
							if ext == ".nfo" {
								nfoPath = filepath.Join(targetDir, entry.Name())
							}
							if (kind == "subtitle" && ext == ".srt") || (kind == "nfo" && ext == ".nfo") || (kind == "poster" && ext == ".jpg") {
								output = filepath.Join(targetDir, entry.Name())
							}
						}
						require.NotEmpty(t, output)
						if foreign {
							foreignBody, e = afero.ReadFile(env.fs, output)
							require.NoError(t, e)
							require.NoError(t, env.fs.Rename(output, output+".saved"))
							require.NoError(t, afero.WriteFile(env.fs, output, foreignBody, 0644))
							require.NotEmpty(t, nfoPath)
							fs.prefix = nfoPath
							fs.armed = true
						} else {
							fs.armed = true
						}
					}}
					cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, move, organizer.LinkModeNone, false, false)
					cmd.PublicationFence = fence
					cmd.GenerateNFO = true
					cmd.Download = kind == "poster"
					result, err := orch.Execute(t.Context(), cmd)
					require.Error(t, err)
					require.NotNil(t, result)
					fs.armed = false
					require.False(t, result.PrePublication, "owned NFO rollback failure must remain revertable")
					reopenPublicationRecoveryDB(t, env)
					_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
					require.NoError(t, err)
					if foreign {
						body, e := afero.ReadFile(env.fs, output)
						require.NoError(t, e, "foreign successor must survive Undo")
						require.Equal(t, string(foreignBody), string(body))
					} else {
						_, e := env.fs.Stat(output)
						require.True(t, os.IsNotExist(e), "owned output should be compensated")
					}
					require.FileExists(t, target)
					_, e := env.fs.Stat(nfoPath)
					require.True(t, os.IsNotExist(e), "own NFO must be compensated")
				})
			}
		}
	}
}
func TestResidentPublicationSwapAfterFailureBeforeUndo(t *testing.T) {
	for _, kind := range []string{"subtitle", "nfo", "poster"} {
		for _, move := range []bool{false, true} {
			for _, foreign := range []bool{false} {
				t.Run(fmt.Sprint(kind, "/move=", move, "/foreign=", foreign), func(t *testing.T) {
					env := setupCopyIntentE2E(t, "review-artifact-failure")
					require.NoError(t, env.fs.Remove(env.multipart))
					if kind != "subtitle" {
						require.NoError(t, env.fs.Remove(env.subtitle))
					}
					targetDir := filepath.Join(env.dest, env.movie.ID)
					require.NoError(t, env.fs.MkdirAll(targetDir, 0755))
					target := filepath.Join(targetDir, env.movie.ID+".mp4")
					require.NoError(t, os.Link(env.source, target))
					fs := &recoveryDenyRollbackFS{Fs: env.fs, dir: targetDir}
					bytes := pr260PosterBytes(t)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "image/jpeg")
						_, _ = w.Write(bytes)
					}))
					defer server.Close()
					env.movie.Poster.PosterURL = server.URL + "/poster.jpg"
					orch := pr260RealApply(fs, &env.movie, organizer.MediaFormatConfig{PosterFormat: "poster.jpg"}, server.Client(), true)
					orch.revertLog = env.orch.revertLog
					orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
					output := ""
					foreignBody := []byte("foreign bytes")
					fence := postPublishFence{movie: &env.movie, err: errors.New("fence failure"), after: func() {
						entries, e := afero.ReadDir(env.fs, targetDir)
						require.NoError(t, e)
						for _, entry := range entries {
							ext := filepath.Ext(entry.Name())
							if (kind == "subtitle" && ext == ".srt") || (kind == "nfo" && ext == ".nfo") || (kind == "poster" && ext == ".jpg") {
								output = filepath.Join(targetDir, entry.Name())
							}
						}
						require.NotEmpty(t, output)
						if foreign {
							foreignBody, e = afero.ReadFile(env.fs, output)
							require.NoError(t, e)
							require.NoError(t, env.fs.Rename(output, output+".saved"))
							require.NoError(t, afero.WriteFile(env.fs, output, foreignBody, 0644))
						} else {
							fs.armed = true
						}
					}}
					cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, move, organizer.LinkModeNone, false, false)
					cmd.PublicationFence = fence
					cmd.GenerateNFO = kind == "nfo"
					cmd.Download = kind == "poster"
					result, err := orch.Execute(t.Context(), cmd)
					require.Error(t, err)
					require.NotNil(t, result)
					fs.armed = false
					foreignBody, err = afero.ReadFile(env.fs, output)
					require.NoError(t, err)
					require.NoError(t, env.fs.Rename(output, output+".saved"))
					require.NoError(t, afero.WriteFile(env.fs, output, foreignBody, 0644))
					foreign = true
					if !foreign {
						require.False(t, result.PrePublication, "owned artifact rollback failure is not a no-op")
					}
					t.Logf("recovery ledger: %+v", p3Ledger(t, env.repo, result.OperationID))
					reopenPublicationRecoveryDB(t, env)
					_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
					require.NoError(t, err)
					if foreign {
						body, e := afero.ReadFile(env.fs, output)
						require.NoError(t, e, "foreign successor must survive Undo")
						require.Equal(t, string(foreignBody), string(body))
					} else {
						_, e := env.fs.Stat(output)
						require.True(t, os.IsNotExist(e), "owned output should be compensated")
					}
					require.FileExists(t, target)
				})
			}
		}
	}
}

func TestResidentPublicationActualUnlinkFailureUndo(t *testing.T) {
	for _, kind := range []string{"subtitle", "nfo", "poster"} {
		for _, move := range []bool{false} {
			for _, foreign := range []bool{false} {
				t.Run(fmt.Sprint(kind, "/move=", move, "/foreign=", foreign), func(t *testing.T) {
					env := setupCopyIntentE2E(t, "review-artifact-failure")
					require.NoError(t, env.fs.Remove(env.multipart))
					if kind != "subtitle" {
						require.NoError(t, env.fs.Remove(env.subtitle))
					}
					targetDir := filepath.Join(env.dest, env.movie.ID)
					require.NoError(t, env.fs.MkdirAll(targetDir, 0755))
					target := filepath.Join(targetDir, env.movie.ID+".mp4")
					require.NoError(t, os.Link(env.source, target))
					fs := &recoveryDenyRollbackFS{Fs: env.fs, dir: targetDir, removeFault: true}
					bytes := pr260PosterBytes(t)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "image/jpeg")
						_, _ = w.Write(bytes)
					}))
					defer server.Close()
					env.movie.Poster.PosterURL = server.URL + "/poster.jpg"
					orch := pr260RealApply(fs, &env.movie, organizer.MediaFormatConfig{PosterFormat: "poster.jpg"}, server.Client(), true)
					orch.revertLog = env.orch.revertLog
					orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
					output := ""
					foreignBody := []byte("foreign bytes")
					fence := postPublishFence{movie: &env.movie, err: errors.New("fence failure"), after: func() {
						entries, e := afero.ReadDir(env.fs, targetDir)
						require.NoError(t, e)
						for _, entry := range entries {
							ext := filepath.Ext(entry.Name())
							if (kind == "subtitle" && ext == ".srt") || (kind == "nfo" && ext == ".nfo") || (kind == "poster" && ext == ".jpg") {
								output = filepath.Join(targetDir, entry.Name())
							}
						}
						require.NotEmpty(t, output)
						if foreign {
							foreignBody, e = afero.ReadFile(env.fs, output)
							require.NoError(t, e)
							require.NoError(t, env.fs.Rename(output, output+".saved"))
							require.NoError(t, afero.WriteFile(env.fs, output, foreignBody, 0644))
						} else {
							fs.armed = true
						}
					}}
					cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, move, organizer.LinkModeNone, false, false)
					cmd.PublicationFence = fence
					cmd.GenerateNFO = kind == "nfo"
					cmd.Download = kind == "poster"
					result, err := orch.Execute(t.Context(), cmd)
					require.Error(t, err)
					require.NotNil(t, result)
					fs.armed = false
					require.Positive(t, fs.removals, "actual rollback unlink reached")
					if !foreign {
						require.False(t, result.PrePublication, "owned artifact rollback failure is not a no-op")
					}
					t.Logf("recovery ledger: %+v", p3Ledger(t, env.repo, result.OperationID))
					reopenPublicationRecoveryDB(t, env)
					_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
					require.NoError(t, err)
					if foreign {
						body, e := afero.ReadFile(env.fs, output)
						require.NoError(t, e, "foreign successor must survive Undo")
						require.Equal(t, string(foreignBody), string(body))
					} else {
						_, e := env.fs.Stat(output)
						require.True(t, os.IsNotExist(e), "owned output should be compensated")
					}
					require.FileExists(t, target)
				})
			}
		}
	}
}

func TestResidentPublicationCompensatedOutputNoop(t *testing.T) {
	for _, kind := range []string{"subtitle", "nfo", "poster"} {
		for _, move := range []bool{false, true} {
			for _, foreign := range []bool{false} {
				t.Run(fmt.Sprint(kind, "/move=", move, "/foreign=", foreign), func(t *testing.T) {
					env := setupCopyIntentE2E(t, "review-artifact-failure")
					require.NoError(t, env.fs.Remove(env.multipart))
					if kind != "subtitle" {
						require.NoError(t, env.fs.Remove(env.subtitle))
					}
					targetDir := filepath.Join(env.dest, env.movie.ID)
					require.NoError(t, env.fs.MkdirAll(targetDir, 0755))
					target := filepath.Join(targetDir, env.movie.ID+".mp4")
					require.NoError(t, os.Link(env.source, target))
					fs := &recoveryDenyRollbackFS{Fs: env.fs, dir: targetDir}
					bytes := pr260PosterBytes(t)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "image/jpeg")
						_, _ = w.Write(bytes)
					}))
					defer server.Close()
					env.movie.Poster.PosterURL = server.URL + "/poster.jpg"
					orch := pr260RealApply(fs, &env.movie, organizer.MediaFormatConfig{PosterFormat: "poster.jpg"}, server.Client(), true)
					orch.revertLog = env.orch.revertLog
					orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
					output := ""
					foreignBody := []byte("foreign bytes")
					fence := postPublishFence{movie: &env.movie, err: errors.New("fence failure"), after: func() {
						entries, e := afero.ReadDir(env.fs, targetDir)
						require.NoError(t, e)
						for _, entry := range entries {
							ext := filepath.Ext(entry.Name())
							if (kind == "subtitle" && ext == ".srt") || (kind == "nfo" && ext == ".nfo") || (kind == "poster" && ext == ".jpg") {
								output = filepath.Join(targetDir, entry.Name())
							}
						}
						require.NotEmpty(t, output)
						if foreign {
							foreignBody, e = afero.ReadFile(env.fs, output)
							require.NoError(t, e)
							require.NoError(t, env.fs.Rename(output, output+".saved"))
							require.NoError(t, afero.WriteFile(env.fs, output, foreignBody, 0644))
						} else {
							fs.armed = false
						}
					}}
					cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, move, organizer.LinkModeNone, false, false)
					cmd.PublicationFence = fence
					cmd.GenerateNFO = kind == "nfo"
					cmd.Download = kind == "poster"
					result, err := orch.Execute(t.Context(), cmd)
					require.Error(t, err)
					require.NotNil(t, result)
					fs.armed = false
					if !foreign {
						require.True(t, result.PrePublication, "successful compensation should be no-op")
					}
					t.Logf("recovery ledger: %+v", p3Ledger(t, env.repo, result.OperationID))
					reopenPublicationRecoveryDB(t, env)
					_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
					require.NoError(t, err)
					if foreign {
						body, e := afero.ReadFile(env.fs, output)
						require.NoError(t, e, "foreign successor must survive Undo")
						require.Equal(t, string(foreignBody), string(body))
					} else {
						_, e := env.fs.Stat(output)
						require.True(t, os.IsNotExist(e), "owned output should be compensated")
					}
					require.FileExists(t, target)
				})
			}
		}
	}
}

func reopenPublicationRecoveryDB(t *testing.T, env *copyIntentE2EEnv) {
	t.Helper()
	var rows []struct {
		Name string
		File string
	}
	require.NoError(t, env.db.Raw("PRAGMA database_list").Scan(&rows).Error)
	file := ""
	for _, r := range rows {
		if r.Name == "main" {
			file = r.File
		}
	}
	require.NotEmpty(t, file)
	require.NoError(t, env.db.Close())
	fresh, err := database.New(&database.Config{Type: "sqlite", DSN: file, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = fresh.Close() })
	env.db = fresh
	env.repo = database.NewBatchFileOperationRepository(fresh)
}

func TestResidentPublicationWeakProofNotReportedSuccess(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(fmt.Sprint(move), func(t *testing.T) {
			env := setupCopyIntentE2E(t, "review-weak-recovery")
			output := filepath.Join(t.TempDir(), "owned.srt")
			require.NoError(t, afero.WriteFile(env.fs, output, []byte("retain"), 0600))
			proof := models.DeleteEntry{Path: output, RecoveryUnproven: true}
			ledger := models.GeneratedFilesJSON{PrimaryResident: true}
			typ := models.OperationTypeCopy
			if move {
				typ = models.OperationTypeMove
				ledger.MoveBack = []models.FileMove{{OriginalPath: output + ".original", NewPath: output, Proof: &proof}}
			} else {
				ledger.PlannedDeletes = []models.DeleteEntry{proof}
			}
			row := &models.BatchFileOperation{BatchJobID: env.jobID, MovieID: env.movie.ID, OriginalPath: env.source, OperationType: typ, RevertStatus: models.RevertStatusFailed, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
			require.NoError(t, env.repo.Create(t.Context(), row))
			reopenPublicationRecoveryDB(t, env)
			undone, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			require.NoError(t, err)
			require.Zero(t, undone.Succeeded, "retained unsupported proof must not report successful recovery")
			require.Equal(t, 1, undone.Failed)
			saved, err := env.repo.FindByID(t.Context(), row.ID)
			require.NoError(t, err)
			require.Equal(t, models.RevertStatusFailed, saved.RevertStatus)
			require.FileExists(t, output)
		})
	}
}
func TestResidentPublicationSamePayloadLinkRetained(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(fmt.Sprint(move), func(t *testing.T) {
			env := setupCopyIntentE2E(t, "review-weak-recovery")
			output := filepath.Join(t.TempDir(), "owned.srt")
			if err := os.Symlink("same-target", output); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			// Replace the observed link with a distinct same-payload entry.
			require.NoError(t, os.Rename(output, output+".saved"))
			require.NoError(t, os.Symlink("same-target", output))
			proof := models.DeleteEntry{Path: output, RecoveryUnproven: true}
			ledger := models.GeneratedFilesJSON{PrimaryResident: true}
			typ := models.OperationTypeCopy
			if move {
				typ = models.OperationTypeMove
				ledger.MoveBack = []models.FileMove{{OriginalPath: output + ".original", NewPath: output, Proof: &proof}}
			} else {
				ledger.PlannedDeletes = []models.DeleteEntry{proof}
			}
			row := &models.BatchFileOperation{BatchJobID: env.jobID, MovieID: env.movie.ID, OriginalPath: env.source, OperationType: typ, RevertStatus: models.RevertStatusFailed, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
			require.NoError(t, env.repo.Create(t.Context(), row))
			reopenPublicationRecoveryDB(t, env)
			undone, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			require.NoError(t, err)
			require.Zero(t, undone.Succeeded, "retained unsupported proof must not report successful recovery")
			require.Equal(t, 1, undone.Failed)
			saved, err := env.repo.FindByID(t.Context(), row.ID)
			require.NoError(t, err)
			require.Equal(t, models.RevertStatusFailed, saved.RevertStatus)
			target, err := os.Readlink(output)
			require.NoError(t, err)
			require.Equal(t, "same-target", target)
		})
	}
}

func TestResidentPublicationUnarmedForeignMovedSubtitleRetained(t *testing.T) {
	env := setupCopyIntentE2E(t, "review-unarmed-foreign")
	require.NoError(t, env.fs.Remove(env.multipart))
	org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	targetDir := filepath.Join(env.dest, env.movie.ID)
	require.NoError(t, env.fs.MkdirAll(targetDir, 0755))
	primary := filepath.Join(targetDir, env.movie.ID+".mp4")
	require.NoError(t, os.Link(env.source, primary))
	output := ""
	var foreign os.FileInfo
	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		for _, sr := range result.Subtitles {
			if sr.Moved {
				output = sr.NewPath
				body, err := afero.ReadFile(env.fs, output)
				require.NoError(t, err)
				require.NoError(t, env.fs.Rename(output, output+".saved"))
				require.NoError(t, afero.WriteFile(env.fs, output, body, 0644))
				foreign, err = env.fs.Stat(output)
				require.NoError(t, err)
			}
		}
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	require.NotEmpty(t, output)
	t.Logf("error=%v ledger=%+v", err, p3Ledger(t, env.repo, result.OperationID))
	reopenPublicationRecoveryDB(t, env)
	_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	now, err := env.fs.Stat(output)
	require.NoError(t, err, "unarmed foreign successor must remain at target")
	require.True(t, os.SameFile(foreign, now))
	require.FileExists(t, primary)
}

func TestResidentPublicationMissingMoveProofRetainsRetryable(t *testing.T) {
	env := setupCopyIntentE2E(t, "missing-move-proof")
	require.NoError(t, env.fs.Remove(env.multipart))
	org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, template.NewEngine(), nil)
	dir := filepath.Join(env.dest, env.movie.ID)
	require.NoError(t, env.fs.MkdirAll(dir, 0755))
	require.NoError(t, os.Link(env.source, filepath.Join(dir, env.movie.ID+".mp4")))
	output := ""
	env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		for i, sr := range result.Subtitles {
			if sr.Moved {
				output = sr.NewPath
				result.Subtitles[i].InstalledIdentity = nil
			}
		}
	}}
	cmd := pr260FencedCommand(&env.movie, env.match, env.dest, pr260FencedCounter(t, env.db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	result, err := env.orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, output)
	ledger := p3Ledger(t, env.repo, result.OperationID)
	require.Len(t, ledger.MoveBack, 1)
	require.True(t, ledger.MoveBack[0].Proof.RecoveryUnproven)
	reopenPublicationRecoveryDB(t, env)
	undone, err := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	require.Zero(t, undone.Succeeded)
	require.Equal(t, 1, undone.Failed)
	require.FileExists(t, output)
}
