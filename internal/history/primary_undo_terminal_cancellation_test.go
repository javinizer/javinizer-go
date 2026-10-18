package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPrimaryUndoTerminalCloseCancellationPreservesErrors(t *testing.T) {
	for _, fault := range []string{"none", "read", "named-close"} {
		t.Run(fault, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			require.NoError(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
			j := f.journal()
			require.Equal(t, "done", j.State)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			open, syncParent := restoreOpenReplacementSource, primaryUndoSyncParent
			t.Cleanup(func() { restoreOpenReplacementSource, primaryUndoSyncParent = open, syncParent })
			primaryUndoSyncParent = func(string) error {
				t.Error("terminal verification must not sync an unmodified namespace")
				return nil
			}
			failure := errors.New("synthetic terminal verification failure")
			opens, closes := 0, 0
			restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
				file, err := open(fs, path)
				if err != nil || path != j.Destination {
					return file, err
				}
				opens++
				at := opens
				return &primaryUndoFaultFile{File: file, fail: func(operation string, _ int) error {
					if operation == "close" {
						closes++
						if at == 1 {
							// The read pin closes after verification's return value
							// has been evaluated, on both success and error paths.
							cancel()
						}
					}
					if (fault == "read" && operation == "read") ||
						(fault == "named-close" && at == 2 && operation == "close") {
						return failure
					}
					return nil
				}}, nil
			}
			err := resumePrimaryUndo(ctx, f.fs, f.repo, f.op, j)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.ErrorIs(t, err, context.Canceled)
			if fault != "none" {
				require.ErrorIs(t, err, failure)
			}
			require.Equal(t, opens, closes, "all verification pins must still close")
			f.success()
		})
	}
}

func TestPrimaryUndoTerminalCloseCancellationBlocksMetadata(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	require.NoError(t, beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op))
	require.Equal(t, "done", f.journal().State)
	artifact := filepath.Join(filepath.Dir(f.op.NewPath), "generated.nfo")
	nfoPath := filepath.Join(filepath.Dir(f.op.OriginalPath), "movie.nfo")
	require.NoError(t, os.WriteFile(artifact, []byte("generated metadata"), 0600))
	require.NoError(t, os.WriteFile(nfoPath, []byte("new metadata"), 0600))
	ledger, err := models.ParseGeneratedFiles(f.op.GeneratedFiles)
	require.NoError(t, err)
	ledger.Delete = append(ledger.Delete, artifact)
	f.op.GeneratedFiles = models.MarshalLedgerJSON(ledger)
	f.op.NFOPath, f.op.NFOSnapshot = nfoPath, "saved metadata"
	require.NoError(t, f.repo.Update(context.Background(), f.op))
	f.reopen()
	beforeLedger, beforeStatus := f.op.GeneratedFiles, f.op.RevertStatus
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	open := restoreOpenReplacementSource
	t.Cleanup(func() { restoreOpenReplacementSource = open })
	opens, closes := 0, 0
	restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
		file, err := open(fs, path)
		if err != nil || path != f.op.OriginalPath {
			return file, err
		}
		opens++
		if opens != 1 {
			return file, nil
		}
		return &primaryUndoFaultFile{File: file, fail: func(operation string, _ int) error {
			if operation == "close" {
				closes++
				cancel()
			}
			return nil
		}}, nil
	}
	result, err := NewReverter(f.fs, f.repo).revertFile(ctx, f.op)
	require.Equal(t, 1, closes, "cancel only from the terminal verification pin's deferred Close")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	require.Contains(t, result.Error, context.Canceled.Error())
	artifactBody, err := os.ReadFile(artifact)
	require.NoError(t, err, "cancellation must prevent generated-file removal")
	require.Equal(t, "generated metadata", string(artifactBody))
	nfoBody, err := os.ReadFile(nfoPath)
	require.NoError(t, err)
	require.Equal(t, "new metadata", string(nfoBody), "cancellation must prevent NFO snapshot restoration")
	restoreOpenReplacementSource = open
	f.reopen()
	require.Equal(t, beforeStatus, f.op.RevertStatus)
	requireOnlyUndoAdmissionChanged(t, beforeLedger, f.op.GeneratedFiles)
	f.success()

	result, err = NewReverter(f.fs, f.repo).revertFile(context.Background(), f.op)
	require.NoError(t, err)
	require.Equal(t, models.RevertOutcomeReverted, result.Outcome)
	require.NoFileExists(t, artifact)
	nfoBody, err = os.ReadFile(nfoPath)
	require.NoError(t, err)
	require.Equal(t, "saved metadata", string(nfoBody))
	f.success()
}
