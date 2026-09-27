package r18devdump

import (
	"strings"
	"time"
)

// ParseFilenameSourceDate extracts the dump date from a filename token using
// the strict "_dump_YYYY-MM-DD" grammar: the marker is case-sensitive, the
// date must be a valid calendar date, and it must be immediately followed by
// '.' or end-of-token (any other following character disqualifies the match).
// Returns "" when no strict date is present. Malformed dates are empty, never
// errors — a missing date is not a reason to reject an upload.
//
//	r18dotdev_dump_2026-09-20.sql.gz → 2026-09-20
//	r18dotdev_dump_2026-13-99.sql.gz → ""
//	r18dotdev_dump_2026-09-20abc     → ""
//	r18dotdev_dump_2026-09-20.foo    → 2026-09-20
func ParseFilenameSourceDate(name string) string {
	const marker = "_dump_"
	i := strings.Index(name, marker)
	if i < 0 {
		return ""
	}
	rest := name[i+len(marker):]
	if len(rest) < len("2006-01-02") {
		return ""
	}
	candidate := rest[:len("2006-01-02")]
	if len(rest) > len(candidate) && rest[len(candidate)] != '.' {
		return ""
	}
	if _, err := time.Parse("2006-01-02", candidate); err != nil {
		return ""
	}
	return candidate
}
