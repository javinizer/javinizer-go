package contracts

import "time"

const iso8601Format = "2006-01-02T15:04:05Z07:00"

// FormatTime formats a time.Time as an ISO 8601 string.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(iso8601Format)
}

// FormatTimePtr formats a *time.Time as an *string in ISO 8601, returning nil for nil input.
func FormatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(iso8601Format)
	return &s
}
