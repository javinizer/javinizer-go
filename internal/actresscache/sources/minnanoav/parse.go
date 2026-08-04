package minnanoavsource

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	xhtml "golang.org/x/net/html"
)

// Profile is the parsed minnano-av actress page, owned by the cache builder.
// Phase 3's scraper package reuses this export; there is no reverse import.
type Profile struct {
	DMMID        int
	FirstName    string
	LastName     string
	JapaneseName string
	Aliases      []string
	ThumbURL     string
}

// ParseProfile parses a minnano-av actress page. sourceURL is the page URL,
// used to resolve relative og:image thumbnails.
func ParseProfile(body []byte, sourceURL string) (Profile, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return Profile{}, err
	}
	page := parseActressPage(doc, sourceURL)
	profile := Profile{
		DMMID:        parseDMMActressID(doc),
		JapaneseName: page.primaryName,
		ThumbURL:     stripQuery(page.thumbURL),
		Aliases:      make([]string, 0, len(page.aliases)),
	}
	if firstName, lastName, ok := splitRomajiName(page.romaji); ok {
		profile.FirstName = firstName
		profile.LastName = lastName
	}
	seen := make(map[string]struct{}, len(page.aliases))
	for _, alias := range page.aliases {
		aliasName := strings.TrimSpace(alias.japanese)
		if aliasName == "" || aliasName == profile.JapaneseName {
			continue
		}
		if _, exists := seen[aliasName]; exists {
			continue
		}
		seen[aliasName] = struct{}{}
		profile.Aliases = append(profile.Aliases, aliasName)
	}
	return profile, nil
}

var dmmActressIDPattern = regexp.MustCompile("(?:^|[?&])actress=([0-9]+)")

func parseDMMActressID(doc *goquery.Document) int {
	if doc == nil {
		return 0
	}
	id := 0
	doc.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		href := html.UnescapeString(strings.TrimSpace(link.AttrOr("href", "")))
		for range 3 {
			decoded, err := url.QueryUnescape(href)
			if err != nil || decoded == href {
				break
			}
			href = decoded
		}
		match := dmmActressIDPattern.FindStringSubmatch(href)
		if len(match) != 2 {
			return true
		}
		id, _ = strconv.Atoi(match[1])
		return false
	})
	return id
}

type actressPage struct {
	primaryName string
	reading     string
	romaji      string
	aliases     []aliasEntry
	thumbURL    string
}

type aliasEntry struct {
	japanese string
	reading  string
	romaji   string
}

func parseActressPage(doc *goquery.Document, sourceURL string) actressPage {
	page := actressPage{}
	if doc == nil {
		return page
	}
	doc.Find("h1").First().Each(func(_ int, h1 *goquery.Selection) {
		page.primaryName = scraperutil.CleanString(directText(h1))
		reading, romaji := parseReadingRomaji(scraperutil.CleanString(h1.Find("span").Text()))
		page.reading = reading
		page.romaji = romaji
	})
	if page.primaryName == "" {
		h2 := doc.Find(".act-profile h2").First()
		if h2.Length() > 0 {
			name, reading, romaji := parseNameEntry(scraperutil.CleanString(h2.Text()))
			page.primaryName = name
			page.reading = reading
			page.romaji = romaji
		}
	}
	doc.Find(".act-profile tr").Each(func(_ int, tr *goquery.Selection) {
		label := scraperutil.CleanString(tr.Find("span").Text())
		value := scraperutil.CleanString(tr.Find("p").Text())
		if label != "別名" || value == "" {
			return
		}
		jp, reading, romaji := parseNameEntry(value)
		if jp == "" {
			return
		}
		page.aliases = append(page.aliases, aliasEntry{japanese: jp, reading: reading, romaji: romaji})
	})
	if thumb := doc.Find(`meta[property="og:image"]`).First(); thumb.Length() > 0 {
		if src := strings.TrimSpace(thumb.AttrOr("content", "")); src != "" {
			// url.ResolveReference resolves relative AND protocol-relative values
			// correctly, including query strings (scraperutil.ResolveURL glues a
			// query into the escaped path).
			if ref, err := url.Parse(src); err == nil {
				if base, berr := url.Parse(sourceURL); berr == nil {
					page.thumbURL = base.ResolveReference(ref).String()
				} else {
					page.thumbURL = scraperutil.ResolveURL(sourceURL, src)
				}
			} else {
				page.thumbURL = scraperutil.ResolveURL(sourceURL, src)
			}
		}
	}
	return page
}

func parseNameEntry(raw string) (japanese, reading, romaji string) {
	parts := strings.SplitN(raw, "（", 2)
	japanese = scraperutil.CleanString(parts[0])
	if len(parts) < 2 {
		return japanese, "", ""
	}
	inner := strings.TrimSuffix(strings.TrimSpace(parts[1]), "）")
	reading, romaji = parseReadingRomaji(scraperutil.CleanString(inner))
	return japanese, reading, romaji
}

func parseReadingRomaji(inner string) (reading, romaji string) {
	if inner == "" {
		return "", ""
	}
	parts := strings.SplitN(inner, "/", 2)
	reading = strings.TrimSpace(parts[0])
	if len(parts) < 2 {
		return reading, ""
	}
	romaji = strings.TrimSpace(parts[1])
	return reading, romaji
}

func splitRomajiName(romaji string) (string, string, bool) {
	romaji = strings.TrimSpace(romaji)
	if romaji == "" {
		return "", "", false
	}
	parts := strings.Fields(romaji)
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[1], parts[0], true
}

func stripQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	return u.String()
}

func directText(sel *goquery.Selection) string {
	if sel == nil || len(sel.Nodes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, node := range sel.Nodes {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == xhtml.TextNode {
				b.WriteString(child.Data)
			}
		}
	}
	return b.String()
}
