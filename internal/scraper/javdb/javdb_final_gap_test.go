package javdb

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCovFinal_ResolveActressMetadataFindActorIDError(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, BaseURL: "http://localhost:1"}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.ResolveActressMetadata(context.Background(), models.ActressInfo{JapaneseName: "test", ThumbURL: ""})
	assert.Error(t, err)
}
