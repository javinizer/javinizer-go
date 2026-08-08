package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func rankActressMatches(infos ...models.ActressInfo) []rankedActressMatch {
	matches := make([]rankedActressMatch, 0, len(infos))
	for _, info := range infos {
		matches = append(matches, rankedActressMatch{info: info})
	}
	return matches
}

// Sync decisions must follow the user-configured scraper priority instead of
// registry iteration order or the legacy hard-coded thumbnail order.
func TestActressSyncScraperPriorityOrdering(t *testing.T) {
	set := &fixedScraperSet{enabled: []models.Scraper{
		&actressSyncScraper{name: "dmm"},
		&actressSyncScraper{name: "javdb"},
		&actressSyncScraper{name: "r18dev"},
	}}

	got := actressMetadataScrapers(set, true, []string{"javdb", "dmm"})
	require.Len(t, got, 3)
	require.Equal(t, "javdb", got[0].Name())
	require.Equal(t, "dmm", got[1].Name())
	require.Equal(t, "r18dev", got[2].Name())

	got = actressMetadataScrapers(set, true, nil)
	require.Equal(t, "dmm", got[0].Name(), "nil priority keeps registry order")

	// Thumbnail ranking: the actress field priority beats the legacy quality
	// order; unlisted and global-only names fall back sensibly.
	require.Less(t, actressSyncThumbnailRank([]string{"javdb"}, nil)("javdb"), actressSyncThumbnailRank([]string{"javdb"}, nil)("dmm"))
	require.Less(t, actressSyncThumbnailRank(nil, nil)("dmm"), actressSyncThumbnailRank(nil, nil)("javdb"))
	require.Less(t, actressSyncThumbnailRank(nil, []string{"r18dev"})("r18dev"), actressSyncThumbnailRank(nil, []string{"r18dev"})("minnanoav"))
}

func TestResolveActressInfoPrefersConfiguredSourceDeterministically(t *testing.T) {
	matches := rankActressMatches(
		models.ActressInfo{DMMID: 1, FirstName: "low"},
		models.ActressInfo{DMMID: 1, FirstName: "high"},
	)
	matches[0].rank = 5
	matches[1].rank = 1

	candidate, conflict := resolveActressInfo(&models.Actress{DMMID: 1}, matches, true)
	require.False(t, conflict)
	require.Equal(t, "high", candidate.FirstName)

	// Same-rank disagreement stays ambiguous; classic mode keeps flagging any.
	matches[1].rank = 5
	_, conflict = resolveActressInfo(&models.Actress{DMMID: 1}, matches, true)
	require.True(t, conflict)
	_, conflict = resolveActressInfo(&models.Actress{DMMID: 1}, matches, false)
	require.True(t, conflict)
}

func TestActressSyncFieldExclusivity(t *testing.T) {
	require.True(t, actressSyncSkipSentinel([]string{"__skip__"}))
	require.False(t, actressSyncSkipSentinel([]string{"dmm"}))
	require.False(t, actressSyncSkipSentinel(nil))

	resolvers := []models.Scraper{&actressSyncScraper{name: "dmm"}, &actressSyncScraper{name: "javdb"}, &actressSyncScraper{name: "minnanoav"}}
	kept := restrictScrapersByPriorityNames(resolvers, []string{"javdb"})
	require.Len(t, kept, 1)
	require.Equal(t, "javdb", kept[0].Name())
	require.Empty(t, restrictScrapersByPriorityNames(resolvers, []string{"nonexistent"}))
}
