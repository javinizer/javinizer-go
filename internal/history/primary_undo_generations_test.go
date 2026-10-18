package history

import (
	"context"
	"errors"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

func TestPrimaryUndoSupersededClaimCrashCutsRetainOwnership(t *testing.T) {
	for _, cut := range []string{"before-claim", "after-claim", "before-take", "after-take", "before-remove", "after-remove", "after-sync", "before-pop", "destination-after-claim"} {
		t.Run(cut, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			j := f.pause("copying")
			stop := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "primary_removed", after: true}
			require.Error(t, resumePrimaryUndo(context.Background(), f.fs, stop, f.op, j))
			require.True(t, stop.fired)
			j = f.journal()
			require.Len(t, j.SupersededStages, 1)
			var repo database.BatchFileOperationRepositoryInterface = f.repo
			fault := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "primary_removed"}
			publish, remove, syncParent := primaryUndoPublish, primaryUndoRemove, primaryUndoSyncParent
			t.Cleanup(func() { primaryUndoPublish = publish; primaryUndoRemove = remove; primaryUndoSyncParent = syncParent })
			switch cut {
			case "before-claim", "after-claim":
				fault.after = cut == "after-claim"
				repo = fault
			case "destination-after-claim":
				fault.onlyHook = true
				fault.hook = func(*models.PrimaryUndo) {
					require.NoError(t, os.Rename(j.Destination, j.Destination+".owner"))
					require.NoError(t, os.WriteFile(j.Destination, []byte("foreign"), 0600))
				}
				repo = fault
			case "before-take", "after-take":
				primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
					if cut == "before-take" {
						return errPrimaryUndoFault
					}
					if err := publish(fs, src, dst); err != nil {
						return err
					}
					return errPrimaryUndoFault
				}
			case "before-remove", "after-remove":
				primaryUndoRemove = func(fs afero.Fs, path string) error {
					if cut == "before-remove" {
						return errPrimaryUndoFault
					}
					if err := remove(fs, path); err != nil {
						return err
					}
					return errPrimaryUndoFault
				}
			case "after-sync":
				primaryUndoSyncParent = func(string) error { return errPrimaryUndoFault }
			case "before-pop":
				fault.fired = true
				repo = fault
				primaryUndoRemove = func(fs afero.Fs, path string) error { err := remove(fs, path); fault.fired = false; return err }
			}
			require.Error(t, cleanupSupersededPrimaryUndo(context.Background(), f.fs, repo, f.op, j))
			saved := f.journal()
			require.Equal(t, "primary_removed", saved.State)
			require.Len(t, saved.SupersededStages, 1)
			require.True(t, models.PrimaryUndoNeedsRetention(*f.op))
			if cut == "destination-after-claim" {
				got, err := os.ReadFile(j.Destination)
				require.NoError(t, err)
				require.Equal(t, "foreign", string(got))
				require.FileExists(t, j.Destination+".owner")
			} else {
				got, err := os.ReadFile(j.Destination)
				require.NoError(t, err)
				require.Equal(t, "synthetic primary video", string(got))
			}
			primaryUndoPublish, primaryUndoRemove, primaryUndoSyncParent = publish, remove, syncParent
			if saved.SupersededStages[0].Cleanup != "" {
				before := f.op.GeneratedFiles
				require.Error(t, f.resume())
				require.Equal(t, before, f.op.GeneratedFiles, "interrupted claim ownership must not be superseded")
			} else {
				require.NoError(t, f.resume())
				f.success()
			}
		})
	}
}

