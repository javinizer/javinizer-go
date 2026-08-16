package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestHelperGaps_ActressIdentityNames(t *testing.T) {
	assert.Nil(t, actressIdentityNames(nil))
	a := &models.Actress{JapaneseName: "test", Aliases: "a|b"}
	names := actressIdentityNames(a)
	assert.Contains(t, names, "test")
	assert.Contains(t, names, "a")
}

func TestHelperGaps_CanMergeMissingDMMActress(t *testing.T) {
	assert.False(t, canMergeMissingDMMActress(nil, nil))
	assert.False(t, canMergeMissingDMMActress(&models.Actress{DMMID: 1}, &models.Actress{DMMID: 2}))
	assert.False(t, canMergeMissingDMMActress(&models.Actress{DMMID: 0}, &models.Actress{DMMID: 0}))
	assert.True(t, canMergeMissingDMMActress(&models.Actress{DMMID: 0, JapaneseName: "test"}, &models.Actress{DMMID: 5, JapaneseName: "test"}))
}

func TestHelperGaps_ActressMetadataVerified(t *testing.T) {
	assert.False(t, actressMetadataVerified(nil, nil))
	actress := &models.Actress{DMMID: 1, JapaneseName: "test"}
	matches := []rankedActressMatch{{info: models.ActressInfo{DMMID: 1, JapaneseName: "test"}}}
	assert.True(t, actressMetadataVerified(actress, matches))

	matches2 := []rankedActressMatch{{info: models.ActressInfo{DMMID: 99, JapaneseName: "test"}}}
	assert.False(t, actressMetadataVerified(actress, matches2))

	actress2 := &models.Actress{DMMID: 1, FirstName: "First", LastName: "Last"}
	matches3 := []rankedActressMatch{{info: models.ActressInfo{DMMID: 1, FirstName: "First", LastName: "Last"}}}
	assert.True(t, actressMetadataVerified(actress2, matches3))

	actress3 := &models.Actress{DMMID: 1, ThumbURL: "http://example.com/thumb.jpg"}
	matches4 := []rankedActressMatch{{info: models.ActressInfo{DMMID: 1, ThumbURL: "http://example.com/thumb.jpg"}}}
	assert.True(t, actressMetadataVerified(actress3, matches4))
}

func TestHelperGaps_ActressNeedsMetadata(t *testing.T) {
	assert.True(t, actressNeedsMetadata(nil))
	assert.True(t, actressNeedsMetadata(&models.Actress{ThumbURL: ""}))
	assert.True(t, actressNeedsMetadata(&models.Actress{ThumbURL: "http://valid.jpg", JapaneseName: ""}))
	assert.False(t, actressNeedsMetadata(&models.Actress{ThumbURL: "http://valid.jpg", JapaneseName: "name", FirstName: "F", LastName: "L"}))
}
