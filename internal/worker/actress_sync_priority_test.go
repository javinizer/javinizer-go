package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

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
