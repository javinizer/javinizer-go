package core

import "net/url"

// EffectiveOriginPort returns an explicit port, the default for HTTP or HTTPS,
// or an empty string if the URL's scheme has no default in this API. The
// caller remains responsible for parsing and checking the origin itself.
func EffectiveOriginPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch u.Scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
