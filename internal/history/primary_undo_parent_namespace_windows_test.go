//go:build windows

package history

import (
	"path/filepath"
	"testing"
)

func TestPrimaryUndoWindowsAbsoluteParentNamespace(t *testing.T) {
	for _, destination := range []string{`C:\incoming\movie.mkv`, `D:\incoming\movie.mkv`, `\\synthetic-server\share\incoming\movie.mkv`} {
		t.Run(destination, func(t *testing.T) {
			primaryUndoCheckParentNamespace(t, destination, filepath.Dir(destination))
		})
	}
}
