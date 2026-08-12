package javdb

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/imageutil"
)

func (s *scraper) ValidateActressThumbnail(ctx context.Context, rawURL string) error {
	if s == nil || s.client == nil {
		return imageutil.ValidateRemoteImage(ctx, rawURL)
	}
	userAgent := s.settings.UserAgent
	if userAgent == "" {
		userAgent = config.DefaultUserAgent
	}
	return imageutil.ValidateRemoteImageWithSafeClient(ctx, s.client.GetClient(), rawURL, userAgent, httpclient.ResolveMediaReferer(rawURL, s.baseURL))
}
