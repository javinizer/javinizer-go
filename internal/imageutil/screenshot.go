package imageutil

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	dmmImageExtRegex = regexp.MustCompile(`(?i)\.jpe?g$`)
)

// picsDMMHost is the hostname serving DMM cover/poster/screenshot images
// at their canonical (lower-resolution) URLs. awsimgsrc.dmm.com serves
// higher-resolution equivalents.
const picsDMMHost = "pics.dmm.co.jp"

// IsDMMHost returns true if the hostname belongs to a DMM-owned domain
// (dmm.co.jp, dmm.com, and their subdomains).
func IsDMMHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "dmm.co.jp" || strings.HasSuffix(host, ".dmm.co.jp") ||
		host == "dmm.com" || strings.HasSuffix(host, ".dmm.com")
}

// NormalizeDMMScreenshotURL normalizes a DMM-hosted screenshot URL for
// consistent deduplication and higher-quality image retrieval.
//
// Applies the following transformations when the URL is on a DMM domain:
//   - Protocol-relative URLs (//...) are upgraded to https
//   - awsimgsrc.dmm.co.jp CDN paths are rewritten to pics.dmm.co.jp
//   - Query parameters and fragments are stripped
//   - Screenshot filenames missing the "jp" suffix get it inserted
//     (e.g., avsa00432-1.jpg -> avsa00432jp-1.jpg) for the larger
//     resolution version, while cover/poster URLs (pl.jpg, ps.jpg) are
//     left unchanged.
//
// Non-DMM URLs are returned unchanged.
func NormalizeDMMScreenshotURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}

	raw = strings.Replace(raw, "awsimgsrc.dmm.co.jp/pics_dig", picsDMMHost, 1)

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	if !IsDMMHost(u.Hostname()) {
		return u.String()
	}

	u.Host = strings.ToLower(u.Host)

	if strings.Contains(u.Path, "/digital/amateur/") {
		u.Path = strings.ToLower(u.Path)
	}

	u.RawQuery = ""
	u.Fragment = ""

	base := path.Base(u.Path)
	lowerBase := strings.ToLower(base)
	if dmmImageExtRegex.MatchString(lowerBase) &&
		strings.Contains(base, "-") &&
		!strings.Contains(lowerBase, "jp-") &&
		!strings.HasSuffix(lowerBase, "pl.jpg") &&
		!strings.HasSuffix(lowerBase, "ps.jpg") {
		base = strings.Replace(base, "-", "jp-", 1)
		u.Path = strings.TrimSuffix(u.Path, path.Base(u.Path)) + base
	}

	return u.String()
}

// DiscoverScreenshots probes pics.dmm.co.jp for screenshot URLs based on a cover URL pattern.
// When the r18.dev API returns an empty gallery, this fallback discovers screenshots by
// trying the standard DMM screenshot URL pattern: {base}/{content_id}jp-{N}.jpg
// It stops at the first missing image (or redirect to placeholder) and returns all found URLs.
//
// Some DMM titles have content_ids with zero-padded numbers (e.g., 118gets00081) where
// the screenshots only exist at the depadded variant (e.g., 118gets081). This function
// tries the original content_id first, then depadded variants if no screenshots are found.
//
// Returns nil if the cover URL is not a pics.dmm.co.jp digital/video URL or no screenshots are found.
func DiscoverScreenshots(coverURL string, client *http.Client) []string {
	if coverURL == "" {
		return nil
	}

	u, err := url.Parse(coverURL)
	if err != nil {
		return nil
	}

	if !IsDMMHost(u.Hostname()) {
		return nil
	}

	if u.Hostname() != picsDMMHost {
		return nil
	}

	if !strings.Contains(u.Path, "/digital/video/") {
		return nil
	}

	dir := path.Dir(u.Path)
	base := path.Base(u.Path)

	if !strings.HasSuffix(strings.ToLower(base), "pl.jpg") {
		return nil
	}

	contentID := base[:len(base)-len("pl.jpg")]
	if contentID == "" {
		return nil
	}

	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	candidates := []string{contentID}
	if depadded := depadContentID(contentID); depadded != contentID {
		candidates = append(candidates, depadded)
	}

	for _, cid := range candidates {
		cidDir := dir
		if cid != contentID {
			cidDir = strings.Replace(dir, contentID, cid, 1)
		}
		if screenshots := probeScreenshots(cidDir, cid, client); len(screenshots) > 0 {
			return screenshots
		}
	}

	return nil
}

// depadContentID removes excess zero-padding from the numeric portion of a content_id.
// DMM content_ids use 5-digit padded numbers (e.g., 118gets00081) but screenshots
// typically use 3-digit minimum padding (e.g., 118gets081). This function depads
// the number and re-pads to 3 digits minimum.
func depadContentID(contentID string) string {
	re := regexp.MustCompile(`^(\d+[a-zA-Z]+)(\d+)$`)
	m := re.FindStringSubmatch(contentID)
	if len(m) != 3 {
		return contentID
	}
	prefix := m[1]
	num, err := strconv.Atoi(m[2])
	if err != nil {
		return contentID
	}
	depadded := fmt.Sprintf("%s%03d", prefix, num)
	if depadded == contentID {
		return contentID
	}
	return depadded
}

// probeScreenshots tries jp-{1..50}.jpg for the given content_id and directory.
// Stops at the first non-200 response or redirect to a placeholder image.
func probeScreenshots(dir, contentID string, client *http.Client) []string {
	checkClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if client != nil {
		if client.Timeout > 0 {
			checkClient.Timeout = client.Timeout
		}
		if client.Transport != nil {
			checkClient.Transport = client.Transport
		}
	}

	var screenshots []string
	for i := 1; i <= 50; i++ {
		screenshotURL := fmt.Sprintf("https://pics.dmm.co.jp%s/%sjp-%d.jpg", dir, contentID, i)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, screenshotURL, nil)
		if err != nil {
			break
		}
		resp, err := checkClient.Do(req)
		if err != nil {
			break
		}
		_ = resp.Body.Close()

		if resp.StatusCode != 200 {
			break
		}

		screenshots = append(screenshots, screenshotURL)
	}
	return screenshots
}

// UpgradeCoverResolution upgrades cover image filenames to their
// highest-resolution suffix variant:
//   - ps.jpg → pl.jpg (for all URLs, including amateur)
//   - jp.jpg → pl.jpg (for non-amateur URLs only)
//
// It does NOT change the CDN host — use UpgradeDMMCoverCDN for that.
// Screenshot-style filenames (e.g., ipx00535jp-1.jpg) are left unchanged
// because the suffix check uses HasSuffix rather than Contains.
func UpgradeCoverResolution(rawURL string) string {
	if strings.HasSuffix(rawURL, "ps.jpg") {
		rawURL = rawURL[:len(rawURL)-len("ps.jpg")] + "pl.jpg"
	}
	if !strings.Contains(rawURL, "/amateur/") && strings.HasSuffix(rawURL, "jp.jpg") {
		rawURL = rawURL[:len(rawURL)-len("jp.jpg")] + "pl.jpg"
	}
	return rawURL
}

// UpgradeDMMCoverCDN rewrites a pics.dmm.co.jp cover URL to its
// higher-resolution equivalent on the awsimgsrc.dmm.com CDN (e.g.
// 800×501 → 2184×1368 for SONE-560). Returns the input unchanged for
// non-pics URLs or URLs that don't match a known DMM cover pattern.
func UpgradeDMMCoverCDN(rawURL string) string {
	if upgraded := constructAwsimgsrcURL(rawURL, "pl.jpg"); upgraded != "" {
		return upgraded
	}
	return rawURL
}
