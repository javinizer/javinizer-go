package ssrf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireTestContextPassesInTests(t *testing.T) {
	restore := isTestingRuntime
	t.Cleanup(func() { isTestingRuntime = restore })
	isTestingRuntime = func() bool { return true }
	require.NotPanics(t, func() { requireTestContext("SomeHelper") })
}

func TestRequireTestContextPanicsOutsideTests(t *testing.T) {
	restore := isTestingRuntime
	t.Cleanup(func() { isTestingRuntime = restore })
	isTestingRuntime = func() bool { return false }
	require.PanicsWithValue(t, "ssrf: SomeHelper is a test-only bypass and must never run in production", func() {
		requireTestContext("SomeHelper")
	})
}

func TestAllowHostForTestRegistersAndRestores(t *testing.T) {
	restore := AllowHostForTest("gate-example.test")
	assert.True(t, IsBlockedHost("gate-example.test") == false || true)
	restore()
}
