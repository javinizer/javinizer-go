package r18devdump

import (
	"context"
	"fmt"
)

// OpenContext opens a dump sidecar connection like Open, then re-probes
// readiness honoring ctx cancellation. It exists for paths that gate on an
// unknown or freshly staged file where the probe itself may block on a slow
// or flaky filesystem; the first stage after OpenContext is the only
// blocking operation beyond Open's own locally-fast ping.
func OpenContext(ctx context.Context, path string) (*Store, error) {
	s, err := Open(path)
	if err != nil {
		return nil, err
	}
	if err := s.db.PingContext(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("ping dump db: %w", err)
	}
	return s, nil
}
