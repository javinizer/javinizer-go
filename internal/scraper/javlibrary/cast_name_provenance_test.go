package javlibrary

import (
	"encoding/json"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCastNameProvenanceStatesAndCompatibility(t *testing.T) {
	html := `<span class="star"><a href="vl_star.php?s=aa6sc">Yorimoto Shiori</a></span><span class="star"><a href="vl_star.php?s=jnka">Jenna Jameson</a></span><span class="star"><a href="vl_star.php?s=native">&#x5c71;田</a></span><span class="star"><a href="vl_star.php?s=single">RION</a></span><span class="star"><a href="vl_star.php?s=multi">Mary Jane Smith</a></span><span class="star"><a href="vl_star.php?s=other" href="vl_star.php?s=bad">Mary &amp; Jane</a></span>`
	for _, overridden := range []bool{false, true} {
		s := &scraper{}
		if overridden {
			s.settings = nameOverrideSettings()
		}
		result, err := s.parseDetailPage(html, "TEST", "https://example.invalid/", "en")
		require.NoError(t, err)
		state := models.CastNameUnknownOrder
		first, last := "Yorimoto", "Shiori"
		if overridden {
			state = models.CastNameUserCorrected
			first, last = "Shiori", "Yorimoto"
		}
		require.Equal(t, models.ActressInfo{FirstName: first, LastName: last}, result.Actresses[0])
		require.Equal(t, []models.CastNameProvenance{
			{Source: "javlibrary", SourceID: "aa6sc", DisplayName: "Yorimoto Shiori", State: state},
			{Source: "javlibrary", SourceID: "jnka", DisplayName: "Jenna Jameson", State: models.CastNameUnknownOrder},
			{Source: "javlibrary", SourceID: "native", DisplayName: "山田", State: models.CastNameNativeUnsplit},
			{Source: "javlibrary", SourceID: "single", DisplayName: "RION", State: models.CastNameSingleName},
			{Source: "javlibrary", SourceID: "multi", DisplayName: "Mary Jane Smith", State: models.CastNameUnknownOrder},
			{Source: "javlibrary", DisplayName: "Mary & Jane", State: models.CastNameUnknownOrder},
		}, result.CastNameProvenance)
		data, err := json.Marshal(result)
		require.NoError(t, err)
		var decoded models.ScraperResult
		require.NoError(t, json.Unmarshal(data, &decoded))
		require.Equal(t, result.Actresses, decoded.Actresses)
		require.Equal(t, result.CastNameProvenance, decoded.CastNameProvenance)
	}
}

func TestCastNameProvenanceStaleGuardReturnsUnknown(t *testing.T) {
	s := &scraper{settings: nameOverrideSettings()}
	result, err := s.parseDetailPage(`<span class="star"><a href="vl_star.php?s=aa6sc">Yorimoto New</a></span>`, "TEST", "https://example.invalid/", "en")
	require.NoError(t, err)
	require.Equal(t, models.CastNameUnknownOrder, result.CastNameProvenance[0].State)
	require.Equal(t, "Yorimoto New", result.CastNameProvenance[0].DisplayName)
	require.Equal(t, models.ActressInfo{FirstName: "Yorimoto", LastName: "New"}, result.Actresses[0])
	require.Nil(t, castNameProvenance(nil, nil))
	require.Nil(t, castNameProvenance([]models.ActressInfo{{FirstName: "Test"}}, nil))
}
