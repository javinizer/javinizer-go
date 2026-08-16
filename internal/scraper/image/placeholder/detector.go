package placeholder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strconv"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

func isPlaceholder(ctx context.Context, client *resty.Client, url string, cfg Config) (bool, error) {
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

	resp, headErr := client.R().SetContext(ctx).Head(url)
	if headErr == nil && resp.StatusCode() == http.StatusNotFound {
		logging.Debugf("placeholder: 404 response for %s, treating as missing", url)
		return false, nil
	}
	if headErr == nil && resp.StatusCode() < http.StatusBadRequest {
		contentLengthStr := resp.Header().Get("Content-Length")
		logging.Debugf("placeholder: HEAD check for %s: Content-Length=%s, threshold=%d", url, contentLengthStr, cfg.Threshold)
		if contentLengthStr != "" {
			contentLength, parseErr := strconv.ParseInt(contentLengthStr, 10, 64)
			if parseErr == nil && contentLength >= cfg.Threshold {
				return false, nil
			}
		}
	} else if headErr != nil {
		logging.Debugf("placeholder: HEAD check failed for %s, falling back to GET: %v", url, headErr)
	} else {
		logging.Debugf("placeholder: HEAD returned status %d for %s, falling back to GET", resp.StatusCode(), url)
	}

	httpClient := client.GetClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("download request creation failed: %w", err)
	}

	for k, vv := range client.Header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	downloadResp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = downloadResp.Body.Close() }()

	if downloadResp.StatusCode == http.StatusNotFound {
		logging.Debugf("placeholder: 404 response on download for %s, treating as missing", url)
		return false, nil
	}

	if downloadResp.StatusCode >= 400 {
		return false, models.NewScraperStatusError("placeholder", downloadResp.StatusCode, "download failed")
	}

	hasher := sha256.New()
	bodySize, err := streamAndHash(io.LimitReader(downloadResp.Body, cfg.Threshold), hasher)
	if err != nil {
		return false, fmt.Errorf("streaming hash failed: %w", err)
	}

	logging.Debugf("placeholder: Streamed %s: size=%d", url, bodySize)

	if bodySize >= cfg.Threshold {
		return false, nil
	}

	hashStr := hex.EncodeToString(hasher.Sum(nil))

	if hashSet[hashStr] {
		logging.Debugf("placeholder: Placeholder detected for %s via hash match: %s", url, hashStr)
		return true, nil
	}

	return false, nil
}

func streamAndHash(r io.Reader, h hash.Hash) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			total += int64(n)
			if _, writeErr := h.Write(buf[:n]); writeErr != nil {
				return total, writeErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return total, readErr
		}
	}
	return total, nil
}

// FilterURLs returns the URLs that are not placeholder images, along with the count of placeholders removed.
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
		isPlaceholder, err := isPlaceholder(ctx, client, url, cfg)
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
