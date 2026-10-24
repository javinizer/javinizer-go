//go:build !darwin && !linux

package artifactstore

import (
	"fmt"
	"os"
)

// Writing fails closed until native lock/replacement/directory-sync contracts
// have been verified for this platform. Reading remains bounded and validated.
func privateDirectory(os.FileInfo) error { return nil }
func syncDirectory(*os.Root) error {
	return fmt.Errorf("directory durability unsupported on this platform")
}
func replaceManifest(*os.Root, string, string) error {
	return fmt.Errorf("atomic publication unsupported on this platform")
}
func (s *Store) lock() (*os.File, error) {
	return nil, fmt.Errorf("artifact publication unsupported on this platform")
}

func lockHandle(*os.File) error { return fmt.Errorf("native writer lock unsupported on this platform") }
