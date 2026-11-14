package scraperutil

import (
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// CleanString collapses whitespace and replaces non-breaking spaces with regular spaces.
func CleanString(v string) string {
	v = strings.ReplaceAll(v, "\u00a0", " ")
	v = strings.TrimSpace(v)
	v = strings.Join(strings.Fields(v), " ")
	return v
}

// NormalizeLanguage maps a language code to one of the supported canonical codes: ja, en, or zh.
func NormalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "ja":
		return "ja"
	case "en":
		return "en"
	case "zh", "cn", "tw":
		return "zh"
	default:
		return "en"
	}
}

var dateFormats = []string{
	"2006-01-02",
	"2006/01/02",
	"2006.01.02",
	"01-02-2006",
}

// ParseDate parses s against a set of common date formats and returns the first match.
func ParseDate(s string) *time.Time {
	s = CleanString(s)
	for _, f := range dateFormats {
		if t, err := time.Parse(f, s); err == nil {
			return &t
		}
	}
	return nil
}

// IntPtr returns a pointer to the given int value.
func IntPtr(i int) *int { return &i }

// BoolPtr returns a pointer to the given bool value.
func BoolPtr(b bool) *bool { return &b }

var nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizeID lowercases v and removes non-alphanumeric characters.
func NormalizeID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	return nonAlphaNumRegex.ReplaceAllString(v, "")
}

// HasJapanese reports whether v contains any Hiragana, Katakana, or Han characters.
func HasJapanese(v string) bool {
	for _, r := range v {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			return true
		}
	}
	return false
}

// IsHTTPURL reports whether v parses as an http or https URL with a host.
func IsHTTPURL(v string) bool {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// ResolveURL resolves raw against base into an absolute URL, handling protocol-relative and absolute inputs.
func ResolveURL(base, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		return raw
	}

	if strings.HasPrefix(raw, "/") {
		baseURL.Path = raw
		baseURL.RawQuery = ""
		baseURL.Fragment = ""
		return baseURL.String()
	}

	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return baseURL.ResolveReference(ref).String()
}
