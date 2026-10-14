package javdb

import (
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures below are verbatim cast panels captured from real JavDB detail
// pages (via the Wayback Machine, since JavDB geo-blocks direct access). They
// pin the extraction behaviour against production markup rather than synthetic
// HTML: male co-stars carry a ♂ marker and must never reach the actress list.
func loadJavDBFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func extractFixtureActresses(t *testing.T, name string) []models.ActressInfo {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(loadJavDBFixture(t, name)))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return extractActresses(doc.Find(".value").First())
}

func TestExtractActressesRealJavDBCastPanels(t *testing.T) {
	cases := []struct {
		fixture string
		want    []string
		thumbs  map[string]string
	}{
		{
			fixture: "real_cast_dsvr033.html",
			want:    []string{"美谷朱里"},
			thumbs:  map[string]string{"美谷朱里": "https://c0.jdbstatic.com/avatars/gy/gyRE.jpg"},
		},
		{
			fixture: "real_cast_eyan172.html",
			want:    []string{"白石みき"},
			thumbs:  map[string]string{"白石みき": "https://c0.jdbstatic.com/avatars/qd/qDQy6.jpg"},
		},
		{
			fixture: "real_cast_avsa176.html",
			want:    []string{"佐藤あいり"},
			thumbs:  map[string]string{"佐藤あいり": "https://c0.jdbstatic.com/avatars/ga/GaG7.jpg"},
		},
		{
			fixture: "real_cast_live_eyan172.html",
			want:    []string{"白石みき"},
			thumbs:  map[string]string{"白石みき": "https://c0.jdbstatic.com/avatars/qd/qDQy6.jpg"},
		},
		{
			fixture: "real_cast_live_sone227.html",
			want:    []string{"金松季歩"},
			thumbs:  map[string]string{"金松季歩": "https://c0.jdbstatic.com/avatars/rd/RdZe8.jpg"},
		},
		{
			fixture: "real_cast_live_sone297.html",
			want:    []string{"仁藤さや香"},
			thumbs:  map[string]string{"仁藤さや香": "https://c0.jdbstatic.com/avatars/p3/p3Wye.jpg"},
		},
		{
			// Mirror variant: the female marker is missing while male markers
			// remain, which is how actresses disappeared and male actors leaked
			// into the catalog before the extraction fix.
			fixture: "real_cast_partial_markers.html",
			want:    []string{"白石みき"},
			thumbs:  map[string]string{"白石みき": "https://c0.jdbstatic.com/avatars/qd/qDQy6.jpg"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			actresses := extractFixtureActresses(t, tc.fixture)
			require.Len(t, actresses, len(tc.want), "got %v", actressNames(actresses))
			for i, want := range tc.want {
				assert.Equal(t, want, actresses[i].JapaneseName)
			}
			for _, actress := range actresses {
				assert.Equal(t, tc.thumbs[actress.JapaneseName], actress.ThumbURL, "thumb for %s", actress.JapaneseName)
			}
		})
	}
}

func TestParseDetailPageRealCastMarkupExcludesMaleActors(t *testing.T) {
	page := `<html><body>
<div class="title is-4"><strong>EYAN-172</strong> 30歳、性欲の全盛期</div>
<div class="movie-panel-info">
<div class="panel-block"><strong>番號:</strong><div class="value">EYAN-172</div></div>
` + loadJavDBFixture(t, "real_cast_eyan172.html") + `
</div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	require.NoError(t, err)

	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/00Akq", "EYAN-172")
	require.NoError(t, err)

	require.Len(t, result.Actresses, 1, "got %v", actressNames(result.Actresses))
	assert.Equal(t, "白石みき", result.Actresses[0].JapaneseName)
	assert.Equal(t, "https://c0.jdbstatic.com/avatars/qd/qDQy6.jpg", result.Actresses[0].ThumbURL)
	for _, actress := range result.Actresses {
		assert.NotEqual(t, "マッスル澤野", actress.JapaneseName)
		assert.NotEqual(t, "鮫島", actress.JapaneseName)
	}
}
