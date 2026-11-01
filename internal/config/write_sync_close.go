package config

import (
	"fmt"
	"io"
)

type syncWriteCloser interface {
	io.Writer
	io.Closer
	Sync() error
}

// writeSyncClose takes responsibility for exactly one Close of an open handle.
// A failed write or sync remains the primary error even if closing also fails.
// Callers retain all namespace, cleanup, ownership and publication decisions.
func writeSyncClose(file syncWriteCloser, data []byte, description string) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to write %s: %w", description, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to sync %s: %w", description, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", description, err)
	}
	return nil
}
