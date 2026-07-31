package javdb

import (
	"context"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestJavDBValidateActressThumbnail(t *testing.T) {
	s := &scraper{enabled: true, settings: models.ScraperSettings{Enabled: true}}
	err := s.ValidateActressThumbnail(context.Background(), "https://example.com/img.jpg")
	_ = err
	s.client = resty.New()
	_ = s.ValidateActressThumbnail(context.Background(), "http://127.0.0.1/img.jpg")
	s.settings.UserAgent = "custom-agent"
	_ = s.ValidateActressThumbnail(context.Background(), "http://127.0.0.1/img.jpg")
}
