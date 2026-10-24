package javlibrary

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/stretchr/testify/require"
)

func TestCastNameEntitiesDecodeOnce(t *testing.T) {
	for _, tc := range []struct {
		name, source, display string
		want                  models.ActressInfo
	}{
		{"hex native", "&#x5c71;&#x7530; &#x82b1;&#x5b50;", "山田 花子", models.ActressInfo{JapaneseName: "山田 花子"}},
		{"decimal kana", "&#12373;&#12367;&#12425;", "さくら", models.ActressInfo{JapaneseName: "さくら"}},
		{"mixed scripts", "Mary &#x82b1;", "Mary 花", models.ActressInfo{JapaneseName: "Mary 花"}},
		{"named entities", "Mary &amp; Jane Doe", "Mary & Jane Doe", models.ActressInfo{FirstName: "Mary", LastName: "& Jane Doe"}},
		{"single token", "RI&#79;N", "RION", models.ActressInfo{FirstName: "RION"}},
		{"nonbreaking whitespace", "&nbsp;Mary&nbsp;Jane Smith&nbsp;", "Mary\u00a0Jane Smith", models.ActressInfo{FirstName: "Mary", LastName: "Jane Smith"}},
		{"literal numeric entity", "&amp;#x5c71; Name", "&#x5c71; Name", models.ActressInfo{FirstName: "&#x5c71;", LastName: "Name"}},
		{"literal named entity", "Mary &amp;amp; Jane", "Mary &amp; Jane", models.ActressInfo{FirstName: "Mary", LastName: "&amp; Jane"}},
		{"unknown entity", "Mary &unknown; Jane", "Mary &unknown; Jane", models.ActressInfo{FirstName: "Mary", LastName: "&unknown; Jane"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actors, evidence := (&scraper{}).extractActressesWithEvidence(`<span class="star"><a href="vl_star.php?s=fixture">` + tc.source + `</a></span>`)
			require.Equal(t, []models.ActressInfo{tc.want}, actors)
			require.Equal(t, []models.CastNameEvidence{{SourceID: "fixture", DisplayName: tc.display}}, evidence)
		})
	}
	actors, evidence := (&scraper{}).extractActressesWithEvidence(`<span class="star"><a>&nbsp; </a></span>`)
	require.Empty(t, actors)
	require.Empty(t, evidence)
}

func TestCastNameEntitiesDeduplicateDecodedLabels(t *testing.T) {
	s := &scraper{}
	actors, evidence := s.extractActressesWithEvidence(`<span class="star"><a href="vl_star.php?s=native">&#x5c71;田</a></span><span class="star"><a href="vl_star.php?s=native">山田</a></span><span class="star"><a href="vl_star.php?s=roman">Mary &amp; Jane</a></span><span class="star"><a href="vl_star.php?s=roman">Mary & Jane</a></span>`)
	require.Equal(t, []models.ActressInfo{{JapaneseName: "山田"}, {FirstName: "Mary", LastName: "& Jane"}}, actors)
	require.Equal(t, []models.CastNameEvidence{{SourceID: "native", DisplayName: "山田"}, {SourceID: "roman", DisplayName: "Mary & Jane"}}, evidence)
}

func TestCastNameEntityOverrideUsesSameDecodedLabel(t *testing.T) {
	s := &scraper{settings: nameOverrideSettings()}
	for _, tc := range []struct {
		source, guard string
		applied       bool
	}{
		{"Yorimoto &#83;hiori", "Yorimoto Shiori", true},
		{"Yorimoto &amp;#83;hiori", "Yorimoto Shiori", false},
		{"Yorimoto &amp;#83;hiori", "Yorimoto &#83;hiori", true},
		{"Yorimoto &#83;hiori", "Yorimoto Other", false},
	} {
		t.Run(tc.source+"/"+tc.guard, func(t *testing.T) {
			parts := []string{"Shiori", "Yorimoto"}
			if tc.guard == "Yorimoto &#83;hiori" {
				parts[0] = "&#83;hiori"
			}
			s.settings.ActressNameOverrides["aa6sc"] = scraperconfig.ActressNameOverride{SourceName: tc.guard, FirstName: parts[0], LastName: parts[1]}
			actors, evidence := s.extractActressesWithEvidence(`<span class="star"><a href="vl_star.php?s=aa6sc">` + tc.source + `</a></span>`)
			require.Len(t, actors, 1)
			require.Equal(t, tc.applied, evidence[0].ManualOverride)
			if tc.applied {
				require.Equal(t, models.ActressInfo{FirstName: parts[0], LastName: parts[1]}, actors[0])
			} else {
				require.Equal(t, "Yorimoto", actors[0].FirstName)
			}
		})
	}
}
