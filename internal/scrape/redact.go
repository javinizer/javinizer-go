package scrape

import "net/url"

// RedactURLQuery strips the query (and fragment) from a URL-shaped raw input
// before it reaches logs, failure JobEvents, or persisted provenance (security
// F2 / Phase 2 #6). Query strings may carry tokens; the redacted value keeps
// only scheme+host+path so a row is still identifiable without leaking secrets.
//
// Plain IDs (no scheme/host) round-trip unchanged — a bare string is never a
// URL fetch (security F4), and url.Parse happily attaches a RawQuery to a
// schemeless string like "ABC-?123", so the scheme guard prevents redacting
// part of a plain ID. Inputs with no query are returned unchanged.
//
// Exported so the batch rescrape log (and other manual-input log sites) can
// share the same redaction as resolveScrapeInput's parse-fail fallback.
// RedactSourceURL strips credentials and secret query parameters from a
// provenance URL while retaining the non-secret `v` identifier (the canonical
// JavLibrary detail-page param) and the path itself.
func RedactSourceURL(input string) string {
	u, err := url.Parse(input)
	if err != nil {
		return input
	}
	u.User = nil
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if k != "v" {
			q.Del(k)
		}
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	} else {
		u.RawQuery = ""
	}
	return u.String()
}

func RedactURLQuery(input string) string {
	if input == "" {
		return ""
	}
	u, err := url.Parse(input)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return input
	}
	if u.RawQuery == "" && u.Fragment == "" && u.User == nil {
		return input
	}
	// Clear userinfo (user:pass@) as well as query/fragment — a URL carrying
	// credentials must not leak them into logs or persisted identifiers.
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
