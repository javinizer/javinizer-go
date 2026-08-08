package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressIdentityNamesLastOnly(t *testing.T) {
	names := actressIdentityNames(&models.Actress{LastName: "Takamori"})
	require.Contains(t, names, "Takamori")
	firstOnly := actressIdentityNames(&models.Actress{FirstName: "Mona"})
	require.Contains(t, firstOnly, "Mona")
}

func TestFilterActressResolverFieldsEachArms(t *testing.T) {
	stub := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "javdb"}}
	_ = stub
	// resolver advertising ONLY first_name drops everything else.
	r := &cappedFieldsResolver{actressSyncScraper: actressSyncScraper{name: "x"}, fields: []string{"actress_first_name"}}
	got := filterActressResolverFields(r, models.ActressInfo{JapaneseName: "jp", FirstName: "f", LastName: "l", ThumbURL: "u"})
	require.Empty(t, got.JapaneseName)
	require.Empty(t, got.LastName)
	require.Empty(t, got.ThumbURL)
	require.Equal(t, "f", got.FirstName)
}

func TestAppendDedupLowerDuplicateSkips(t *testing.T) {
	names := []string{"Mona Hashimoto"}
	assert.Len(t, appendDedupLower(names, "Mona Hashimoto"), 1)
	assert.Len(t, appendDedupLower(names, "Aki Takamori"), 2)
}

// Non-blank fields skip resolution entirely (resolveField early return).
func TestResolveActressInfoByRankNonBlanks(t *testing.T) {
	matches := rankActressMatches(
		models.ActressInfo{DMMID: 1, FirstName: "lower"},
		models.ActressInfo{DMMID: 1, FirstName: "higher"},
	)
	matches[0].rank, matches[1].rank = 5, 1
	candidate, conflict := resolveActressInfo(&models.Actress{DMMID: 1, FirstName: "preset", ThumbURL: "thanks-no"}, matches, true)
	require.False(t, conflict)
	assert.Equal(t, "", candidate.FirstName, "non-blank fields are not re-resolved")
	assert.Equal(t, "", candidate.ThumbURL)
}

func TestResolveByRankSecondMatchImprovesBest(t *testing.T) {
	matches := rankActressMatches(
		models.ActressInfo{DMMID: 1, JapaneseName: "一"},
		models.ActressInfo{DMMID: 1, JapaneseName: "二"},
	)
	matches[0].rank, matches[1].rank = 1, 0
	candidate, conflict := resolveActressInfo(&models.Actress{DMMID: 1}, matches, true)
	require.False(t, conflict)
	assert.Equal(t, "二", candidate.JapaneseName, "higher rank wins even when it shows up later")
}

func TestResolveByRankThumbBestRankTracks(t *testing.T) {
	matches := rankActressMatches(
		models.ActressInfo{DMMID: 1, ThumbURL: "thumb-low"},
		models.ActressInfo{DMMID: 1, ThumbURL: "thumb-high"},
	)
	matches[0].rank, matches[1].rank = 9, 2
	candidate, _ := resolveActressInfo(&models.Actress{DMMID: 1, ThumbURL: ""}, matches, true)
	assert.Equal(t, "thumb-high", candidate.ThumbURL)
}

func TestRestrictScrapersSkipsNilEntries(t *testing.T) {
	dmm := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "dmm"}}
	got := restrictScrapersByPriorityNames([]models.Scraper{nil, dmm}, []string{"dmm"})
	require.Len(t, got, 1)
	require.Same(t, dmm, got[0])
	require.Empty(t, restrictScrapersByPriorityNames([]models.Scraper{nil, dmm}, []string{"other"}))
}

func TestRestrictScrapersByPriorityNamesEmptyKeysIgnored(t *testing.T) {
	dmm := &namedMetadataResolver{actressSyncScraper: actressSyncScraper{name: "dmm"}}
	got := restrictScrapersByPriorityNames([]models.Scraper{dmm}, []string{"", "  ", "dmm"})
	require.Len(t, got, 1)
}
