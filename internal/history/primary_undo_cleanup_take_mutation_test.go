package history

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPrimaryUndoCleanupTakeRestampedMutationRetained(t *testing.T) {
	for _, timing := range []string{"before-take", "after-take", "claimed-hash", "final-witness"} {
		t.Run(timing, func(t *testing.T) {
			f := newPrimaryUndoFixture(t)
			info, err := os.Stat(f.op.NewPath)
			require.NoError(t, err)
			mutated := bytes.Repeat([]byte("x"), int(info.Size()))
			// Model a tagger that opened the source before Undo. Its handle
			// still addresses the same inode after the cleanup rename.
			writer, err := os.OpenFile(f.op.NewPath, os.O_RDWR, 0)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, writer.Close()) })
			change := func(path string) {
				n, err := writer.WriteAt(mutated, 0)
				require.NoError(t, err)
				require.Equal(t, len(mutated), n)
				require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))
			}
			publish := primaryUndoPublish
			t.Cleanup(func() { primaryUndoPublish = publish })
			fired, changed := false, false
			claim := ""
			primaryUndoPublish = func(fs afero.Fs, src, dst string) error {
				if src != f.op.NewPath || !strings.Contains(dst, ".undo-remove.") {
					return publish(fs, src, dst)
				}
				fired = true
				claim = dst
				if timing == "before-take" {
					change(src)
					changed = true
				}
				err := publish(fs, src, dst)
				if err == nil && timing == "after-take" {
					change(dst)
					changed = true
				}
				return err
			}
			open := restoreOpenReplacementSource
			t.Cleanup(func() { restoreOpenReplacementSource = open })
			claimOpens := 0
			restoreOpenReplacementSource = func(fs afero.Fs, path string) (afero.File, error) {
				file, err := open(fs, path)
				if err != nil || claim == "" || path != claim {
					return file, err
				}
				claimOpens++
				if timing == "final-witness" && claimOpens == 3 {
					change(claim)
					changed = true
				}
				if timing == "claimed-hash" {
					return &primaryUndoFaultFile{File: file, afterRead: func() {
						if !changed {
							change(claim)
							changed = true
						}
					}}, nil
				}
				return file, nil
			}
			err = beginPrimaryUndo(context.Background(), f.fs, f.repo, f.op)
			require.True(t, fired)
			require.True(t, changed)
			require.Error(t, err, "an edit during the cleanup take must survive")
			f.reopen()
			j := f.journal()
			require.Equal(t, "unlinking", j.State)
			beforeRetry := f.op.GeneratedFiles
			got, err := os.ReadFile(j.Cleanup)
			require.NoError(t, err)
			require.Equal(t, mutated, got)
			require.NoFileExists(t, j.Source)
			got, err = os.ReadFile(j.Destination)
			require.NoError(t, err)
			require.Equal(t, "synthetic primary video", string(got))
			require.ErrorContains(t, f.resume(), "manual recovery")
			f.reload()
			require.Equal(t, beforeRetry, f.op.GeneratedFiles)
			got, err = os.ReadFile(j.Cleanup)
			require.NoError(t, err)
			require.Equal(t, mutated, got)
		})
	}
}
