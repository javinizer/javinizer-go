package ssrf

import (
	"net"
	"net/http"
	"testing"
	"weak"
)

// requireTestContext is the tripwire keeping these bypasses test-only even
// though the file compiles into production binaries (some helpers are used
// across package boundaries, so build tags cannot move them to _test files).
// A production caller -- or a test helper forgotten outside a live test --
// panics loudly instead of silently disabling SSRF validation.
// isTestingRuntime is the indirection point: testing.Testing() can never
// return false inside a test binary, so coverage for the panic arm needs a
// stubbed gate.
var isTestingRuntime = testing.Testing

func requireTestContext(helper string) {
	if !isTestingRuntime() {
		panic("ssrf: " + helper + " is a test-only bypass and must never run in production")
	}
}

// SetLookupIPForTest overrides the package's IP lookup function for the duration of a test, returning a restore function.
func SetLookupIPForTest(fn func(string) ([]net.IP, error)) func() {
	requireTestContext("SetLookupIPForTest")
	return setLookupIPForTest(fn)
}

// AllowHostForTest bypasses the SSRF blocklist for one exact hostname (e.g.
// a loopback httptest listener). Panics outside a live test binary.
func AllowHostForTest(host string) func() {
	requireTestContext("AllowHostForTest")
	key := normalizeHost(host)
	testAllowedHosts.Store(key, struct{}{})
	return func() { testAllowedHosts.Delete(key) }
}

// RemoteDNSHasWeakEntryForTest reports whether a previously-seen weak key
// still has a registry entry. Test scaffolding for GC-driven cleanup.
func RemoteDNSHasWeakEntryForTest(wp weak.Pointer[http.Transport]) bool {
	requireTestContext("RemoteDNSHasWeakEntryForTest")
	remoteDNSRegistry.Lock()
	defer remoteDNSRegistry.Unlock()
	_, ok := remoteDNSRegistry.set[wp]
	return ok
}
