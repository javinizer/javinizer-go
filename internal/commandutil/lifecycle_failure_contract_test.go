package commandutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoreDepsNilCloseSupportsFailedBootstrapCleanup(t *testing.T) {
	// Callers may defer cleanup before construction succeeds. The absent
	// dependency graph owns no resources, so repeated cleanup is harmless.
	var deps *CoreDeps
	require.NoError(t, deps.Close())
	require.NoError(t, deps.Close())
}
