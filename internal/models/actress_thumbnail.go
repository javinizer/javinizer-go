package models

import (
	"net/url"
	"path"
	"strings"
)

// IsKnownInvalidDMMActressThumbnail ...
func IsKnownInvalidDMMActressThumbnail(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !isDMMActressImageHost(parsed.Hostname()) {
		return false
	}

	imagePath := strings.ToLower(parsed.Path)
	if strings.HasSuffix(imagePath, "/mono/noimage/now_printing.jpg") {
		return true
	}
	if !strings.Contains(imagePath, "/mono/actjpgs/") || strings.HasSuffix(imagePath, "/") {
		return false
	}

	base := path.Base(imagePath)
	return base != "." && base != "actjpgs" && path.Ext(base) == ""
}

// isDMMActressImageHost ...
func isDMMActressImageHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "pics.dmm.co.jp", "awsimgsrc.dmm.co.jp", "awsimgsrc.dmm.com":
		return true
	default:
		return false
	}
}
