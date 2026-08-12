package worker

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestSyncCov_PartialCandidatesError(t *testing.T) {
	inner := errors.New("partial failure")
	e := partialCandidatesError{err: inner}
	assert.Equal(t, "partial failure", e.Error())
	assert.Equal(t, inner, e.Unwrap())
}

func TestSyncCov_CacheMatchesCanonical(t *testing.T) {
	match := models.ActressInfo{DMMID: 42, JapaneseName: "\xe6\xb5\x8b\xe8\xaf\x95"}
	actress := &models.Actress{DMMID: 42, JapaneseName: "\xe6\x97\xa7\xe5\x90\x8d", Aliases: " | \xe6\xb5\x8b\xe8\xaf\x95 |"}
	assert.True(t, cacheMatchesCanonical(match, actress))

	match2 := models.ActressInfo{DMMID: 99}
	assert.False(t, cacheMatchesCanonical(match2, actress))
}

func TestSyncCov_ActressInfoFields(t *testing.T) {
	info := models.ActressInfo{DMMID: 1, FirstName: "Test", LastName: "User", JapaneseName: "\xe3\x83\x86\xe3\x82\xb9\xe3\x83\x88", ThumbURL: "http://example.com/thumb.jpg"}
	fields := actressInfoFields(info)
	assert.NotEmpty(t, fields)

	empty := models.ActressInfo{}
	emptyFields := actressInfoFields(empty)
	assert.Empty(t, emptyFields)
}

func TestSyncCov_FilterActressResolverFields(t *testing.T) {
	info := models.ActressInfo{DMMID: 1, FirstName: "Test", ThumbURL: "http://example.com/thumb.jpg"}
	scraper := &actressSyncScraper{name: "dmm"}
	filtered := filterActressResolverFields(scraper, info)
	assert.Equal(t, 1, filtered.DMMID)
}

func TestSyncCov_ActressThumbnailNeedsResolution(t *testing.T) {
	assert.True(t, actressThumbnailNeedsResolution(""))
	assert.True(t, actressThumbnailNeedsResolution("https://pics.dmm.co.jp/mono/noimage/now_printing.jpg"))
	assert.False(t, actressThumbnailNeedsResolution("https://example.com/valid.jpg"))
}
