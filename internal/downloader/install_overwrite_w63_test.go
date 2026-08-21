package downloader

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Wave-62 (codex P2, PR#215): a canceled ctx must NOT ride a just-acquired
// marker into a publish — the check must run between the (blocking) marker
// acquisition and ANY destination mutation.
func TestInstallOverwritingW63_CancelAfterBusyAcquireRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/out/w63/poster.jpg", []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/out/w63/poster.tmp", []byte("new"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel INSIDE the busy-marker acquisition: the pre-marker lock-path
	// check (line ~993) must not be the arm that fires; the wave-62 gate
	// itself is what must refuse.
	base2 := &w63CancelOnBusyAcquireFs{Fs: base, dest: "/out/w63/poster.jpg", cancel: cancel}
	d := NewDownloader(nil, base2, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, _, err := d.installOverwriting(ctx, "/out/w63/poster.tmp", "/out/w63/poster.jpg", downloadLedger{
		opID: "w63", recorder: &armedTestLedger{},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, base2.canceled, "the cancellation happened inside the marker acquisition")
	require.Equal(t, "old", string(mustReadDownloaderW7(t, base, "/out/w63/poster.jpg")),
		"dest never written after cancellation")
}

// w63CancelOnBusyAcquireFs cancels the context the moment the process writes
// the destination's busy marker — exactly the racing boundary the wave-62
// gate defends.
type w63CancelOnBusyAcquireFs struct {
	afero.Fs
	dest     string
	cancel   context.CancelFunc
	canceled bool
}

func (f *w63CancelOnBusyAcquireFs) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	if !f.canceled && strings.HasPrefix(name, f.dest+".dlbusy") && flags&os.O_CREATE != 0 {
		f.cancel()
		f.canceled = true
	}
	return f.Fs.OpenFile(name, flags, perm)
}
