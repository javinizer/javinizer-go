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
		if tempCfg.ImageCacheMaxSizeMB > 0 {
			if _, evicted, verr := worker.EvictImageCacheToSize(fs, tempCfg.TempDir, int64(tempCfg.ImageCacheMaxSizeMB)<<20); verr != nil {
				logging.Warnf("Image cache size enforcement failed: %v", verr)
			} else if evicted > 0 {
				logging.Infof("Evicted %d image cache entr(ies) to enforce the %d MB quota", evicted, tempCfg.ImageCacheMaxSizeMB)
			}
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
