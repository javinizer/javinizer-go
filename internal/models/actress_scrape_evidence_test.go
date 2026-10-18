package models

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
	"sync"
	"testing"
)

func TestActressScrapeEvidenceIsServerOnlyAndDetached(t *testing.T) {
	info := ActressInfo{FirstName: "Reported", JapaneseName: "報告", ThumbURL: "reported.jpg"}
	actress := Actress{FirstName: "Canonical"}
	actress.SetScrapeEvidence("first", info, 2)
	movie := &Movie{Actresses: []Actress{actress}, Credits: []MovieCredit{{Scraped: actress}}}
	clone := movie.Clone()
	clone.Actresses[0].SetScrapeEvidence("other", ActressInfo{}, 4)
	clone.Credits[0].Scraped.SetScrapeEvidence("other", ActressInfo{}, 4)
	for _, original := range []Actress{movie.Actresses[0], movie.Credits[0].Scraped} {
		source, evidence, priority := original.ScrapeEvidence()
		require.Equal(t, "first", source)
		require.Equal(t, info, evidence)
		require.Equal(t, 2, priority)
	}
	encoded, err := json.Marshal(actress)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "reported.jpg")
	var decoded Actress
	require.NoError(t, json.Unmarshal([]byte(`{"first_name":"Client","scrapeEvidence":{"source":"forged"},"scrape_evidence":{"source":"forged"}}`), &decoded))
	source, _, _ := decoded.ScrapeEvidence()
	require.Empty(t, source)
	parsed, err := schema.Parse(&Actress{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	require.NotContains(t, parsed.FieldsByName, "scrapeEvidence")
}
