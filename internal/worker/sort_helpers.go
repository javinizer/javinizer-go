package worker

import (
	"strconv"
	"strings"
)

// suffixOrder returns a sort key for multipart suffix strings.
// Empty suffix gets 100 (sorted last), single uppercase letters get 0-25,
// numeric suffixes (with optional "pt" prefix) get 10+n, and
// unrecognized strings get 50.
// Note: Only ASCII A-Z/a-z letters are recognized as suffix characters.
// Unicode letters are treated as unrecognized (sort key 50).
//
//nolint:unused
func suffixOrder(s string) int {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return 100
	}
	if len(s) == 1 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) {
		c := s[0]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		return int(c - 'A')
	}
	if strings.HasPrefix(s, "pt") {
		if n, err := strconv.Atoi(s[2:]); err == nil {
			return 10 + n
		}
	}
	if n, err := strconv.Atoi(s); err == nil {
		return 10 + n
	}
	return 50
}
