package config

import (
	"fmt"
	"os"
)

func applyDefaultsPatches(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	if cfg.DefaultsVersion < 0 {
		return false
	}

	changed := false

	if cfg.DefaultsVersion < 1 {
		if cfg.Scrapers.RequestTimeoutSeconds == 60 {
			fmt.Fprintf(os.Stderr, "✓ Config default updated: scrapers.request_timeout_seconds 60 → 180 (was the previous default; set it explicitly in config.yaml to keep 60)\n")
			cfg.Scrapers.RequestTimeoutSeconds = 180
		}
		cfg.DefaultsVersion = 1
		changed = true
	}

	return changed
}
