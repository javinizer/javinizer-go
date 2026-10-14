package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestInPlaceResidentAdmissionAndCheckpointFaults(t *testing.T) {
	for _, fault := range []string{"admission-probe", "weak-directory", "checkpoint", "late-target"} {
		t.Run(fault, func(t *testing.T) {
			stage, orch, state, old, target := directoryMoveFixture(t)
			alias := filepath.Join(old, "IPX-123.mkv")
			require.NoError(t, os.Link(stage.sourcePath, alias))
			base := stage.fs
			switch fault {
			case "admission-probe":
				stage.fs = &directoryFaultFS{Fs: base, probe: func(path string) error {
					if path == alias {
						return os.ErrPermission
					}
					return nil
				}}
			case "weak-directory":
				stage.sourceDirIdentity.hasDevIno = false
			case "checkpoint":
				orch.revertLog = &directoryFaultLog{completeFailAt: 1, failure: errors.New("durable checkpoint unavailable")}
			case "late-target":
				wrapped := &directoryFaultFS{Fs: base, afterRename: func(_, dst string) error {
					if dst == target {
						p := filepath.Join(target, filepath.Base(alias))
						if err := base.Remove(p); err != nil {
							return err
						}
						return afero.WriteFile(base, p, []byte("foreign alias"), 0644)
					}
					return nil
				}}
				stage.fs, orch.fs = wrapped, wrapped
			}
			if fault == "late-target" {
				require.ErrorContains(t, stage.publish(t.Context(), orch, state, nil), "resident primary changed")
			} else {
				require.Error(t, stage.relocateInPlaceDirectory(t.Context(), orch, "op", state))
			}
		})
	}
}

func TestResidentDirectoryLedgerMergeOwnership(t *testing.T) {
	prior := models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: &models.DirectoryMove{Original: "/old", Locations: []string{"/scratch", "/new"}}, Replacements: []models.ReplacementEntry{{Destination: "/new/movie.nfo"}}}
	next := models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: &models.DirectoryMove{Original: "/old", Locations: []string{"/new"}}, Delete: []string{"/new/movie.nfo", "/new/created.jpg"}, PlannedDeletes: []models.DeleteEntry{{Path: "/new/movie.nfo"}, {Path: "/new/created.png"}}}
	got, err := models.ParseGeneratedFiles(mergeReplacementLedgerIntents(models.MarshalLedgerJSON(prior), models.MarshalLedgerJSON(next), true))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/scratch", "/new"}, got.DirectoryMove.Locations)
	require.Equal(t, []string{"/new/created.jpg"}, got.Delete)
	require.Equal(t, []models.DeleteEntry{{Path: "/new/created.png"}}, got.PlannedDeletes)
	// Empty completion still preserves the directory marker and earlier inverse.
	got, err = models.ParseGeneratedFiles(mergeReplacementLedgerIntents(models.MarshalLedgerJSON(got), "", true))
	require.NoError(t, err)
	require.NotNil(t, got.DirectoryMove)
}

func TestInPlaceResidentForeignTargetsRemainUnowned(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, kind := range []string{"same-bytes", "symlink"} {
			t.Run(string(mode)+"/"+kind, func(t *testing.T) {
				env := setupCopyIntentE2E(t, "resident-foreign")
				incoming := filepath.Dir(env.source)
				target := filepath.Join(incoming, env.movie.ID+".mp4")
				if kind == "same-bytes" {
					body, err := afero.ReadFile(env.fs, env.source)
					require.NoError(t, err)
					require.NoError(t, afero.WriteFile(env.fs, target, body, 0644))
				} else {
					if err := os.Symlink(env.source, target); err != nil {
						t.Skipf("symlink fixture unsupported: %v", err)
					}
				}
				before, err := env.fs.Stat(env.source)
				require.NoError(t, err)
				env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, template.NewEngine(), nil)
				cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), mode, false, true, organizer.LinkModeNone, false, false)
				cmd.Organize.ForceUpdate = false
				_, err = env.orch.Execute(t.Context(), cmd)
				require.Error(t, err)
				after, err := env.fs.Stat(env.source)
				require.NoError(t, err)
				require.True(t, os.SameFile(before, after))
				info, _, err := env.fs.(afero.Lstater).LstatIfPossible(target)
				require.NoError(t, err)
				if kind == "symlink" {
					require.NotZero(t, info.Mode()&os.ModeSymlink)
				} else {
					require.False(t, os.SameFile(before, info))
				}
			})
		}
	}
}

func TestResidentDirectoryLedgerRetainsCreatedReplacementOwnership(t *testing.T) {
	for _, planned := range []bool{false, true} {
		t.Run(fmt.Sprint(planned), func(t *testing.T) {
			prior := models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: &models.DirectoryMove{Original: "/old", Locations: []string{"/new"}}, Replacements: []models.ReplacementEntry{{Destination: "/new/created.jpg"}}}
			if planned {
				prior.PlannedDeletes = []models.DeleteEntry{{Path: "/new/created.jpg", SHA256: "created-proof"}}
			} else {
				prior.Delete = []string{"/new/created.jpg"}
			}
			next := models.GeneratedFilesJSON{Delete: []string{"/new/created.jpg"}}
			got, err := models.ParseGeneratedFiles(mergeReplacementLedgerIntents(models.MarshalLedgerJSON(prior), models.MarshalLedgerJSON(next), true))
			require.NoError(t, err)
			require.Equal(t, []string{"/new/created.jpg"}, got.Delete)
		})
	}
}

func TestResidentDirectoryCreatedReplacementCaseOwnership(t *testing.T) {
	previous := fsutil.CaseSensitiveProbe
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return false, nil }
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() { fsutil.CaseSensitiveProbe = previous; fsutil.ResetCaseSensitivityCache() })
	dir := t.TempDir()
	upper, lower := filepath.Join(dir, "Movie.nfo"), filepath.Join(dir, "movie.nfo")
	prior := models.GeneratedFilesJSON{PrimaryResident: true, DirectoryMove: &models.DirectoryMove{Original: dir, Locations: []string{dir}}, Replacements: []models.ReplacementEntry{{Destination: lower}}, PlannedDeletes: []models.DeleteEntry{{Path: upper, SHA256: "created-proof"}}}
	fresh := models.GeneratedFilesJSON{Delete: []string{lower}}
	got, err := models.ParseGeneratedFiles(mergeReplacementLedgerIntents(models.MarshalLedgerJSON(prior), models.MarshalLedgerJSON(fresh), true))
	require.NoError(t, err)
	require.Equal(t, []string{lower}, got.Delete)
}
