package javlibrary

import (
	"html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
)

// castNameOverride only consumes metadata already present on the detail page.
// No profile fetch, catalog lookup, or name/nationality inference is performed.
func (s *scraper) castNameOverride(anchorAttributes, name string) (models.ActressInfo, bool) {
	if len(s.settings.ActressNameOverrides) == 0 {
		return models.ActressInfo{}, false
	}
	// The bounded anchor fragment has no nesting and strings.Reader cannot
	// return a read error. HTML parsing tolerates malformed attributes.
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader("<a" + anchorAttributes + "></a>"))
	href, _ := doc.Find("a").First().Attr("href")
	u, err := url.Parse(href)
	if err != nil || u.Host != "" || u.Scheme != "" || u.Fragment != "" || u.User != nil {
		return models.ActressInfo{}, false
	}
	// Accept the source's relative performer link, plus its localized root form.
	path := strings.TrimPrefix(u.Path, "/")
	if path != "vl_star.php" {
		parts := strings.Split(path, "/")
		if len(parts) != 2 || !isValidLanguage(parts[0]) || parts[1] != "vl_star.php" {
			return models.ActressInfo{}, false
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["s"]) != 1 {
		return models.ActressInfo{}, false
	}
	rule, ok := s.settings.ActressNameOverrides[query.Get("s")]
	if !ok || !rule.Valid() || strings.Join(strings.Fields(rule.SourceName), " ") != strings.Join(strings.Fields(html.UnescapeString(name)), " ") {
		return models.ActressInfo{}, false
	}
	return models.ActressInfo{
		FirstName: strings.Join(strings.Fields(rule.FirstName), " "),
		LastName:  strings.Join(strings.Fields(rule.LastName), " "),
	}, true
}
