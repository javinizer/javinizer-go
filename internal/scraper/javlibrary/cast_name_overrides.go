package javlibrary

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// A strict attribute-token subset keeps quoted contents together and rejects
// malformed/unrecognized syntax instead of letting an HTML parser repair it.
var castAttributeToken = regexp.MustCompile("^[ \t\r\n\f]+([A-Za-z_:][A-Za-z0-9_.:-]*)(?:[ \t\r\n\f]*=[ \t\r\n\f]*(?:\"([^\"]*)\"|'([^']*)'|([^ \t\r\n\f\"'`<>]+)))?")

// castNameOverride only consumes metadata already present on the detail page.
// No profile fetch, catalog lookup, or name/nationality inference is performed.
func (s *scraper) castNameOverride(anchorAttributes, name string) (models.ActressInfo, bool) {
	if len(s.settings.ActressNameOverrides) == 0 {
		return models.ActressInfo{}, false
	}
	id := castSourceID(anchorAttributes)
	if id == "" {
		return models.ActressInfo{}, false
	}
	rule, ok := s.settings.ActressNameOverrides[id]
	if !ok || !rule.Valid() || strings.Join(strings.Fields(rule.SourceName), " ") != strings.Join(strings.Fields(html.UnescapeString(name)), " ") {
		return models.ActressInfo{}, false
	}
	return models.ActressInfo{
		FirstName: strings.Join(strings.Fields(rule.FirstName), " "),
		LastName:  strings.Join(strings.Fields(rule.LastName), " "),
	}, true
}

// castSourceID fails closed on malformed or ambiguous source links.
func castSourceID(anchorAttributes string) string {
	// HTML parsers (including the current tokenizer) discard duplicate
	// attributes. Inspect raw tokens before trusting a performer identity.
	var href string
	hrefs := 0
	for strings.TrimSpace(anchorAttributes) != "" {
		match := castAttributeToken.FindStringSubmatch(anchorAttributes)
		if match == nil {
			return ""
		}
		if strings.EqualFold(match[1], "href") {
			href = html.UnescapeString(match[2] + match[3] + match[4])
			hrefs++
		}
		anchorAttributes = anchorAttributes[len(match[0]):]
	}
	if hrefs != 1 {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil || u.Host != "" || u.Scheme != "" || u.Fragment != "" || u.User != nil {
		return ""
	}
	// Accept the source's relative performer link, plus its localized root form.
	path := strings.TrimPrefix(u.Path, "/")
	if path != "vl_star.php" {
		parts := strings.Split(path, "/")
		if len(parts) != 2 || !isValidLanguage(parts[0]) || parts[1] != "vl_star.php" {
			return ""
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["s"]) != 1 {
		return ""
	}
	return query.Get("s")
}
