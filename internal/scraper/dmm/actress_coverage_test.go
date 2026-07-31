package dmm

import (
	"context"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestDMMActressThumbnailValidation(t *testing.T) {
	s := &scraper{enabled: true, settings: models.ScraperSettings{Enabled: true}}
	_ = s.ValidateActressThumbnail(context.Background(), "https://example.com/img.jpg")
	s.client = resty.New()
	_ = s.ValidateActressThumbnail(context.Background(), "http://127.0.0.1/img.jpg")
	s.settings.UserAgent = "custom-agent"
	_ = s.ValidateActressThumbnail(context.Background(), "http://127.0.0.1/img.jpg")
}

func TestDMMResolveActressThumbnailNil(t *testing.T) {
	defer func() { _ = recover() }()
	var s *scraper
	_ = s.ResolveActressThumbnail(context.Background(), models.ActressInfo{DMMID: 1})
}

func TestDMMResolveActressMetadataNil(t *testing.T) {
	defer func() { _ = recover() }()
	var s *scraper
	_ = s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 1})
}

func TestDMMFetchActressMetadataDoc(t *testing.T) {
	defer func() { _ = recover() }()
	s := &scraper{enabled: true, settings: models.ScraperSettings{Enabled: true}}
	_ = s.fetchActressMetadataDoc(context.Background(), 1)
}
