package dmm

import (
	"context"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraper/image/placeholder"
)

var DefaultPlaceholderHashes = placeholder.DefaultDMMPlaceholderHashes

const DefaultPlaceholderThresholdKB = placeholder.DefaultThresholdKB

const (
	ConfigKeyPlaceholderThreshold   = placeholder.ConfigKeyThreshold
	ConfigKeyExtraPlaceholderHashes = placeholder.ConfigKeyHashes
)

func GetPlaceholderThreshold(settings *config.ScraperSettings) int {
	cfg := placeholder.ConfigFromSettings(settings, DefaultPlaceholderHashes)
	return int(cfg.Threshold / 1024)
}

func GetExtraPlaceholderHashes(settings *config.ScraperSettings) []string {
	cfg := placeholder.ConfigFromSettings(settings, DefaultPlaceholderHashes)
	if len(cfg.Hashes) <= len(DefaultPlaceholderHashes) {
		return nil
	}
	return cfg.Hashes[len(DefaultPlaceholderHashes):]
}

func MergePlaceholderHashes(settings *config.ScraperSettings) []string {
	cfg := placeholder.ConfigFromSettings(settings, DefaultPlaceholderHashes)
	return cfg.Hashes
}

func IsPlaceholder(ctx context.Context, client *resty.Client, url string, thresholdBytes int64, hashes []string) (bool, error) {
	cfg := placeholder.Config{
		Enabled:   true,
		Threshold: thresholdBytes,
		Hashes:    hashes,
	}
	return placeholder.IsPlaceholder(ctx, client, url, cfg)
}
