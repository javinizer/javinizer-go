//go:build darwin || linux

package updater

import "strings"

// shellQuote wraps s in single quotes for safe use in a sh -c body, escaping
// embedded single quotes so paths containing spaces or shell metacharacters
// are passed through verbatim.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// swapWaitMaxIters caps the detached helper's wait for the old process to
// exit. At 0.2s per iteration this is a 30s ceiling; a stuck PID bails the
// swap instead of hanging the helper forever. Shared by the darwin and linux
// helpers so the two cannot drift.
const swapWaitMaxIters = 150
