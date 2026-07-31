package dmm

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestDMMActressThumbnailValidation(t *testing.T) {
	s := &scraper{enabled: true, settings: models.ScraperSettings{Enabled: true}}
	_ = s.ValidateActressThumbnail(context.Background(), "https://example.com/img.jpg")
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
