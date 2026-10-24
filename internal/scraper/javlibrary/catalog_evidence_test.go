package javlibrary

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCatalogNameEvidencePreservesRawContract(t *testing.T) {
	html, err := os.ReadFile("testdata/cast_name_overrides.html")
	require.NoError(t, err)
	for _, overridden := range []bool{false, true} {
		s := &scraper{}
		if overridden {
			s.settings = nameOverrideSettings()
		}
		result, err := s.parseDetailPage(string(html), "HJMO-732", "https://www.javlibrary.com/en/javme3q25q.html", "en")
		require.NoError(t, err)
		require.Len(t, result.CastNameEvidence, len(result.Actresses))
		require.Equal(t, models.CastNameEvidence{SourceID: "aa6sc", DisplayName: "Yorimoto Shiori", ManualOverride: overridden}, result.CastNameEvidence[0])
		require.Equal(t, models.CastNameEvidence{SourceID: "jnka", DisplayName: "Jenna Jameson"}, result.CastNameEvidence[1])
		before, err := json.Marshal(result)
		require.NoError(t, err)
		clone := result.Clone()
		clone.CastNameEvidence[0].SourceID = "changed"
		require.Equal(t, "aa6sc", result.CastNameEvidence[0].SourceID)
		clone.CastNameEvidence = nil
		after, err := json.Marshal(clone)
		require.NoError(t, err)
		require.JSONEq(t, string(before), string(after), "evidence never changes raw JSON")
	}
}

func TestCatalogEvidenceRejectsAmbiguousSourceLinks(t *testing.T) {
	s := &scraper{}
	actors, evidence := s.extractActressesWithEvidence(`<span class="star"><a href="vl_star.php?s=aa6sc" href="vl_star.php?s=other">Yorimoto Shiori</a></span>`)
	require.Equal(t, []models.ActressInfo{{FirstName: "Yorimoto", LastName: "Shiori"}}, actors)
	require.Equal(t, []models.CastNameEvidence{{DisplayName: "Yorimoto Shiori"}}, evidence)
	actors, evidence = s.extractActressesWithEvidence(`<span class="star"><a href="vl_star.php?s=native">&#x5c71;&#x7530; &#x82b1;&#x5b50;</a></span>`)
	require.Equal(t, "山田 花子", evidence[0].DisplayName)
	require.Equal(t, "山田 花子", actors[0].JapaneseName)
	actors, evidence = s.extractActressesWithEvidence(`<span class="star"><a href="vl_star.php?s=compound">Mary &amp; Jane Doe</a></span>`)
	require.Equal(t, "Mary & Jane Doe", evidence[0].DisplayName)
	require.Equal(t, "& Jane Doe", actors[0].LastName)
}

// extractActresses keeps existing parser-focused tests scoped to public cast fields.
func (s *scraper) extractActresses(html string) []models.ActressInfo {
	actresses, _ := s.extractActressesWithEvidence(html)
	return actresses
}
