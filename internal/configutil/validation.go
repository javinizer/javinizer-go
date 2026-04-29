package configutil

import (
	"fmt"
	"net/url"
	"strings"
)

func ValidateRequestDelay(path string, delay int) error {
	if delay < 0 {
		return fmt.Errorf("%s.request_delay must be non-negative", path)
	}
	return nil
}

func ValidateHTTPBaseURL(path, raw string) error {
	if raw == "" {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("%s must be a valid HTTP or HTTPS URL", path)
	}
	_, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL: %w", path, err)
	}
	return nil
}
