package minnanoav

import (
	"context"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func TestMinnanoAVScraperInterface(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30, RetryCount: 1, RateLimit: 1000}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	if s.Name() != "minnanoav" {
		t.Fatalf("expected 'minnanoav', got %q", s.Name())
	}
	if !s.IsEnabled() {
		t.Fatal("expected enabled")
	}
	if s.Config() == nil {
		t.Fatal("expected non-nil config")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	url, err := s.GetURL(context.Background(), "TEST-001")
	_ = url
	_ = err
	_ = settings
	_ = scraperutil.NewScraperRegistry()
}

func TestValidateScraperSettings(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30, RetryCount: 1, RateLimit: 1000}
	validateScraperSettings(settings)
}

func TestRegister(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	Register(registry)
}

func TestMinnanoAVValidateActressThumbnailBranches(t *testing.T) {
	var nilScraper *scraper
	_ = nilScraper.ValidateActressThumbnail(context.Background(), "http://127.0.0.1/img.jpg")
	s := &scraper{client: resty.New(), settings: models.ScraperSettings{}}
	_ = s.ValidateActressThumbnail(context.Background(), "http://127.0.0.1/img.jpg")
	s.settings.UserAgent = "custom-agent"
	_ = s.ValidateActressThumbnail(context.Background(), "http://127.0.0.1/img.jpg")
}
