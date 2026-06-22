package poster

import (
	"regexp"
)

var (
	pathPattern = regexp.MustCompile(
		`/(?:tmp|var|etc|data|home|Users|dev|usr)(?:/[\w.-]+)+` +
			`|(?:[A-Za-z]:[/\\][\w.-]+(?:[/\\][\w.-]+)*)`,
	)
	urlPattern = regexp.MustCompile(`https?://[^\s]+`)
)

func stripSensitivePaths(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = urlPattern.ReplaceAllString(msg, "[url]")
	msg = pathPattern.ReplaceAllString(msg, "[path]")
	return msg
}

type sanitizedError struct {
	sanitized string
	cause     error
}

func (e *sanitizedError) Error() string {
	if e == nil {
		return ""
	}
	return e.sanitized
}

func (e *sanitizedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func sanitizedErrorFrom(err error) error {
	if err == nil {
		return nil
	}
	return &sanitizedError{
		sanitized: stripSensitivePaths(err),
		cause:     err,
	}
}
