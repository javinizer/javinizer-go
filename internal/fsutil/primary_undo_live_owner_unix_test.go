//go:build darwin || linux

package fsutil_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const primaryUndoOwnerDestination = "JAVINIZER_TEST_PRIMARY_UNDO_OWNER_DESTINATION"

// The child owns a real replacement claim until the parent closes its input.
// No liveness or process-start probe is replaced in either process.
func TestPrimaryUndoLiveReplacementOwnerHelper(t *testing.T) {
	destination := os.Getenv(primaryUndoOwnerDestination)
	if destination == "" {
		return
	}
	release, token, err := fsutil.AcquireReplacementBusyEx(afero.NewOsFs(), destination)
	require.NoError(t, err)
	defer release()
	_, err = fmt.Fprintln(os.Stdout, token)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, os.Stdin)
	require.NoError(t, err)
}

func TestPrimaryUndoLiveReplacementOwnerPreservesPendingRestore(t *testing.T) {
	for _, age := range []time.Duration{0, time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			if age > 0 && runtime.GOOS != "darwin" {
				t.Skip("Darwin has no process-start evidence; Linux recognizes a token predating this child as PID reuse")
			}
			root := t.TempDir()
			source := filepath.Join(root, "incoming", "ABC-123.mp4")
			destination := filepath.Join(root, "library", "ABC-123.mp4")
			backup := destination + ".dlbak.0123456789abcdef"
			require.NoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
			require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o755))
			require.NoError(t, os.WriteFile(source, []byte("unmoved primary video"), 0o600))
			require.NoError(t, os.WriteFile(backup, []byte("previous library video"), 0o600))

			db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "history.sqlite"), LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.AutoMigrate(&models.BatchFileOperation{}, &models.Job{}))
			repo := database.NewBatchFileOperationRepository(db)
			job := models.Job{ID: "live-primary-replacement", Status: models.JobStatusFailed, TotalFiles: 1, Failed: 1}
			require.NoError(t, db.Create(&job).Error)
			// This is the journal left when a reviewed replacement set the old
			// primary aside, but the new primary was never published.
			op := &models.BatchFileOperation{
				BatchJobID: job.ID, MovieID: "ABC-123",
				OriginalPath: source, OperationType: models.OperationTypeMove,
				RevertStatus: models.RevertStatusApplied,
				GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
					MoveBack:     []models.FileMove{{OriginalPath: source, NewPath: destination}},
					Replacements: []models.ReplacementEntry{{Destination: destination, Backup: backup, DestSeq: 1}},
				}),
			}
			require.NoError(t, repo.Create(t.Context(), op))
			originalLedger, err := models.ParseGeneratedFiles(op.GeneratedFiles)
			require.NoError(t, err)

			childCtx, cancelChild := context.WithTimeout(t.Context(), 30*time.Second)
			t.Cleanup(cancelChild)
			child := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestPrimaryUndoLiveReplacementOwnerHelper$")
			child.Env = append(os.Environ(), primaryUndoOwnerDestination+"="+destination)
			var childStderr bytes.Buffer
			child.Stderr = &childStderr
			stdin, err := child.StdinPipe()
			require.NoError(t, err)
			stdout, err := child.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, child.Start())
			waited := false
			t.Cleanup(func() {
				if !waited {
					_ = stdin.Close()
					_ = child.Process.Kill()
					_ = child.Wait()
					if childStderr.Len() > 0 {
						t.Logf("replacement owner stderr: %s", childStderr.String())
					}
				}
			})
			token, err := bufio.NewReader(stdout).ReadString('\n')
			require.NoError(t, err)
			token = strings.TrimSpace(token)
			require.Contains(t, token, fmt.Sprintf("pid=%d,", child.Process.Pid))
			marker := fsutil.ReplacementBusyPath(destination)
			if age > 0 {
				// Age only the on-disk marker fixture. Without a process-start
				// timestamp, a live Darwin owner must still defeat age alone.
				token = fmt.Sprintf("pid=%d,time=%d", child.Process.Pid, time.Now().Add(-age).UnixNano())
				require.NoError(t, os.WriteFile(marker, []byte(token), 0o600))
			}
			require.NoError(t, child.Process.Signal(syscall.Signal(0)))
			markerBefore, err := os.Stat(marker)
			require.NoError(t, err)

			reverter := history.NewReverter(afero.NewOsFs(), repo)
			result, err := reverter.RevertBatch(t.Context(), op.BatchJobID)
			require.NoError(t, err)
			require.Equal(t, 1, result.Failed)
			require.Len(t, result.Outcomes, 1)
			require.Contains(t, result.Outcomes[0].Error, fsutil.ErrReplacementBusy.Error())
			require.NoFileExists(t, destination)
			for path, want := range map[string]string{source: "unmoved primary video", backup: "previous library video", marker: token} {
				data, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(data))
			}
			markerAfter, err := os.Stat(marker)
			require.NoError(t, err)
			require.True(t, os.SameFile(markerBefore, markerAfter), "busy refusal must not replace the owner's marker")
			stored, err := repo.FindByID(t.Context(), op.ID)
			require.NoError(t, err)
			require.Equal(t, models.RevertStatusApplied, stored.RevertStatus)
			ledger, err := models.ParseGeneratedFiles(stored.GeneratedFiles)
			require.NoError(t, err)
			require.True(t, ledger.UndoStarted)
			require.Equal(t, originalLedger.MoveBack, ledger.MoveBack)
			require.Equal(t, originalLedger.Replacements, ledger.Replacements, "the live owner keeps the pending inverse armed")
			var storedJob models.Job
			require.NoError(t, db.First(&storedJob, "id = ?", job.ID).Error)
			require.Equal(t, job.Status, storedJob.Status)
			require.Equal(t, job.PruneVersion, storedJob.PruneVersion)
			require.Equal(t, job.EnvelopeGeneration, storedJob.EnvelopeGeneration)
			require.Nil(t, storedJob.RevertedAt)
			require.NoError(t, child.Process.Signal(syscall.Signal(0)))

			require.NoError(t, stdin.Close())
			err = child.Wait()
			waited = true
			require.NoError(t, err, "replacement owner stderr: %s", childStderr.String())
			// Once the real owner exits, a new claimant may acquire the marker.
			// The aged Darwin token was intentionally retained by the child's
			// original release capability, so this also checks dead-owner reclaim.
			release, successorToken, err := fsutil.AcquireReplacementBusyEx(afero.NewOsFs(), destination)
			require.NoError(t, err)
			t.Cleanup(release)
			require.NotEqual(t, token, successorToken)
			release()
			require.NoFileExists(t, marker)
			require.NoFileExists(t, destination)
			for path, want := range map[string]string{source: "unmoved primary video", backup: "previous library video"} {
				data, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(data))
			}
			retained, err := repo.FindByID(t.Context(), op.ID)
			require.NoError(t, err)
			require.Equal(t, models.RevertStatusApplied, retained.RevertStatus)
			require.Equal(t, stored.GeneratedFiles, retained.GeneratedFiles)

		})
	}
}
