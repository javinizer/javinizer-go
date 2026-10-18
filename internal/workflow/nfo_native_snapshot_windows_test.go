//go:build windows

package workflow

import (
	"path/filepath"
	"testing"
)

func TestNFONativeWindowsSnapshotRetainsVolume(t *testing.T) {
	for _, p := range []string{`C:\incoming\movie.nfo`, `D:\incoming\movie.nfo`, `\\synthetic-server\share\incoming\movie.nfo`} {
		t.Run(p, func(t *testing.T) {
			checkNFONativeSnapshotPath(t, p, filepath.Clean(p))
			checkNFONativeCapturePersistence(t, filepath.Join(filepath.Dir(p), "movie.mp4"), p)
		})
	}
}
