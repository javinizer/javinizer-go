package update

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type canceledContextChecker struct {
	calls int
}

func (c *canceledContextChecker) CheckLatestVersion(ctx context.Context) (*versionInfo, error) {
	c.calls++
	return nil, ctx.Err()
}

func TestService_ForceCheck_CanceledContextReturnsUsableErrorState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update_cache.json")
	checker := &canceledContextChecker{}
	service := NewServiceWithOptions(UpdateConfig{Enabled: true}, ServiceOptions{
		Checker: checker, StatePath: path,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	state, err := service.ForceCheck(ctx)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, 1, checker.calls)
	require.Equal(t, UpdateSourceError, state.Source)
	require.Equal(t, context.Canceled.Error(), state.Error)
	require.Empty(t, state.Version)
	require.Empty(t, state.CheckedAt)
	require.False(t, state.Available)
	require.NoFileExists(t, path)
}
