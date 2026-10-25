//go:build darwin || linux

package actress

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func provisionPlatformSupported() error { return nil }

// Refuse symlink/special-file leaves before opening, then use no-follow and
// nonblocking admission so a substituted FIFO cannot hang the CLI. Pin and
// verify the same regular inode; the operator must exclude concurrent writers.
func openProvisionInput(path string, maxBytes int64) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > maxBytes {
		return nil, fmt.Errorf("dump must be a bounded regular file, not a symlink or special file")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	after, statErr := file.Stat()
	leaf, leafErr := os.Lstat(path)
	if statErr != nil || leafErr != nil {
		_ = file.Close()
		return nil, errors.Join(statErr, leafErr)
	}
	if !after.Mode().IsRegular() || !leaf.Mode().IsRegular() || after.Size() > maxBytes || leaf.Size() > maxBytes || !os.SameFile(before, after) || !os.SameFile(after, leaf) {
		_ = file.Close()
		return nil, fmt.Errorf("dump identity, type or size changed while opening")
	}
	return file, nil
}
