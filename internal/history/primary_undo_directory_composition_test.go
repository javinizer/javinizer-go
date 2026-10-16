package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// DirectoryMove is a resident-only inverse. Keeping a malformed combined
// journal is intentional: neither ownership claim can authorize the other.
func TestPrimaryUndoDirectoryCompositionContradictionsSurviveReopen(t *testing.T) {
	for _, state := range []string{"copying", "published", "done"} {
		for _, resident := range []bool{false, true} {
			for _, withHook := range []bool{false, true} {
				t.Run(state+map[bool]string{false: "/no-resident", true: "/resident"}[resident]+map[bool]string{false: "/no-hook", true: "/hook"}[withHook], func(t *testing.T) {
					f := newPrimaryUndoFixture(t)
					pauseState := state
					if state == "done" {
						pauseState = "published"
					}
					j := f.pause(pauseState)
					if state == "done" {
						require.NoError(t, f.resume())
						j = f.journal()
					}
					ledger, err := models.ParseGeneratedFiles(f.op.GeneratedFiles)
					require.NoError(t, err)
					ledger.PrimaryResident = resident
					// Deliberately missing directory identity: the ownership contradiction
					// must win before a directory-location error can rewrite row status.
					ledger.DirectoryMove = &models.DirectoryMove{Original: filepath.Join(filepath.Dir(j.Destination), "missing-original"), Locations: []string{filepath.Join(filepath.Dir(j.Source), "missing-current")}}
					f.op.GeneratedFiles = models.MarshalLedgerJSON(ledger)
					require.NoError(t, f.repo.Update(context.Background(), f.op))
					before := f.op.GeneratedFiles
					contents := make(map[string][]byte)
					identities := make(map[string]os.FileInfo)
					for _, path := range []string{j.Source, j.Stage, j.Destination} {
						info, err := os.Stat(path)
						if os.IsNotExist(err) {
							continue
						}
						require.NoError(t, err)
						identities[path] = info
						contents[path], err = os.ReadFile(path)
						require.NoError(t, err)
					}
					f.reopen()
					result, err := NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
					require.NoError(t, err)
					require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
					require.Contains(t, result.Error, "conflicting resident")
					require.Contains(t, result.Error, "manual recovery")
					old := time.Now().Add(-48 * time.Hour)
					jobs := database.NewJobRepository(f.db)
					require.NoError(t, jobs.Create(context.Background(), &models.Job{ID: f.op.BatchJobID, Status: models.JobStatusOrganized, StartedAt: old, OrganizedAt: &old}))
					if withHook {
						jobs.SetOrganizedJobPruneHook(NewReplacementSweeper(f.fs, f.repo).PruneOperationBackups)
					}
					require.NoError(t, jobs.DeleteOrganizedOlderThan(context.Background(), time.Now().Add(-24*time.Hour)))
					f.reopen()
					requireOnlyUndoAdmissionChanged(t, before, f.op.GeneratedFiles)
					require.Equal(t, models.RevertStatusApplied, f.op.RevertStatus)
					job, err := database.NewJobRepository(f.db).FindByID(context.Background(), f.op.BatchJobID)
					require.NoError(t, err)
					require.Equal(t, models.JobStatusOrganized, job.Status)
					for _, path := range []string{j.Source, j.Stage, j.Destination} {
						info, err := os.Stat(path)
						if identities[path] == nil {
							require.True(t, os.IsNotExist(err), path)
							continue
						}
						require.NoError(t, err)
						require.True(t, os.SameFile(identities[path], info), path)
						got, err := os.ReadFile(path)
						require.NoError(t, err)
						require.Equal(t, contents[path], got, path)
					}
					claims, err := filepath.Glob(j.Source + ".undo-remove.*")
					require.NoError(t, err)
					require.Empty(t, claims)
				})
			}
		}
	}
}

func TestPrimaryUndoDirectoryCompositionFreshClaimBlocksCAS(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("published")
	stale := *f.op
	ledger, err := models.ParseGeneratedFiles(f.op.GeneratedFiles)
	require.NoError(t, err)
	ledger.DirectoryMove = &models.DirectoryMove{Original: "synthetic-directory-claim"}
	f.op.GeneratedFiles = models.MarshalLedgerJSON(ledger)
	require.NoError(t, f.repo.Update(context.Background(), f.op))
	before := f.op.GeneratedFiles
	f.reopen()
	require.ErrorContains(t, primaryUndoState(context.Background(), f.repo, &stale, j, "unlinking"), "conflicting resident")
	f.reopen()
	require.Equal(t, before, f.op.GeneratedFiles)
	require.Equal(t, models.RevertStatusApplied, f.op.RevertStatus)
	for _, path := range []string{j.Source, j.Destination} {
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "synthetic primary video", string(got))
	}
}
