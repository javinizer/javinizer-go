package ssrf

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetLookupIPForTest(t *testing.T) {
	restore := SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.2.3.4")}, nil
	})
	defer restore()

	result := currentLookupIP()
	require.NotNil(t, result)
	ips, err := result("example.com")
	assert.NoError(t, err)
	assert.Len(t, ips, 1)
	assert.Equal(t, net.ParseIP("1.2.3.4"), ips[0])

	// Restore original
	restore()
	// Can't compare func values directly; just verify it doesn't panic
	assert.NotPanics(t, func() { currentLookupIP() })
}

func currentLookupIP() func(string) ([]net.IP, error) {
	lookupIPMu.RLock()
	defer lookupIPMu.RUnlock()
	if lookup := lookupIP; lookup != nil {
		return func(host string) ([]net.IP, error) { return lookup(context.Background(), host) }
	}
	return net.LookupIP
}

func TestSetLookupIPForTestNilTemporarilyRestoresDefaultResolver(t *testing.T) {
	// An IP literal is resolved locally by net.DefaultResolver, so resetting
	// the hook never depends on a real DNS server or external network access.
	const address = "203.0.113.20"
	calls := 0
	t.Cleanup(SetLookupIPForTest(func(host string) ([]net.IP, error) {
		calls++
		require.Equal(t, address, host)
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}))

	_, err := resolvePublicIPs(t.Context(), address)
	require.ErrorContains(t, err, "private/internal IP")
	require.Equal(t, 1, calls)

	t.Run("default resolver", func(t *testing.T) {
		t.Cleanup(SetLookupIPForTest(nil))
		ips, err := resolvePublicIPs(t.Context(), address)
		require.NoError(t, err)
		require.Len(t, ips, 1)
		require.True(t, ips[0].Equal(net.ParseIP(address)))
		require.Equal(t, 1, calls, "cleared hook must not be called")
	})

	// The nested cleanup must restore the original security decision, rather
	// than leave later tests silently using the process-wide default resolver.
	_, err = resolvePublicIPs(t.Context(), address)
	require.ErrorContains(t, err, "private/internal IP")
	require.Equal(t, 2, calls)
}
