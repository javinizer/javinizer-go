package httpclient

import (
	"github.com/javinizer/javinizer-go/internal/config"
)

func StandardHTMLHeaders() map[string]string {
	return map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language":           "en-US,en;q=0.9",
		"Accept-Encoding":           "gzip, deflate",
		"Connection":                "keep-alive",
		"Upgrade-Insecure-Requests": "1",
	}
}

func JSONAPIHeaders() map[string]string {
	return map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "en-US,en;q=0.9",
		"Accept-Encoding": "gzip, deflate",
		"Connection":      "keep-alive",
	}
}

func RefererHeader(url string) map[string]string {
	return map[string]string{
		"Referer": url,
	}
}

func DMMHeaders() map[string]string {
	return map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language":           "en-US,en;q=0.9,ja;q=0.8",
		"Accept-Encoding":           "gzip, deflate",
		"Connection":                "keep-alive",
		"Upgrade-Insecure-Requests": "1",
		"Cookie":                    "age_check_done=1; cklg=ja",
	}
}

func R18DevHeaders() map[string]string {
	return map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "en-US,en;q=0.9",
		"Accept-Encoding": "gzip, deflate",
		"Connection":      "keep-alive",
	}
}

func UserAgentHeader(ua string) map[string]string {
	resolved := config.ResolveScraperUserAgent(ua)
	return map[string]string{
		"User-Agent": resolved,
	}
}

func CombineHeaders(presets ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, preset := range presets {
		for k, v := range preset {
			result[k] = v
		}
	}
	return result
}
