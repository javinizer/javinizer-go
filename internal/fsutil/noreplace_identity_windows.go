//go:build windows

package fsutil

// noClobberDirIdentity has no Stat_t identity on Windows; the probe bypasses
// Windows entirely, and the empty string keeps path-only caching as the
// documented degradation.
func noClobberDirIdentity(string) string { return "" }
