package javdb

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestJavDBValidateActressThumbnail(t *testing.T) {
	s := &scraper{enabled: true, settings: models.ScraperSettings{Enabled: true}}
	err := s.ValidateActressThumbnail(context.Background(), "https://example.com/img.jpg")
	_ = err
}
