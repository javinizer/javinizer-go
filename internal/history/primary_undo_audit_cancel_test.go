package history

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type auditCancelRead struct {
	afero.File
	cancel context.CancelFunc
	total  *int
	after  *int
	ctx    context.Context
}

func (f *auditCancelRead) Read(p []byte) (int, error) {
	was := f.ctx.Err() != nil
	n, e := f.File.Read(p)
	*f.total += n
	if was {
		*f.after += n
	}
	if n > 0 {
		f.cancel()
	}
	return n, e
}
func (f *auditCancelRead) Fd() uintptr { return f.File.(interface{ Fd() uintptr }).Fd() }
func TestAuditPrimaryUndoCancellationCopy(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	payload := bytes.Repeat([]byte("synthetic"), 1024*1024)
	require.NoError(t, os.WriteFile(f.op.NewPath, payload, 0600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	total, after := 0, 0
	original := restoreOpenReplacementSource
	t.Cleanup(func() { restoreOpenReplacementSource = original })
	restoreOpenReplacementSource = func(fs afero.Fs, p string) (afero.File, error) {
		file, err := original(fs, p)
		if err == nil && p == f.op.NewPath {
			return &auditCancelRead{File: file, cancel: cancel, total: &total, after: &after, ctx: ctx}, nil
		}
		return file, err
	}
	err := beginPrimaryUndo(ctx, f.fs, f.repo, f.op)
	require.ErrorIs(t, err, context.Canceled)
	t.Logf("source bytes=%d total read=%d read after cancellation=%d", len(payload), total, after)
	require.FileExists(t, f.op.NewPath)
	restoreOpenReplacementSource = original
	f.reopen()
	require.NoError(t, f.resume())
	got, e := os.ReadFile(f.op.OriginalPath)
	require.NoError(t, e)
	require.Equal(t, payload, got)
	require.Zero(t, after, "primary Undo must stop reading further chunks after context cancellation")
}
func TestAuditPrimaryUndoCancellationSealedPublication(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	j := f.pause("sealed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	total, after := 0, 0
	original := restoreOpenReplacementSource
	t.Cleanup(func() { restoreOpenReplacementSource = original })
	restoreOpenReplacementSource = func(fs afero.Fs, p string) (afero.File, error) {
		file, err := original(fs, p)
		if err == nil && p == j.Stage {
			return &auditCancelRead{File: file, cancel: cancel, total: &total, after: &after, ctx: ctx}, nil
		}
		return file, err
	}
	err := resumePrimaryUndo(ctx, f.fs, f.repo, f.op, j)
	require.ErrorIs(t, err, context.Canceled)
	require.FileExists(t, f.op.NewPath)
	_, publishedErr := os.Stat(f.op.OriginalPath)
	t.Logf("canceled during sealed verification; destination published=%v", publishedErr == nil)
	restoreOpenReplacementSource = original
	f.reopen()
	require.NoError(t, f.resume())
	f.success()
	require.True(t, os.IsNotExist(publishedErr), "cancellation during verification must stop new publication")
}
func TestAuditPrimaryUndoAlreadyCanceledAdmission(t *testing.T) {
	f := newPrimaryUndoFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := beginPrimaryUndo(ctx, f.fs, f.repo, f.op)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, f.journal())
	require.NoFileExists(t, f.op.OriginalPath)
	got, e := os.ReadFile(f.op.NewPath)
	require.NoError(t, e)
	require.Equal(t, "synthetic primary video", string(got))
}
