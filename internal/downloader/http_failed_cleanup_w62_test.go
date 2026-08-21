package downloader

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// Wave-62 (codex P2, PR#215): a FAILED install must bind its tempPath
// cleanup identically to the wave-59 skipped leg — a foreign occupant that
// replaced tempPath after validation is preserved, never pathname-unlinked.

// w62CancelPlantFs drives both wedges on the destination's busy marker: when
// the marker is created (O_CREATE), it cancels ctx so installOverwriting's
// wave-62 gate refuses after the marker lands; on Lstat of a *.tmp staged
// name after cancellation, it plants foreign bytes at that name.
type w62CancelPlantFs struct {
	afero.Fs
	dest      string
	cancel    context.CancelFunc
	ctx       context.Context
	canceled  bool
	planted   bool
	plantData []byte
}

func (f *w62CancelPlantFs) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	if !f.canceled && strings.HasPrefix(name, f.dest+".dlbusy") && flags&os.O_CREATE != 0 {
		f.cancel()
		f.canceled = true
	}
	return f.Fs.OpenFile(name, flags, perm)
}

func (f *w62CancelPlantFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.canceled && !f.planted && strings.HasSuffix(name, ".tmp") {
		if err := afero.WriteFile(f.Fs, name, f.plantData, 0o600); err != nil {
			return nil, false, err
		}
		f.planted = true
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func TestDownloadW62_FailedInstallPreservesForeignStagedOccupant(t *testing.T) {
	payload := []byte("w62-image bytes")
	server := w41MediaServer(t, payload)
	defer server.Close()

	base := afero.NewMemMapFs()
	destDir := "/output/W62-FAIL"
	dest := destDir + "/poster.jpg"
	require.NoError(t, base.MkdirAll(destDir, 0o755))

	ctx, cancel := context.WithCancel(context.Background())
	plant := []byte("w62 foreign occupant at tempPath")
	fs := &w62CancelPlantFs{Fs: base, dest: dest, cancel: cancel, ctx: ctx, plantData: plant}

	var logs bytes.Buffer
	restore := logging.SetOutput(&logs)
	defer restore()

	d := NewDownloader(server.Client(), fs, &Config{}, nil)
	result, err := d.download(ctx, server.URL+"/poster.jpg", dest, MediaTypePoster, true)
	require.Error(t, err)
	require.True(t, fs.canceled, "the cancel fired inside the busy-marker acquisition")
	_ = result

	// The staged name a foreign occupant replaced must be preserved byte-intact.
	entries, rerr := afero.ReadDir(base, destDir)
	require.NoError(t, rerr)
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			got, gerr := afero.ReadFile(base, destDir+"/"+e.Name())
			require.NoError(t, gerr)
			if string(got) == string(plant) {
				found = true
			}
		}
	}
	require.True(t, found, "the foreign occupant at the staged name survived the failed install's cleanup")
	// The wave-62 refusal text pinned the canceled-acquire leg; the preserving
	// cleanup log is what we actually assert (that's the codex fix surface).
	require.Contains(t, logs.String(), "no longer provably names the validated download",
		"the wave-62 preserve-dynamic log confirms foreign bytes are untouched")
}
