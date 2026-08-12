package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressMetadataVerified_NilActress(t *testing.T) {
	assert.False(t, actressMetadataVerified(nil, nil))
}

func TestActressMetadataVerified_NoMatches(t *testing.T) {
	actress := &models.Actress{DMMID: 1}
	assert.False(t, actressMetadataVerified(actress, nil))
	assert.False(t, actressMetadataVerified(actress, rankActressMatches()))
}

func TestActressMetadataVerified_DifferentDMMID(t *testing.T) {
	actress := &models.Actress{DMMID: 1}
	matches := rankActressMatches(models.ActressInfo{DMMID: 2})
	assert.False(t, actressMetadataVerified(actress, matches))
}

func TestActressMetadataVerified_JapaneseNameMatch(t *testing.T) {
	actress := &models.Actress{DMMID: 1, JapaneseName: "テスト"}
	matches := rankActressMatches(models.ActressInfo{DMMID: 1, JapaneseName: "テスト"})
	assert.True(t, actressMetadataVerified(actress, matches))
}

func TestActressMetadataVerified_FirstLastNameMatch(t *testing.T) {
	actress := &models.Actress{DMMID: 1, FirstName: "Test", LastName: "Actor"}
	matches := rankActressMatches(models.ActressInfo{DMMID: 1, FirstName: "test", LastName: "actor"})
	assert.True(t, actressMetadataVerified(actress, matches))
}

func TestActressMetadataVerified_ThumbURLMatch(t *testing.T) {
	actress := &models.Actress{DMMID: 1, ThumbURL: "https://example.com/img.jpg"}
	matches := rankActressMatches(models.ActressInfo{DMMID: 1, ThumbURL: "https://example.com/img.jpg"})
	assert.True(t, actressMetadataVerified(actress, matches))
}

func TestActressMetadataVerified_NoFieldMatch(t *testing.T) {
	actress := &models.Actress{DMMID: 1, JapaneseName: "テスト", FirstName: "Test", LastName: "Actor"}
	matches := rankActressMatches(models.ActressInfo{DMMID: 1, JapaneseName: "別人", FirstName: "Other", LastName: "Name"})
	assert.False(t, actressMetadataVerified(actress, matches))
}

func TestActressNeedsMetadata_NilActress(t *testing.T) {
	assert.True(t, actressNeedsMetadata(nil))
}

