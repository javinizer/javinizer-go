package javlibrary

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func nameOverrideSettings() models.ScraperSettings {
	return models.ScraperSettings{Enabled: true, Language: "en", ActressNameOverrides: scraperconfig.ActressNameOverrides{
		"aa6sc": {SourceName: "Yorimoto Shiori", FirstName: "Shiori", LastName: "Yorimoto"},
	}}
}

func TestCastNameOverridesRawJSON(t *testing.T) {
	html, err := os.ReadFile("testdata/cast_name_overrides.html")
	require.NoError(t, err)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		require.Equal(t, "/en/vl_searchbyid.php", r.URL.Path, "no performer profile fetch")
		_, _ = w.Write(html)
	}))
	defer server.Close()
	settings := nameOverrideSettings()
	settings.BaseURL = server.URL
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	// Constructor owns a copy, so caller edits cannot silently change a running scraper.
	delete(settings.ActressNameOverrides, "aa6sc")
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(s)
	result, queryErr := scrape.NewQueryOnly(registry).QueryRaw(t.Context(), "HJMO-732", "javlibrary")
	require.Nil(t, queryErr)
	data, err := json.Marshal(result)
	require.NoError(t, err)
	var decoded models.ScraperResult
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, []models.ActressInfo{
		{FirstName: "Shiori", LastName: "Yorimoto"},
		{FirstName: "Jenna", LastName: "Jameson"},
		{JapaneseName: "依本しおり"},
		{FirstName: "RION"},
	}, decoded.Actresses)
	require.EqualValues(t, 1, requests.Load())
}

func TestCastNameOverrideGuards(t *testing.T) {
	for _, tc := range []struct {
		name, attrs, display string
		applied              bool
	}{
		{"live", ` href="vl_star.php?s=aa6sc" rel="tag"`, "Yorimoto Shiori", true},
		{"duplicate href conflicting", ` href="vl_star.php?s=aa6sc" href="vl_star.php?s=other"`, "Yorimoto Shiori", false},
		{"duplicate href reversed", ` href="vl_star.php?s=other" href="vl_star.php?s=aa6sc"`, "Yorimoto Shiori", false},
		{"duplicate href same", ` href="vl_star.php?s=aa6sc" href="vl_star.php?s=aa6sc"`, "Yorimoto Shiori", false},
		{"duplicate href case", ` href="vl_star.php?s=aa6sc" HREF="vl_star.php?s=other"`, "Yorimoto Shiori", false},
		{"uppercase href", ` HREF="vl_star.php?s=aa6sc"`, "Yorimoto Shiori", true},
		{"not anchor", `bbr href="vl_star.php?s=aa6sc"`, "Yorimoto Shiori", false},
		{"unquoted", ` href=vl_star.php?s=aa6sc rel=tag`, "Yorimoto Shiori", true},
		{"quoted href text is not attribute", ` title='href="vl_star.php?s=other"' href="vl_star.php?s=aa6sc"`, "Yorimoto Shiori", true},
		{"boolean attribute", ` disabled href="vl_star.php?s=aa6sc"`, "Yorimoto Shiori", true},
		{"malformed quote", ` href="vl_star.php?s=aa6sc`, "Yorimoto Shiori", false},
		{"single quote", ` href='vl_star.php?s=aa6sc'`, "Yorimoto Shiori", true},
		{"localized", ` href="/en/vl_star.php?s=aa6sc"`, "Yorimoto Shiori", true},
		{"whitespace", ` href="vl_star.php?s=aa6sc"`, " Yorimoto　 Shiori ", true},
		{"other ID", ` href="vl_star.php?s=other"`, "Yorimoto Shiori", false},
		{"changed name", ` href="vl_star.php?s=aa6sc"`, "Yorimoto New", false},
		{"case", ` href="vl_star.php?s=aa6sc"`, "yorimoto Shiori", false},
		{"native", ` href="vl_star.php?s=aa6sc"`, "依本しおり", false},
		{"stage", ` href="vl_star.php?s=aa6sc"`, "RION", false},
		{"absent href", "", "Yorimoto Shiori", false},
		{"invalid URL", ` href="%zz"`, "Yorimoto Shiori", false},
		{"invalid query", ` href="vl_star.php?s=%zz"`, "Yorimoto Shiori", false},
		{"external", ` href="https://other.invalid/vl_star.php?s=aa6sc"`, "Yorimoto Shiori", false},
		{"protocol relative", ` href="//other.invalid/vl_star.php?s=aa6sc"`, "Yorimoto Shiori", false},
		{"other path", ` href="video.php?s=aa6sc"`, "Yorimoto Shiori", false},
		{"wrong language", ` href="/xx/vl_star.php?s=aa6sc"`, "Yorimoto Shiori", false},
		{"extra path", ` href="/en/other/vl_star.php?s=aa6sc"`, "Yorimoto Shiori", false},
		{"fragment", ` href="vl_star.php?s=aa6sc#other"`, "Yorimoto Shiori", false},
		{"duplicate ID", ` href="vl_star.php?s=aa6sc&amp;s=other"`, "Yorimoto Shiori", false},
		{"wrong query key", ` href="vl_star.php?other=aa6sc"`, "Yorimoto Shiori", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &scraper{settings: nameOverrideSettings()}
			result, applied := s.castNameOverride(tc.attrs, tc.display)
			require.Equal(t, tc.applied, applied)
			if applied {
				require.Equal(t, models.ActressInfo{FirstName: "Shiori", LastName: "Yorimoto"}, result)
			}
		})
	}
}

