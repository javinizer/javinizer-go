package core

import (
	"github.com/javinizer/javinizer-go/internal/config"
)

// SetInitialConfigs publishes the runtime config and disk-origin snapshot as a
// single atomic unit under reloadMu. The runtime config is the env-overridden
// cfg handlers serve; the disk snapshot is the pre-env cfg persisted to disk,
// used by the config-update service for redacted-secret preservation.
func (r *APIRuntime) SetInitialConfigs(runtimeCfg, diskCfg *config.Config) {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if runtimeCfg != nil && r.deps != nil && r.deps.CoreDeps != nil {
		r.deps.CoreDeps.SetConfig(runtimeCfg)
	}
	if diskCfg != nil {
		r.diskConfigSnapshot = diskCfg.Clone()
	} else {
		r.diskConfigSnapshot = nil
	}
}

// DiskConfigSnapshot returns the cached pre-env disk config, or nil if no
// snapshot has been published yet (callers should fall back to config.Load).
func (r *APIRuntime) DiskConfigSnapshot() *config.Config {
	r.reloadMu.RLock()
	cfg := r.diskConfigSnapshot
	r.reloadMu.RUnlock()
	if cfg == nil {
		return nil
	}
	return cfg.Clone()
}
