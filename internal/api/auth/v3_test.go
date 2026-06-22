package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSecurityConfigV3_NilDeps tests securityConfig with nil deps
func TestSecurityConfigV3_NilDeps(t *testing.T) {
	cfg := securityConfig(nil)
	assert.Nil(t, cfg)
}

// TestComputeTrustedCIDRsV3 tests computeTrustedCIDRs
func TestComputeTrustedCIDRsV3(t *testing.T) {
	cidrs := computeTrustedCIDRs(nil)
	// Should return at least the default trusted CIDRs (127.0.0.0/8, ::1/128)
	assert.NotEmpty(t, cidrs)
}

// TestIsTrustedClientV3 tests isTrustedClient
func TestIsTrustedClientV3(t *testing.T) {
	assert.True(t, isTrustedClient("127.0.0.1", nil))
	assert.True(t, isTrustedClient("::1", nil))
	assert.False(t, isTrustedClient("8.8.8.8", nil))
	assert.False(t, isTrustedClient("invalid-ip", nil))
}

// TestParseCIDRListV3 tests parseCIDRList
func TestParseCIDRListV3(t *testing.T) {
	t.Run("valid CIDRs", func(t *testing.T) {
		result := parseCIDRList("192.168.0.0/16,10.0.0.0/8")
		assert.Len(t, result, 2)
	})

	t.Run("invalid CIDR", func(t *testing.T) {
		result := parseCIDRList("invalid")
		assert.Empty(t, result)
	})

	t.Run("empty string", func(t *testing.T) {
		result := parseCIDRList("")
		assert.Empty(t, result)
	})
}

// TestNewSessionIDV3_Removed - newSessionID is a method on AuthManager, not a standalone function
