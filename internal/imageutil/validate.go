package imageutil

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	_ "golang.org/x/image/webp"
)

// maxThumbnailValidationBytes ...
const maxThumbnailValidationBytes = 2 * 1024 * 1024

var validateRemoteImageWithClient = ValidateRemoteImageWithClient

// ValidateRemoteImage ...
func ValidateRemoteImage(ctx context.Context, rawURL string) error {
	if err := ssrf.CheckURL(rawURL); err != nil {
		return err
	}
	return validateRemoteImageWithClient(ctx, ssrf.NewSSRFSafeClient(30*time.Second), rawURL, config.DefaultUserAgent, httpclient.ResolveMediaReferer(rawURL, ""))
}

// ValidateRemoteImageWithSafeClient ...
func ValidateRemoteImageWithSafeClient(ctx context.Context, client *http.Client, rawURL, userAgent, referer string) error {
	if err := ssrf.CheckURL(rawURL); err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("image validator client is nil")
	}
	safeClient := *client
	if transport, ok := client.Transport.(*http.Transport); ok {
		safeClient.Transport = ssrf.WrapTransportWithSSRFCheck(transport)
	}
	previousCheckRedirect := client.CheckRedirect
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := ssrf.CheckURL(req.URL.String()); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			return previousCheckRedirect(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	return ValidateRemoteImageWithClient(ctx, &safeClient, rawURL, userAgent, referer)
}

// ValidateRemoteImageWithClient ...
func ValidateRemoteImageWithClient(ctx context.Context, client *http.Client, rawURL, userAgent, referer string) error {
	if client == nil {
		return fmt.Errorf("image validator client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = httpclient.DrainAndClose(resp.Body) }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("image source returned status %d", resp.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return fmt.Errorf("image source returned content type %q", resp.Header.Get("Content-Type"))
	}
	cfg, _, err := image.DecodeConfig(io.LimitReader(resp.Body, maxThumbnailValidationBytes))
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("image dimensions are invalid")
	}
	return nil
}
