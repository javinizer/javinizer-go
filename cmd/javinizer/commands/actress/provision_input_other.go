//go:build !darwin && !linux

package actress

import (
	"fmt"
	"os"
)

func provisionPlatformSupported() error {
	return fmt.Errorf("offline artifact publication is supported only on Linux and macOS; this platform can read an already provisioned store")
}

func openProvisionInput(string, int64) (*os.File, error) {
	return nil, provisionPlatformSupported()
}
