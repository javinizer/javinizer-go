package placeholder

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
)

const DefaultThresholdKB = 10

const (
	ConfigKeyThreshold = "placeholder_threshold"
	ConfigKeyHashes    = "extra_placeholder_hashes"
)

type Config struct {
	Enabled   bool
	Threshold int64
	Hashes    []string
}

func ConfigFromSettings(settings *config.ScraperSettings, defaultHashes []string) Config {
	cfg := Config{
		Enabled:   true,
		Threshold: DefaultThresholdKB * 1024,
		Hashes:    make([]string, 0),
	}

	seen := make(map[string]bool)

	for _, h := range defaultHashes {
		if !seen[h] {
			seen[h] = true
			cfg.Hashes = append(cfg.Hashes, h)
		}
	}

	if settings == nil || settings.Extra == nil {
		return cfg
	}

	if val, ok := settings.Extra[ConfigKeyThreshold]; ok {
		switch v := val.(type) {
		case int:
			if v > 0 {
				cfg.Threshold = int64(v * 1024)
			}
		case float64:
			if v > 0 {
				cfg.Threshold = int64(v * 1024)
			}
		}
	}

	if val, ok := settings.Extra[ConfigKeyHashes]; ok {
		switch v := val.(type) {
		case []string:
			for _, h := range v {
				h = strings.TrimSpace(strings.ToLower(h))
				if len(h) == 64 && !seen[h] {
					seen[h] = true
					cfg.Hashes = append(cfg.Hashes, h)
				}
			}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					s = strings.TrimSpace(strings.ToLower(s))
					if len(s) == 64 && !seen[s] {
						seen[s] = true
						cfg.Hashes = append(cfg.Hashes, s)
					}
				}
			}
		case string:
			h := strings.TrimSpace(strings.ToLower(v))
			if len(h) == 64 && !seen[h] {
				seen[h] = true
				cfg.Hashes = append(cfg.Hashes, h)
			}
		}
	}

	return cfg
}
