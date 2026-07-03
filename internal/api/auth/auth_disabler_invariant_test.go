package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// TestAuthManager_DoesNotSatisfyAuthDisabler is the security invariant that
// keeps the fail-closed path safe in production: the production *AuthManager
// must NOT implement authDisabler. If a future contributor adds IsDisabled()
// to *AuthManager, the middleware would silently bypass authentication for
// every request, re-introducing the exact bug this change fixes. This test
// fails loudly if that happens.
func TestAuthManager_DoesNotSatisfyAuthDisabler(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	_, ok := any(manager).(authDisabler)
	assert.False(t, ok, "*AuthManager must not satisfy authDisabler; doing so would silently bypass authentication in production")

	_, ok = any(testkit.NoOpAuth{}).(authDisabler)
	assert.True(t, ok, "testkit.NoOpAuth must satisfy authDisabler so the test pass-through works")
}
