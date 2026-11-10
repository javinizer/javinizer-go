package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestWithRescrapeStatusPreservesEmptyOutcome(t *testing.T) {
	calls := 0
	outcome, err := withRescrapeStatus(rescrapeLifecycle{}, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		calls++
		require.NotNil(t, scope)
		return nil, nil, nil
	})
	require.NoError(t, err)
	require.Nil(t, outcome)
	require.Equal(t, 1, calls)
}
