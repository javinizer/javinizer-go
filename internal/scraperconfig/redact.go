package scraperconfig

import "net/url"

// RedactedValue is the placeholder used for sensitive fields in redacted output.
const RedactedValue = "•••••"

// Redact returns a copy of the ProxyConfig with sensitive profile credentials redacted.
func (pc ProxyConfig) Redact() ProxyConfig {
	pc.Profiles = redactProxyProfiles(pc.Profiles)
	return pc
}

// Redact returns a copy of the ProxyProfile with username, password, and URL-embedded credentials redacted.
func (p ProxyProfile) Redact() ProxyProfile {
	p.Username = redactString(p.Username)
	p.Password = redactString(p.Password)
	p.URL = redactURLCredentials(p.URL)
	return p
}

// redactString returns RedactedValue for non-empty strings, empty string otherwise.
func redactString(s string) string {
	if s == "" {
		return ""
	}
	return RedactedValue
}

// redactURLCredentials redacts user info embedded in a URL string.
// If the URL cannot be parsed, the entire string is redacted as a safety measure.
func redactURLCredentials(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	// Treat unparseable URLs AND URLs without a scheme/authority as unsafe:
	// url.Parse happily parses bare strings like "user:pass@host" into
	// u.User without populating Scheme/Host, so without this guard such a
	// value would round-trip with its credentials intact instead of being
	// redacted. Redact the whole string whenever there is no real authority.
	if err != nil || u.Scheme == "" || u.Host == "" {
		return redactString(rawURL)
	}
	if u.User != nil {
		u.User = url.UserPassword(RedactedValue, RedactedValue)
	}
	return u.String()
}

// redactProxyProfiles returns a copy of the profiles map with credentials redacted.
func redactProxyProfiles(profiles map[string]ProxyProfile) map[string]ProxyProfile {
	if profiles == nil {
		return nil
	}
	result := make(map[string]ProxyProfile, len(profiles))
	for k, v := range profiles {
		result[k] = v.Redact()
	}
	return result
}
