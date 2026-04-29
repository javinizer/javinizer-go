package dmm

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

func validateBrowserURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("browser URL is required")
	}

	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid browser URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("invalid browser URL scheme: %s", parsedURL.Scheme)
	}

	if parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return fmt.Errorf("invalid browser URL: missing host")
	}

	if port := parsedURL.Port(); port != "" {
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			return fmt.Errorf("invalid browser URL port: %s", port)
		}
	}

	return nil
}

// isRunningInContainer detects if we're running inside a Docker container
func isRunningInContainer() bool {
	// Check for /.dockerenv file (Docker specific)
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Check for container environment variable (set in Dockerfile)
	if os.Getenv("CHROME_BIN") != "" || os.Getenv("CHROME_PATH") != "" {
		return true
	}

	// Check /proc/1/cgroup for docker/containerd
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		content := string(data)
		if strings.Contains(content, "docker") || strings.Contains(content, "containerd") {
			return true
		}
	}

	return false
}

// FetchWithBrowser fetches a URL using Chrome browser automation with age verification cookies
func FetchWithBrowser(parentCtx context.Context, url string, timeout int, proxyProfile *config.ProxyProfile) (string, error) {
	if timeout <= 0 {
		timeout = 30 // Default timeout
	}

	if err := validateBrowserURL(url); err != nil {
		return "", err
	}

	logging.Debugf("DMM Browser: Starting browser for %s (timeout: %ds)", url, timeout)

	// Create allocator options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("disable-extensions", true),
	)

	// Check if running in Docker container
	// Chrome's sandbox doesn't work in containers due to namespace restrictions
	// Trade-off: We run as non-root user, and the container itself provides isolation
	if isRunningInContainer() {
		logging.Debug("DMM Browser: Detected container environment, disabling Chrome sandbox and crashpad")
		opts = append(opts,
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-setuid-sandbox", true),
			chromedp.Flag("disable-crashpad", true),
		)
	}

	// Only override Chrome binary path if explicitly set via environment variable
	// This allows chromedp to use its built-in discovery on dev machines while
	// respecting the container's CHROME_BIN setting in production
	chromeBin := os.Getenv("CHROME_BIN")
	if chromeBin == "" {
		chromeBin = os.Getenv("CHROME_PATH")
	}
	if chromeBin != "" {
		logging.Debugf("DMM Browser: Using explicit Chrome binary: %s", chromeBin)
		opts = append(opts, chromedp.ExecPath(chromeBin))
	}

	// Add proxy if configured (ProxyProfile has URL, no Enabled field)
	if proxyProfile != nil && proxyProfile.URL != "" {
		proxyURL := proxyProfile.URL
		if proxyProfile.Username != "" && proxyProfile.Password != "" {
			if idx := strings.Index(proxyURL, "://"); idx != -1 {
				proxyURL = proxyURL[:idx+3] + proxyProfile.Username + ":" + proxyProfile.Password + "@" + proxyURL[idx+3:]
			} else {
				logging.Warnf("DMM Browser: Cannot embed proxy credentials — URL missing scheme: %s", proxyURL[:strings.IndexByte(proxyURL, ':')+1])
			}
		}
		logging.Debugf("DMM Browser: Using proxy (credentials embedded if configured)")
		opts = append(opts, chromedp.ProxyServer(proxyURL))
	}

	// Create context with custom allocator
	allocCtx, allocCancel := chromedp.NewExecAllocator(parentCtx, opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Set timeout
	ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	var bodyHTML string

	// Prepare actions list
	actions := []chromedp.Action{
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Set cookies for age verification
			logging.Debug("DMM Browser: Setting age verification cookies")
			expr := network.SetCookie("age_check_done", "1").
				WithDomain(".dmm.co.jp")
			if err := expr.Do(ctx); err != nil {
				return fmt.Errorf("failed to set age_check_done cookie: %w", err)
			}
			expr = network.SetCookie("cklg", "ja").
				WithDomain(".dmm.co.jp")
			if err := expr.Do(ctx); err != nil {
				return fmt.Errorf("failed to set cklg cookie: %w", err)
			}
			return nil
		}),
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Sleep(3 * time.Second), // Wait for JavaScript rendering
		chromedp.InnerHTML("body", &bodyHTML),
	}

	// Run chromedp actions
	err := chromedp.Run(ctx, actions...)
	if err != nil {
		return "", fmt.Errorf("browser automation failed: %w", err)
	}

	logging.Debugf("DMM Browser: ✓ Successfully fetched %d characters", len(bodyHTML))
	return bodyHTML, nil
}
