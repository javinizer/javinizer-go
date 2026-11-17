package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveExePathDiscoveryFailureDoesNotSelectTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "javinizer")
	original := []byte("installed executable remains intact")
	require.NoError(t, os.WriteFile(target, original, 0600))
	discoveryErr := errors.New("running executable unavailable")
	calls := 0
	got, err := resolveExePathWithExecutable("", func() (string, error) {
		calls++
		return target, discoveryErr
	})
	require.ErrorIs(t, err, discoveryErr)
	require.Empty(t, got, "a path returned alongside an error must never become an upgrade target")
	require.Equal(t, 1, calls)
	contents, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Equal(t, original, contents)
}

func TestResolveExePathExplicitOverrideBypassesDiscovery(t *testing.T) {
	target := filepath.Join(t.TempDir(), "javinizer")
	require.NoError(t, os.WriteFile(target, []byte("installed executable"), 0600))
	got, err := resolveExePathWithExecutable(target, func() (string, error) {
		t.Fatal("explicit target must not discover the running executable")
		return "", errors.New("unreachable")
	})
	require.NoError(t, err)
	resolved, resolveErr := filepath.EvalSymlinks(target)
	require.NoError(t, resolveErr)
	require.Equal(t, resolved, got)
}
