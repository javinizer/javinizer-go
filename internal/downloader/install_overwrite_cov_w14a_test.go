package downloader

import (
	"context"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type w14ABusyObservingRecorder struct {
	fs             afero.Fs
	recordSawBusy  bool
	confirmSawBusy bool
}

func (r *w14ABusyObservingRecorder) RecordReplacement(_ context.Context, _, replacedPath, _ string, _ ...models.ReplacementBackupFacts) error {
	_, err := r.fs.Stat(fsutil.ReplacementBusyPath(replacedPath))
	r.recordSawBusy = err == nil
	return nil
}

func (r *w14ABusyObservingRecorder) ConfirmReplacement(_ context.Context, _, replacedPath, _ string) error {
	_, err := r.fs.Stat(fsutil.ReplacementBusyPath(replacedPath))
	r.confirmSawBusy = err == nil
	return nil
}

func (*w14ABusyObservingRecorder) ReleaseReplacement(context.Context, string, string, string) error {
	return nil
}

func (*w14ABusyObservingRecorder) MarkReplacementRestorePendingKind(context.Context, string, string, string, string) error {
	return nil
}

func TestInstallOverwritingW14A_BusyMarkerCoversArmToConfirm(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W14A-DL/poster.jpg"
	staged := "/out/W14A-DL/poster.tmp"
	require.NoError(t, fs.MkdirAll("/out/W14A-DL", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))
	recorder := &w14ABusyObservingRecorder{fs: fs}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w14a-arm", recorder: recorder})
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced)
	require.True(t, recorder.recordSawBusy, "the marker exists before durable journal arming returns")
	require.True(t, recorder.confirmSawBusy, "the marker remains through install confirmation")
	_, err = fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist, "successful confirmation releases the marker")
	got, readErr := afero.ReadFile(fs, dest)
	require.NoError(t, readErr)
	require.Equal(t, "new", string(got))
}

func TestInstallOverwritingW14A_BusyMarkerWriteFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/W14A-DL-ERR/poster.jpg"
	staged := "/out/W14A-DL-ERR/poster.tmp"
	require.NoError(t, base.MkdirAll("/out/W14A-DL-ERR", 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
	d := NewDownloader(nil, afero.NewReadOnlyFs(base), &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w14a-marker-error", recorder: &armedTestLedger{}})
	require.Error(t, err)
}

func TestInstallOverwritingW14A_LiveBusyRefusesSecondProcess(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/W14A-DL-BUSY/poster.jpg"
	staged := "/out/W14A-DL-BUSY/poster.tmp"
	require.NoError(t, fs.MkdirAll("/out/W14A-DL-BUSY", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new"), 0o644))
	busyRelease, err := fsutil.AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w14a-second", recorder: &armedTestLedger{}})
	require.NoError(t, err)
	require.True(t, skipped)
	require.True(t, replaced)
	got, readErr := afero.ReadFile(fs, dest)
	require.NoError(t, readErr)
	require.Equal(t, "old", string(got))
	busyRelease()
}
