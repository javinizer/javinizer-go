package dmm

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// FetchWithHeadless fetches a URL using headless Chrome with age verification cookies
func FetchWithHeadless(url string, timeout int) (string, error) {
	if timeout <= 0 {
		timeout = 30 // Default timeout
	}

	logging.Debugf("DMM Headless: Starting headless browser for %s (timeout: %ds)", url, timeout)

	// Create context
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	// Set timeout
	ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	var bodyHTML string

	// Set age verification cookies before navigating
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Set cookies for age verification
			logging.Debug("DMM Headless: Setting age verification cookies")
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
		chromedp.Sleep(3*time.Second), // Wait for JavaScript rendering
		chromedp.InnerHTML("body", &bodyHTML),
	)
	if err != nil {
		return "", fmt.Errorf("headless browser failed: %w", err)
	}

	logging.Debugf("DMM Headless: ✓ Successfully fetched %d characters", len(bodyHTML))
	return bodyHTML, nil
}
