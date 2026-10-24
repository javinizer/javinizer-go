//go:build !windows

package artifactstore

import "os"

func snapshotRoot(path string) (os.FileInfo, error) { return os.Lstat(path) }
