package scrape

import (
	"strings"
	"testing"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActressProvenanceKeyPrecedenceContract(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		id                         int
		japanese, first, last, key string
		keys                       []string
	}{
		{"all identities", 7, " 波多野結衣 ", " ＹＵＩ ", "Hatano", "dmmid:7", []string{"dmmid:7", "name:波多野結衣", "name:yui hatano", "name:hatano yui"}},
		{"Japanese before romanized", 0, " 波多野結衣 ", "Yui", "Hatano", "name:波多野結衣", []string{"name:波多野結衣", "name:yui hatano", "name:hatano yui"}},
		{"both orders", 0, "", "Yui", "Hatano", "name:yui hatano", []string{"name:yui hatano", "name:hatano yui"}},
		{"single first", 0, "", " ＹＵＩ ", "", "name:yui", []string{"name:yui"}},
		{"single last", 0, "", "", " Hatano ", "name:hatano", []string{"name:hatano"}},
		{"dedupe first occurrence", 0, "ＹＵＩ　ＨＡＴＡＮＯ", "Yui", "Hatano", "name:yui hatano", []string{"name:yui hatano", "name:hatano yui"}},
		{"identical components", 0, "yui yui", "Yui", "ＹＵＩ", "name:yui yui", []string{"name:yui yui"}},
		{"compatibility characters", 0, "", "ﬃ", "K", "name:ffi k", []string{"name:ffi k", "name:k ffi"}},
		{"combining normalization", 0, "", "E\u0301", "X", "name:é x", []string{"name:é x", "name:x é"}},
		{"negative id", -1, "", "A", "B", "name:a b", []string{"name:a b", "name:b a"}},
		{"empty", 0, "", "", "", "", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actress := models.Actress{DMMID: tc.id, JapaneseName: tc.japanese, FirstName: tc.first, LastName: tc.last}
			info := models.ActressInfo{DMMID: tc.id, JapaneseName: tc.japanese, FirstName: tc.first, LastName: tc.last}
			for i := 0; i < 10; i++ {
				require.Equal(t, tc.key, ActressSourceKey(actress))
				require.Equal(t, tc.keys, actressSourceKeysFromInfo(info))
			}
		})
	}
}

func TestActressProvenanceEmptyNormalizationContract(t *testing.T) {
	whitespace := []string{""}
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) {
			whitespace = append(whitespace, string(r))
		}
	}
	for _, first := range whitespace {
		for _, last := range whitespace {
			actress := models.Actress{JapaneseName: first + last, FirstName: first, LastName: last}
			require.Empty(t, ActressSourceKey(actress), "first=%q last=%q", first, last)
			require.Equal(t, []string{}, actressSourceKeysFromInfo(models.ActressInfo{JapaneseName: first + last, FirstName: first, LastName: last}))
		}
	}
	corpus := append(whitespace, "Yui", "ＹＵＩ", "\u0301", "\u200b", "\ufeff", "\xff", "\u00ad", "K", "ﬃ")
	for _, first := range corpus {
		for _, last := range corpus {
			forward := models.NormalizeActressNameKey(strings.TrimSpace(first + " " + last))
			reverse := models.NormalizeActressNameKey(strings.TrimSpace(last + " " + first))
			require.Equal(t, forward == "", reverse == "", "first=%q last=%q", first, last)
			want := ""
			if forward != "" {
				want = "name:" + forward
			}
			require.Equal(t, want, ActressSourceKey(models.Actress{FirstName: first, LastName: last}))
			keys := actressSourceKeysFromInfo(models.ActressInfo{FirstName: first, LastName: last})
			if forward != "" {
				require.Contains(t, keys, "name:"+forward)
				require.Contains(t, keys, "name:"+reverse)
			}
		}
	}
}

func TestActressProvenanceReverseOrderSourceMatchingContract(t *testing.T) {
	actresses := []models.Actress{{FirstName: "Yui", LastName: "Hatano"}}
	results := []*models.ScraperResult{
		{Source: "first", Actresses: []models.ActressInfo{{FirstName: "Hatano", LastName: "Yui"}}},
		{Source: "second", Actresses: []models.ActressInfo{{FirstName: "Yui", LastName: "Hatano"}}},
	}
	require.Equal(t, map[string]string{"name:yui hatano": "first"}, buildActressSourcesFromScrapeResults(results, nil, nil, actresses))
	require.Equal(t, map[string]string{"name:yui hatano": "second"}, buildActressSourcesFromScrapeResults(results, nil, []string{"second", "first"}, actresses))
}
