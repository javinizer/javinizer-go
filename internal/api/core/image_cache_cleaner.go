package core

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/worker"
)

type imageCacheCleanupOptions struct {
	interval time.Duration
}

func startImageCacheCleanup(rt *APIRuntime, opts imageCacheCleanupOptions) {
	interval := opts.interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}

	ctx := rt.ServerCtx()

	sweep := func() {
		tempCfg := rt.GetAPIConfig().TempConfig()
		ttl := time.Duration(tempCfg.ImageCacheTTLHours) * time.Hour
		if ttl <= 0 {
			return
		}
		fs := rt.Deps().GetFs()
		removed, err := worker.CleanupStaleImageCache(fs, tempCfg.TempDir, ttl)
		if err != nil {
			logging.Warnf("Image cache cleanup failed: %v", err)
		} else if removed > 0 {
			logging.Infof("Cleaned up %d expired image cache entr(ies)", removed)
		}
	}

	go sweep()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sweep()
			case <-ctx.Done():
				return
			}
		}
	}()
}
