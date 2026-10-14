package history

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPrimaryUndoCancellationBoundariesRetainRecovery(t *testing.T) {
	for _, cut := range []string{"before-copy", "before-metadata", "before-publish", "after-publish", "after-publish-error", "after-take-error", "destination-hash", "source-hash", "after-claim-source-hash", "before-take", "claimed-hash", "before-unlink", "after-unlink"} {
		t.Run(cut, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			state := "published"
			if cut == "before-copy" || cut == "before-metadata" {
				state = "planned"
			} else if cut == "before-publish" || cut == "after-publish" || cut == "after-publish-error" {
				state = "sealed"
			}
			j := f.pause(state)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			open, publish, remove, syncParent := restoreOpenReplacementSource, primaryUndoPublish, primaryUndoRemove, primaryUndoSyncParent
			restore := func() {
				restoreOpenReplacementSource, primaryUndoPublish, primaryUndoRemove, primaryUndoSyncParent = open, publish, remove, syncParent
			}
			t.Cleanup(restore)
			fired, canceledSyncs := false, 0
			stop := func() { fired = true; cancel() }
			opens := map[string]int{}
			restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
				file, err := open(fs, path)
				if err != nil {
					return nil, err
				}
				opens[path]++
				at := opens[path]
				if cut == "before-copy" && path == j.Source {
					stop()
				}
				return &primaryUndoFaultFile{File: file, fail: func(operation string, n int) error {
					if fired {
						return nil
					}
					claim := strings.Contains(path, ".undo-remove.")
					if (cut == "before-metadata" && path == j.Source && operation == "stat" && n == 5) ||
						(cut == "before-publish" && path == j.Stage && at == 1 && operation == "close") ||
						(cut == "destination-hash" && path == j.Destination && operation == "read") ||
						(cut == "source-hash" && path == j.Source && operation == "read") ||
						(cut == "after-claim-source-hash" && path == j.Source && at == 3 && operation == "read") ||
						(cut == "before-take" && path == j.Source && at == 3 && operation == "close") ||
						(cut == "claimed-hash" && claim && operation == "read") ||
						(cut == "before-unlink" && claim && at == 3 && operation == "close") {
						stop()
					}
					return nil
				}}, nil
			}
			primaryUndoPublish = func(fs afero.Fs, source, destination string) error {
				err := publish(fs, source, destination)
				if err == nil && source == j.Stage && (cut == "after-publish" || cut == "after-publish-error") {
					stop()
				}
				if err == nil && ((cut == "after-publish-error" && source == j.Stage) || (cut == "after-take-error" && source == j.Source)) {
					stop()
					return fsutil.ErrPublishCompleted
				}
				return err
			}
			primaryUndoRemove = func(fs afero.Fs, path string) error {
				err := remove(fs, path)
				if err == nil && cut == "after-unlink" {
					stop()
				}
				return err
			}
			primaryUndoSyncParent = func(path string) error {
				if ctx.Err() != nil {
					canceledSyncs++
				}
				return syncParent(path)
			}
			err := resumePrimaryUndo(ctx, f.fs, f.repo, f.op, j)
			require.True(t, fired, cut)
			require.ErrorIs(t, err, context.Canceled)
			if cut == "after-take-error" {
				require.ErrorIs(t, err, fsutil.ErrPublishCompleted)
			}
			if cut == "after-publish" || cut == "after-publish-error" || cut == "after-take-error" || cut == "claimed-hash" || cut == "before-unlink" || cut == "after-unlink" {
				require.Positive(t, canceledSyncs, "completed namespace mutations still need their sync tail")
			}
			restore()
			f.reopen()
			saved := f.journal()
			beforeRetry := f.op.GeneratedFiles
			if saved.State == "unlinking" {
				got, err := os.ReadFile(saved.Destination)
				require.NoError(t, err)
				require.Equal(t, "synthetic primary video", string(got))
				if cut == "claimed-hash" || cut == "before-unlink" || cut == "after-take-error" {
					got, err = os.ReadFile(saved.Cleanup)
					require.NoError(t, err)
					require.Equal(t, "synthetic primary video", string(got))
					require.NoFileExists(t, saved.Source)
				} else if cut == "after-unlink" {
					require.NoFileExists(t, saved.Source)
					require.NoFileExists(t, saved.Cleanup)
				} else {
					require.FileExists(t, saved.Source)
					require.NoFileExists(t, saved.Cleanup)
				}
				require.ErrorContains(t, f.resume(), "manual recovery")
				f.reload()
				require.Equal(t, beforeRetry, f.op.GeneratedFiles)
			} else {
				require.FileExists(t, saved.Source)
				if cut == "before-copy" || cut == "before-metadata" || cut == "before-publish" {
					require.NoFileExists(t, saved.Destination)
				}
				require.NoError(t, f.resume())
				f.success()
			}
		})
	}
}

