package aggregator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestFoldRemasterDisplayID(t *testing.T) {
	cases := map[string]string{
		"RCT-156-HD": "RCT-156H",
		"RCT-156_HD": "RCT-156H",
		"RCT-156 HD": "RCT-156H",
		"DV-818-AI":  "DV-818AI",
		"DV-899AI":   "DV-899AI",
		"RCT-156H":   "RCT-156H",
		"RCT-156":    "RCT-156",
		"IPX-535":    "IPX-535",
		"IPX-00535Z": "IPX-00535Z",
	}
	for in, want := range cases {
		assert.Equal(t, want, foldRemasterDisplayID(in), in)
	}
}

func TestAggregate_FoldsRemasterDisplayID(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev", "dmm"}},
		Metadata: config.MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"r18dev", "dmm"}}},
	}
	a := newAggregatorNoDB(testConfigFromAppConfig(cfg))
	require.NotNil(t, a)
	results := []*models.ScraperResult{
		{
			Source:    "r18dev",
			ID:        "RCT-156-HD",
			ContentID: "1rct00156h",
			Title:     "Remaster",
		},
	}
	movie, _, err := a.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)
	assert.Equal(t, "RCT-156H", movie.ID)
	assert.Equal(t, "1rct00156h", movie.ContentID)
}
