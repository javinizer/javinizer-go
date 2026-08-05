package ssrf

import (
	"net"
	"net/http"
	"weak"
)

// SetLookupIPForTest overrides the package's IP lookup function for the duration of a test, returning a restore function.
func SetLookupIPForTest(fn func(string) ([]net.IP, error)) func() {
	return setLookupIPForTest(fn)
}

// AllowHostForTest bypasses the SSRF blocklist for one exact hostname (e.g.
// a loopback httptest listener). Production code must never call this.
func AllowHostForTest(host string) func() {
	key := normalizeHost(host)
	testAllowedHosts.Store(key, struct{}{})
	return func() { testAllowedHosts.Delete(key) }
}

// RemoteDNSHasWeakEntryForTest reports whether a previously-seen weak key
// still has a registry entry. Test scaffolding for GC-driven cleanup.
func RemoteDNSHasWeakEntryForTest(wp weak.Pointer[http.Transport]) bool {
	remoteDNSRegistry.Lock()
	defer remoteDNSRegistry.Unlock()
	_, ok := remoteDNSRegistry.set[wp]
	return ok
}
