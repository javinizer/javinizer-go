package dmm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

var DefaultPlaceholderHashes = []string{}

const DefaultPlaceholderThresholdKB = 10

const (
	ConfigKeyPlaceholderThreshold   = "placeholder_threshold"
	ConfigKeyExtraPlaceholderHashes = "extra_placeholder_hashes"
)

func GetPlaceholderThreshold(settings *config.ScraperSettings) int {
	if settings == nil || settings.Extra == nil {
		return DefaultPlaceholderThresholdKB
	}
	if val, ok := settings.Extra[ConfigKeyPlaceholderThreshold]; ok {
		switch v := val.(type) {
		case int:
			if v > 0 {
				return v
			}
		case float64:
			if v > 0 {
				return int(v)
			}
		}
	}
	return DefaultPlaceholderThresholdKB
}

func GetExtraPlaceholderHashes(settings *config.ScraperSettings) []string {
	if settings == nil || settings.Extra == nil {
		return nil
	}
	if val, ok := settings.Extra[ConfigKeyExtraPlaceholderHashes]; ok {
		switch v := val.(type) {
		case []string:
			result := make([]string, 0, len(v))
			for _, h := range v {
				h = strings.TrimSpace(strings.ToLower(h))
				if len(h) == 64 {
					result = append(result, h)
				}
			}
			return result
		case []interface{}:
			result := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok {
					s = strings.TrimSpace(strings.ToLower(s))
					if len(s) == 64 {
						result = append(result, s)
					}
				}
			}
			return result
		case string:
			h := strings.TrimSpace(strings.ToLower(v))
			if len(h) == 64 {
				return []string{h}
			}
		}
	}
	return nil
}

func MergePlaceholderHashes(settings *config.ScraperSettings) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)

	for _, h := range DefaultPlaceholderHashes {
		if !seen[h] {
			seen[h] = true
			result = append(result, h)
		}
	}

	for _, h := range GetExtraPlaceholderHashes(settings) {
		if !seen[h] {
			seen[h] = true
			result = append(result, h)
		}
	}

	return result
}

// IsPlaceholder checks if an image URL is a placeholder.
// Returns true if the image matches a known placeholder hash or is under the size threshold.
// Returns false for 404s, timeouts, or errors (preserves URL in result).
// Logs detection decisions at Debug level per D-18.
func IsPlaceholder(ctx context.Context, client *resty.Client, url string, thresholdBytes int64, hashes []string) (bool, error) {
	if url == "" {
		return false, fmt.Errorf("empty URL")
	}

	hashSet := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		hashSet[h] = true
	}

	// Phase 1: HEAD request for Content-Length
	resp, err := client.R().SetContext(ctx).Head(url)
	if err != nil {
		return false, fmt.Errorf("HEAD request failed: %w", err)
	}

	// Handle 404 - treat as "missing" not placeholder (per D-15)
	if resp.StatusCode() == 404 {
		logging.Debugf("DMM: 404 response for %s, treating as missing", url)
		return false, nil
	}

	if resp.StatusCode() >= 400 {
		return false, fmt.Errorf("HTTP error %d", resp.StatusCode())
	}

	contentLengthStr := resp.Header().Get("Content-Length")
	logging.Debugf("DMM: HEAD check for %s: Content-Length=%s, threshold=%d", url, contentLengthStr, thresholdBytes)

	// Phase 2: Check if Content-Length indicates large file
	if contentLengthStr != "" {
		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64)
		if err == nil && contentLength >= thresholdBytes {
			// Large file, not a placeholder (per D-12)
			return false, nil
		}
	}

	// Phase 3: Download and check size/hash
	downloadResp, err := client.R().SetContext(ctx).Get(url)
	if err != nil {
		return false, fmt.Errorf("download failed: %w", err)
	}

	if downloadResp.StatusCode() == 404 {
		logging.Debugf("DMM: 404 response on download for %s, treating as missing", url)
		return false, nil
	}

	if downloadResp.StatusCode() >= 400 {
		return false, fmt.Errorf("HTTP error %d on download", downloadResp.StatusCode())
	}

	body := downloadResp.Body()
	bodySize := int64(len(body))
	logging.Debugf("DMM: Downloaded %s: size=%d", url, bodySize)

	// Check actual size against threshold
	if bodySize >= thresholdBytes {
		return false, nil
	}

	// Phase 4: Hash computation and matching (per D-11)
	hash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(hash[:])

	if hashSet[hashStr] {
		logging.Debugf("DMM: Placeholder detected for %s via hash match: %s", url, hashStr)
		return true, nil
	}

	return false, nil
}
