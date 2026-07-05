//go:build darwin || linux

package updater

import "strings"

// shellQuote wraps s in single quotes for safe use in a sh -c body, escaping
// embedded single quotes so paths containing spaces or shell metacharacters
// are passed through verbatim.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