func TestCastNameOverridesFallbackAndDedup(t *testing.T) {
	html, err := os.ReadFile("testdata/cast_name_overrides.html")
	require.NoError(t, err)
	original := (&scraper{}).extractActresses(string(html))
	require.Equal(t, models.ActressInfo{FirstName: "Yorimoto", LastName: "Shiori"}, original[0])
	s := &scraper{settings: nameOverrideSettings()}
	s.settings.ActressNameOverrides["aa6sc"] = scraperconfig.ActressNameOverride{SourceName: "Yorimoto Shiori", FirstName: "Other", LastName: "Person"}
	require.Equal(t, original, s.extractActresses(string(html)), "invalid programmatic settings fail closed")
	s.settings = nameOverrideSettings()
	for _, language := range []string{"en", "ja", "cn", "tw"} {
		s.language = language
		cast := s.extractActresses(string(html))
		require.Equal(t, original[1:], cast[1:])
		require.Equal(t, models.ActressInfo{FirstName: "Shiori", LastName: "Yorimoto"}, cast[0])
	}
	sameName := `<span class="star"><a href="vl_star.php?s=other">Yorimoto Shiori</a></span>`
	for _, html := range []string{string(html) + sameName, sameName + string(html)} {
		cast := s.extractActresses(html + html)
		require.Len(t, cast, 5, "configured and unconfigured homonyms are distinct, repeated anchors are not")
	}
	// Multi-part names are explicitly split, not guessed by first/last token heuristics.
	s.settings.ActressNameOverrides["compound"] = scraperconfig.ActressNameOverride{SourceName: "de Example Mary Jane", FirstName: "Mary Jane", LastName: "de Example"}
	cast := s.extractActresses(`<span class="star"><a href="vl_star.php?s=compound">de Example Mary Jane</a></span>`)
	require.Equal(t, []models.ActressInfo{{FirstName: "Mary Jane", LastName: "de Example"}}, cast)
	require.False(t, strings.Contains(cast[0].JapaneseName, "Example"))
}

func TestCastNameOverrideMatchesDisplayedEntities(t *testing.T) {
	s := &scraper{settings: nameOverrideSettings()}
	s.settings.ActressNameOverrides["compound"] = scraperconfig.ActressNameOverride{SourceName: "Mary & Jane Doe", FirstName: "Mary & Jane", LastName: "Doe"}
	cast := s.extractActresses(`<span class="star"><a href="vl_star.php?s=compound">Mary &amp; Jane Doe</a></span>`)
	require.Equal(t, []models.ActressInfo{{FirstName: "Mary & Jane", LastName: "Doe"}}, cast)
}

func TestCastNameOverridesDuplicateHrefFallback(t *testing.T) {
	s := &scraper{settings: nameOverrideSettings()}
	cast := s.extractActresses(`<span class="star"><a href="vl_star.php?s=aa6sc" HREF="vl_star.php?s=other">Yorimoto Shiori</a></span>`)
	require.Equal(t, []models.ActressInfo{{FirstName: "Yorimoto", LastName: "Shiori"}}, cast)
}
