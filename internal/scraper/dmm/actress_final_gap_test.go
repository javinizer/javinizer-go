package dmm

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCovFinal_ResolveActressMetadataZeroDMMID(t *testing.T) {
	s := &scraper{}
	info, err := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 0})
	assert.NoError(t, err)
	assert.Equal(t, 0, info.DMMID)
}