func TestActressNeedsMetadata_EmptyFields(t *testing.T) {
	assert.True(t, actressNeedsMetadata(&models.Actress{}))
	assert.True(t, actressNeedsMetadata(&models.Actress{DMMID: 1, JapaneseName: "テスト"}))
	assert.True(t, actressNeedsMetadata(&models.Actress{DMMID: 1, FirstName: "Test", LastName: "Actor", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/invalid"}))
}

func TestActressNeedsMetadata_Complete(t *testing.T) {
	assert.False(t, actressNeedsMetadata(&models.Actress{DMMID: 1, JapaneseName: "テスト", FirstName: "Test", LastName: "Actor", ThumbURL: "https://example.com/img.jpg"}))
}

func TestActressThumbnailNeedsResolution(t *testing.T) {
	assert.True(t, actressThumbnailNeedsResolution(""))
	assert.True(t, actressThumbnailNeedsResolution("https://pics.dmm.co.jp/mono/actjpgs/invalid"))
	assert.False(t, actressThumbnailNeedsResolution("https://example.com/valid.jpg"))
}

func TestActressThumbnailSourcePriority(t *testing.T) {
	assert.Equal(t, 0, actressThumbnailSourcePriority("dmm"))
	assert.Equal(t, 1, actressThumbnailSourcePriority("minnanoav"))
	assert.Equal(t, 2, actressThumbnailSourcePriority("javdb"))
	assert.Equal(t, 3, actressThumbnailSourcePriority("unknown"))
}

func TestScraperThumbnailCanRefresh(t *testing.T) {
	assert.False(t, scraperThumbnailCanRefresh(""))
	assert.True(t, scraperThumbnailCanRefresh("https://c0.jdbstatic.com/avatars/zx/ZX.jpg"))
	assert.True(t, scraperThumbnailCanRefresh("https://pics.dmm.co.jp/test.jpg"))
	assert.True(t, scraperThumbnailCanRefresh("https://www.minnano-av.com/test.jpg"))
	assert.False(t, scraperThumbnailCanRefresh("https://example.com/valid.jpg"))
}

func TestAuthoritativeActressScrapers(t *testing.T) {
	// Test with nil interface
	result := authoritativeActressScrapers(nil, true, nil)
	assert.Empty(t, result)
}

func TestActressMetadataScrapers(t *testing.T) {
	result := actressMetadataScrapers(nil, true, nil)
	assert.Empty(t, result)
}

func TestActressIdentityNames_NilActress(t *testing.T) {
	assert.Nil(t, actressIdentityNames(nil))
}

func TestActressIdentityNames_Parentheses(t *testing.T) {
	actress := &models.Actress{JapaneseName: "名前（旧名）", Aliases: "別名,他名"}
	names := actressIdentityNames(actress)
	assert.Contains(t, names, "名前")
	assert.Contains(t, names, "旧名")
	assert.Contains(t, names, "別名")
	assert.Contains(t, names, "他名")
}

func TestIdentityNameMatches(t *testing.T) {
	names := []string{"Test", "Other"}
	assert.True(t, identityNameMatches(names, "test"))
	assert.True(t, identityNameMatches(names, "Test"))
	assert.False(t, identityNameMatches(names, "Missing"))
	assert.False(t, identityNameMatches(names, ""))
}

func TestCanMergeMissingDMMActress(t *testing.T) {
	assert.False(t, canMergeMissingDMMActress(nil, nil))
	assert.False(t, canMergeMissingDMMActress(&models.Actress{DMMID: 1}, nil))
	assert.False(t, canMergeMissingDMMActress(&models.Actress{}, &models.Actress{}))
	assert.False(t, canMergeMissingDMMActress(&models.Actress{DMMID: 0, JapaneseName: "テスト"}, &models.Actress{DMMID: 0}))
	assert.True(t, canMergeMissingDMMActress(&models.Actress{DMMID: 0, JapaneseName: "テスト"}, &models.Actress{DMMID: 1, JapaneseName: "テスト"}))
	assert.True(t, canMergeMissingDMMActress(&models.Actress{DMMID: 0, JapaneseName: "テスト", ThumbURL: "https://example.com/img.jpg"}, &models.Actress{DMMID: 1, JapaneseName: "テスト", ThumbURL: "https://example.com/img.jpg"}))
}

func TestLookupActressCache_NilActress(t *testing.T) {
	_, ok := lookupActressCache(nil, nil)
	assert.False(t, ok)
}

func TestLookupActressCache_NilLookup(t *testing.T) {
	_, ok := lookupActressCache(&models.Actress{DMMID: 1}, nil)
	assert.False(t, ok)
}

func TestNeedsLinkedActressFallback(t *testing.T) {
	assert.False(t, needsLinkedActressFallback(&models.Actress{DMMID: 1, JapaneseName: "テスト", FirstName: "Test", LastName: "Actor", ThumbURL: "https://example.com/img.jpg"}, rankActressMatches(models.ActressInfo{DMMID: 1}), false))
	assert.True(t, needsLinkedActressFallback(&models.Actress{DMMID: 1, JapaneseName: "", FirstName: "", LastName: "", ThumbURL: ""}, rankActressMatches(), false))
}

// Codex latest head: cache fields admitted as DMM-ranked source so a live
// resolver 's equal-or-better source wins when it fills a field; otherwise
// an otherwise-unenriched actress gains the field from the cache.
func TestCacheAdmissionCompetesByRank(t *testing.T) {
	db, repo, movieRepo, actress := newActressSyncFixture(t, &models.Actress{DMMID: 5501, JapaneseName: "松井"})
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	lookup := func(dmmID int, jp, first, last string) (models.ActressInfo, bool) {
		if dmmID == 5501 {
			return models.ActressInfo{DMMID: 5501, FirstName: "Emi", LastName: "Asahi", JapaneseName: "松井"}, true
		}
		return models.ActressInfo{}, false
	}
	// Actress priority: NONE (default ordering), dmm suffix preferred channel.
	result, err := SyncActressMetadata(context.Background(), actress.ID, repo, movieRepo, nil, ActressSyncOptions{
		LookupCache: lookup,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}
