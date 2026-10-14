package httpclient

import (
	"github.com/javinizer/javinizer-go/internal/config"
)

// StandardHTMLHeaders returns the default HTTP headers for browser-like HTML requests.
func StandardHTMLHeaders() map[string]string {
	return map[string]string{
		acceptHeader:                "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		acceptLanguageHeader:        "en-US,en;q=0.9",
		acceptEncodingHeader:        acceptedEncodings,
		connectionHeader:            keepAliveConnection,
		"Upgrade-Insecure-Requests": "1",
	}
}

// JSONAPIHeaders returns HTTP headers for JSON API requests.
func JSONAPIHeaders() map[string]string {
	return map[string]string{
		acceptHeader:         "application/json, text/plain, */*",
		acceptLanguageHeader: "en-US,en;q=0.9",
		acceptEncodingHeader: acceptedEncodings,
		connectionHeader:     keepAliveConnection,
	}
}

// RefererHeader returns a header map that sets the Referer to the given URL.
func RefererHeader(url string) map[string]string {
	return map[string]string{
		"Referer": url,
	}
}

// DMMHeaders returns HTTP headers for DMM requests, including age-check and locale cookies.
func DMMHeaders() map[string]string {
	return map[string]string{
		acceptHeader:                "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		acceptLanguageHeader:        "en-US,en;q=0.9,ja;q=0.8",
		acceptEncodingHeader:        acceptedEncodings,
		connectionHeader:            keepAliveConnection,
		"Upgrade-Insecure-Requests": "1",
		"Cookie":                    "age_check_done=1; cklg=ja",
	}
}

// R18DevHeaders returns HTTP headers for requests to the R18 dev API.
func R18DevHeaders() map[string]string {
	return map[string]string{
		acceptHeader:         "application/json, text/plain, */*",
		acceptLanguageHeader: "en-US,en;q=0.9",
		acceptEncodingHeader: acceptedEncodings,
		connectionHeader:     keepAliveConnection,
	}
}

// UserAgentHeader returns a header map with the User-Agent resolved from the given value.
func UserAgentHeader(ua string) map[string]string {
	resolved := config.ResolveScraperUserAgent(ua)
	return map[string]string{
		"User-Agent": resolved,
	}
}

// CombineHeaders merges the given header presets into a single header map.
func CombineHeaders(presets ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, preset := range presets {
		for k, v := range preset {
			result[k] = v
		}
	}
	return result
}
