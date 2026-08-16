package httpclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilderFinalGaps_ExplicitZeroTimeoutHonored(t *testing.T) {
	b := newScraperClientBuilder()
	b.Apply(withTimeout(0))
	client, err := b.BuildClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, 0, int(b.config.timeout))
	assert.True(t, b.config.timeoutExplicit)
}

func TestBuilderFinalGaps_ExplicitZeroRetryHonored(t *testing.T) {
	b := newScraperClientBuilder()
	b.Apply(withRetryCount(0))
	client, err := b.BuildClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, 0, b.config.retryCount)
	assert.True(t, b.config.retryCountExplicit)
}