func TestPrimaryUndoCanceledEntryDoesNotRotateOrOpen(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("copying")
	before := f.op.GeneratedFiles
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, resumePrimaryUndo(ctx, nil, nil, f.op, j), context.Canceled)
	require.ErrorIs(t, copyPrimaryUndo(ctx, nil, nil, nil, nil), context.Canceled)
	require.ErrorIs(t, beginPrimaryUndo(ctx, nil, nil, nil), context.Canceled)
	require.Equal(t, before, f.op.GeneratedFiles)
	require.Empty(t, j.SupersededStages)
	f.reopen()
	require.NoError(t, f.resume())
	f.success()
}

func TestPrimaryUndoSupersededCancellationRetainsClaims(t *testing.T) {
	for _, cut := range []string{"destination-hash", "before-take", "after-take", "after-take-error", "before-unlink", "after-unlink"} {
		t.Run(cut, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			j := f.pause("copying")
			stopAfterRemoval := &primaryUndoFaultRepo{BatchFileOperationRepositoryInterface: f.repo, state: "primary_removed", after: true}
			require.Error(t, resumePrimaryUndo(context.Background(), f.fs, stopAfterRemoval, f.op, j))
			require.True(t, stopAfterRemoval.fired)
			j = f.journal()
			require.Len(t, j.SupersededStages, 1)
			stage := j.SupersededStages[0]
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			open, publish, remove, syncParent := restoreOpenReplacementSource, primaryUndoPublish, primaryUndoRemove, primaryUndoSyncParent
			restore := func() {
				restoreOpenReplacementSource, primaryUndoPublish, primaryUndoRemove, primaryUndoSyncParent = open, publish, remove, syncParent
			}
			t.Cleanup(restore)
			fired, canceledSyncs, destinationOpens := false, 0, 0
			stop := func() { fired = true; cancel() }
			restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
				file, err := open(fs, path)
				if err != nil {
					return nil, err
				}
				if path == j.Destination {
					destinationOpens++
				}
				at := destinationOpens
				return &primaryUndoFaultFile{File: file, fail: func(operation string, _ int) error {
					if !fired && path == j.Destination &&
						((cut == "destination-hash" && operation == "read") ||
							(cut == "before-take" && at == 3 && operation == "close") ||
							(cut == "before-unlink" && at == 5 && operation == "close")) {
						stop()
					}
					return nil
				}}, nil
			}
			primaryUndoPublish = func(fs afero.Fs, source, destination string) error {
				err := publish(fs, source, destination)
				if err == nil && source == stage.Path && (cut == "after-take" || cut == "after-take-error") {
					stop()
					if cut == "after-take-error" {
						return fsutil.ErrPublishCompleted
					}
				}
				return err
			}
			primaryUndoRemove = func(fs afero.Fs, path string) error {
				err := remove(fs, path)
				if err == nil && cut == "after-unlink" {
					stop()
				}
				return err
			}
			primaryUndoSyncParent = func(path string) error {
				if ctx.Err() != nil {
					canceledSyncs++
				}
				return syncParent(path)
			}
			err := resumePrimaryUndo(ctx, f.fs, f.repo, f.op, j)
			require.True(t, fired)
			require.ErrorIs(t, err, context.Canceled)
			if cut == "after-take-error" {
				require.ErrorIs(t, err, fsutil.ErrPublishCompleted)
			}
			if cut == "after-take" || cut == "after-take-error" || cut == "before-unlink" || cut == "after-unlink" {
				require.Positive(t, canceledSyncs)
			}
			restore()
			f.reopen()
			saved := f.journal()
			require.Equal(t, "primary_removed", saved.State)
			require.Len(t, saved.SupersededStages, 1)
			got, err := os.ReadFile(saved.Destination)
			require.NoError(t, err)
			require.Equal(t, "synthetic primary video", string(got))
			require.NoFileExists(t, saved.Source)
			stage = saved.SupersededStages[0]
			if cut == "destination-hash" {
				require.Empty(t, stage.Cleanup)
				require.FileExists(t, stage.Path)
				require.NoError(t, f.resume())
				f.success()
				return
			}
			require.NotEmpty(t, stage.Cleanup)
			if cut == "before-take" {
				require.FileExists(t, stage.Path)
				require.NoFileExists(t, stage.Cleanup)
			} else {
				require.NoFileExists(t, stage.Path)
				if cut == "after-unlink" {
					require.NoFileExists(t, stage.Cleanup)
				} else {
					require.FileExists(t, stage.Cleanup)
				}
			}
			beforeRetry := f.op.GeneratedFiles
			require.ErrorContains(t, f.resume(), "manual recovery")
			f.reload()
			require.Equal(t, beforeRetry, f.op.GeneratedFiles)
		})
	}
}
