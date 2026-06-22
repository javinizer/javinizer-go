package ssrf

import (
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
