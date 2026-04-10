package placeholder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/logging"
)

func IsPlaceholder(ctx context.Context, client *resty.Client, url string, cfg Config) (bool, error) {
	if url == "" {
		return false, fmt.Errorf("empty URL")
	}

	if !cfg.Enabled {
		return false, nil
	}

	hashSet := make(map[string]bool, len(cfg.Hashes))
	for _, h := range cfg.Hashes {
		hashSet[h] = true
	}

	resp, err := client.R().SetContext(ctx).Head(url)
	if err != nil {
		return false, fmt.Errorf("HEAD request failed: %w", err)
	}

	if resp.StatusCode() == 404 {
		logging.Debugf("placeholder: 404 response for %s, treating as missing", url)
		return false, nil
	}

	if resp.StatusCode() >= 400 {
		return false, fmt.Errorf("HTTP error %d", resp.StatusCode())
	}

	contentLengthStr := resp.Header().Get("Content-Length")
	logging.Debugf("placeholder: HEAD check for %s: Content-Length=%s, threshold=%d", url, contentLengthStr, cfg.Threshold)

	if contentLengthStr != "" {
		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64)
		if err == nil && contentLength >= cfg.Threshold {
			return false, nil
		}
	}

	downloadResp, err := client.R().SetContext(ctx).Get(url)
	if err != nil {
		return false, fmt.Errorf("download failed: %w", err)
	}

	if downloadResp.StatusCode() == 404 {
		logging.Debugf("placeholder: 404 response on download for %s, treating as missing", url)
		return false, nil
	}

	if downloadResp.StatusCode() >= 400 {
		return false, fmt.Errorf("HTTP error %d on download", downloadResp.StatusCode())
	}

	body := downloadResp.Body()
	bodySize := int64(len(body))
	logging.Debugf("placeholder: Downloaded %s: size=%d", url, bodySize)

	if bodySize >= cfg.Threshold {
		return false, nil
	}

	hash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(hash[:])

	if hashSet[hashStr] {
		logging.Debugf("placeholder: Placeholder detected for %s via hash match: %s", url, hashStr)
		return true, nil
	}

	return false, nil
}

func FilterURLs(ctx context.Context, client *resty.Client, urls []string, cfg Config) ([]string, int, error) {
	if len(urls) == 0 {
		return urls, 0, nil
	}

	if !cfg.Enabled {
		return urls, 0, nil
	}

	filtered := make([]string, 0, len(urls))
	filteredCount := 0

	for _, url := range urls {
		isPlaceholder, err := IsPlaceholder(ctx, client, url, cfg)
		if err != nil {
			logging.Warnf("placeholder: check failed for %s: %v", url, err)
			filtered = append(filtered, url)
			continue
		}

		if isPlaceholder {
			filteredCount++
		} else {
			filtered = append(filtered, url)
		}
	}

	if filteredCount > 0 {
		logging.Debugf("placeholder: Filtered %d placeholder screenshots", filteredCount)
	}

	return filtered, filteredCount, nil
}
