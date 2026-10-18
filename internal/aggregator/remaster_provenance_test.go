package aggregator

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSourceReviewRemasterFoldPreservesFieldAndActorProvenance(t *testing.T) {
	for _, id := range []string{"RCT-156", "RCT156HD", "RCT-156-Z-H", "DV-818-AI", "IPX-00535Z"} {
		for _, custom := range []bool{false, true} {
			a := New(&Config{Metadata: &MetadataConfig{Priority: config.PriorityConfig{Priority: []string{"high", "low"}, Fields: map[string][]string{"title": {"low"}, "description": {"high"}, "actresses": {"high"}}}}}, nil, nil, nil)
			actor := models.ActressInfo{DMMID: 42, JapaneseName: "Fixture Actor"}
			rows := []*models.ScraperResult{
				{Source: "low", ID: id, Language: "ja", Title: "low title", Description: "low description", Actresses: []models.ActressInfo{{DMMID: 99, JapaneseName: "Other Actor"}}},
				{Source: "high", ID: id, Language: "ja", Title: "high title", Description: "high description", Actresses: []models.ActressInfo{actor}},
			}
			var m *models.Movie
			var err error
			if custom {
				m, _, err = a.AggregateWithPriority(rows, []string{"high", "low"})
			} else {
				m, _, err = a.Aggregate(rows)
			}
			require.NoError(t, err)
			require.Equal(t, foldRemasterDisplayID(id), m.ID)
			require.Equal(t, "low title", m.Title)
			require.Equal(t, "high description", m.Description)
			require.Len(t, m.Translations, 1)
			require.Equal(t, "low", m.Translations[0].SourceName)
			require.Len(t, m.Actresses, 2)
			index := 0
			if m.Actresses[index].DMMID != 42 {
				index = 1
			}
			source, evidence, rank := m.Actresses[index].ScrapeEvidence()
			require.Equal(t, "high", source)
			require.Equal(t, actor, evidence)
			require.Zero(t, rank)
		}
	}
}
