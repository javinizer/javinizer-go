package httpclient

import (
	"net/url"
	"strings"
)

// ResolveMediaReferer selects a compatible Referer header for media requests.
// Priority:
// 1) Known host overrides (hotlink-protected hosts)
// 2) Configured referer fallback (if provided)
// 3) URL origin fallback
func ResolveMediaReferer(downloadURL, configuredReferer string) string {
	parsedURL, err := url.Parse(downloadURL)
	if err != nil {
		return configuredReferer
	}

	host := strings.ToLower(parsedURL.Hostname())
	switch {
	case strings.HasSuffix(host, "jdbstatic.com"), strings.HasSuffix(host, "javdb.com"):
		return "https://javdb.com/"
	case strings.HasSuffix(host, "javbus.com"), strings.HasSuffix(host, "javbus.org"):
		return "https://www.javbus.com/"
	case strings.HasSuffix(host, "aventertainments.com"):
		return "https://www.aventertainments.com/"
	case strings.HasSuffix(host, "caribbeancom.com"):
		return "https://www.caribbeancom.com/"
	case strings.HasSuffix(host, "libredmm.com"):
		return "https://www.libredmm.com/"
	case strings.HasSuffix(host, "dmm.co.jp"), strings.HasSuffix(host, "dmm.com"), strings.Contains(host, ".dmm."):
		return "https://www.dmm.co.jp/"
	}

	if configuredReferer != "" {
		return configuredReferer
	}

	if (parsedURL.Scheme == "http" || parsedURL.Scheme == "https") && parsedURL.Host != "" {
		return parsedURL.Scheme + "://" + parsedURL.Host + "/"
	}

	return ""
}
