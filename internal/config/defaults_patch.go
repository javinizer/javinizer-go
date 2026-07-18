package config

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
			cfg.Scrapers.RequestTimeoutSeconds = 180
		}
		cfg.DefaultsVersion = 1
		changed = true
	}

	return changed
}