func TestPrimaryUndoGenerationValidationAndCASRefusals(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("copying")
	require.True(t, primaryUndoStagePath(j, j.Stage))
	require.True(t, primaryUndoStagePath(j, j.Stage+"."+strings.Repeat("a", 32)))
	require.False(t, primaryUndoStagePath(j, "foreign"))
	require.False(t, primaryUndoStagePath(j, j.Stage+".bad"))
	for _, change := range []func(*models.PrimaryUndoStage){
		func(s *models.PrimaryUndoStage) { s.Path = j.Stage }, func(s *models.PrimaryUndoStage) { s.Path = "foreign" }, func(s *models.PrimaryUndoStage) { s.Identity.Strong = false },
		func(s *models.PrimaryUndoStage) { s.Cleanup = "foreign" }, func(s *models.PrimaryUndoStage) { s.Cleanup = s.Path + ".undo-remove." + strings.Repeat("x", 32) },
	} {
		copy := clonePrimaryUndo(j)
		stage := models.PrimaryUndoStage{Path: j.Stage + "." + strings.Repeat("a", 32), Identity: j.StageIdentity}
		change(&stage)
		copy.SupersededStages = []models.PrimaryUndoStage{stage}
		require.Error(t, validatePrimaryUndo(f.op, &copy))
	}
	copy := clonePrimaryUndo(j)
	copy.SupersededStages = []models.PrimaryUndoStage{{Path: j.Stage + "." + strings.Repeat("a", 32), Identity: j.StageIdentity}}
	require.NoError(t, validatePrimaryUndo(f.op, &copy))
	copy.SupersededStages[0].CleanupFrom = copy.SupersededStages[0].Path
	copy.SupersededStages[0].Cleanup = copy.SupersededStages[0].Path + ".undo-remove." + strings.Repeat("b", 32)
	require.NoError(t, validatePrimaryUndo(f.op, &copy))
	before := f.op.GeneratedFiles
	copy = clonePrimaryUndo(j)
	copy.StageIdentity.Strong = false
	require.Error(t, rotatePrimaryUndoStage(context.Background(), f.repo, f.op, &copy))
	random := primaryUndoRandom
	t.Cleanup(func() { primaryUndoRandom = random })
	primaryUndoRandom = strings.NewReader("")
	require.Error(t, rotatePrimaryUndoStage(context.Background(), f.repo, f.op, j))
	primaryUndoRandom = random
	copy = clonePrimaryUndo(j)
	copy.State = "stale"
	require.Error(t, rotatePrimaryUndoStage(context.Background(), f.repo, f.op, &copy))
	require.Equal(t, before, f.op.GeneratedFiles)
	require.FileExists(t, j.Source)
	require.FileExists(t, j.Stage)
}

func TestPrimaryUndoNeverReopensSupersededStageWritable(t *testing.T) {
	installPrimaryUndoMetadataUnwrap(t)
	f := newPrimaryUndoFixture(t)
	j := f.pause("copying")
	oldStage := j.Stage
	fs := &primaryUndoFaultFS{Fs: f.fs, open: func(path string, flags int, file afero.File) (afero.File, error) {
		if path == oldStage && flags&(os.O_WRONLY|os.O_RDWR|os.O_TRUNC) != 0 {
			return nil, errors.New("old generation opened writable")
		}
		return file, nil
	}}
	require.NoError(t, resumePrimaryUndo(context.Background(), fs, f.repo, f.op, j))
	f.success()
	require.NotEqual(t, oldStage, f.journal().Stage)
}

func TestPrimaryUndoPreservesPrivateReadOnlyExecutableModes(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0400, 0700, 0750, 0000} {
		t.Run(mode.String(), func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			require.NoError(t, os.Chmod(f.op.NewPath, mode))
			probe, readErr := os.Open(f.op.NewPath)
			if probe != nil {
				require.NoError(t, probe.Close())
			}
			err := beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op)
			if readErr != nil {
				require.Error(t, err)
				require.FileExists(t, f.op.NewPath)
				require.NoFileExists(t, f.op.OriginalPath)
				return
			}
			require.NoError(t, err)
			info, err := os.Stat(f.op.OriginalPath)
			require.NoError(t, err)
			require.Equal(t, mode, info.Mode().Perm())
			require.NoFileExists(t, f.op.NewPath)
		})
	}
}
