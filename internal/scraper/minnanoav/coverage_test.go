package minnanoav

import (
	"context"
	"testing"

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
