package httpclient

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilderGaps_BuildClientSuccess(t *testing.T) {
	b := newScraperClientBuilder()
	client, err := b.BuildClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilderGaps_BuildWithFlareSolverrSuccess(t *testing.T) {
	b := newScraperClientBuilder()
	client, fs, err := b.BuildWithFlareSolverr()
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Nil(t, fs)
}

func TestBuilderGaps_BuildWithProxySuccess(t *testing.T) {
	b := newScraperClientBuilder()
	client, _, err := b.BuildWithProxy()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestBuilderGaps_ExplicitZeroTimeout(t *testing.T) {
	b := newScraperClientBuilder()
	b.Apply(withTimeout(0))
	assert.True(t, b.config.timeoutExplicit)
	assert.Equal(t, 0, int(b.config.timeout))
}

func TestBuilderGaps_ExplicitZeroRetryCount(t *testing.T) {
	b := newScraperClientBuilder()
	b.Apply(withRetryCount(0))
	assert.True(t, b.config.retryCountExplicit)
	assert.Equal(t, 0, b.config.retryCount)
}

func TestBuilderGaps_FromScraperSettingsExplicitZeroTimeout(t *testing.T) {
	s := &models.ScraperSettings{Timeout: 0}
	s.SetTimeoutPresence(true)
	b := FromScraperSettings(s, nil, models.FlareSolverrConfig{})
	assert.True(t, b.config.timeoutExplicit)
	assert.Equal(t, 0, int(b.config.timeout))
}

func TestBuilderGaps_FromScraperSettingsExplicitZeroRetry(t *testing.T) {
	s := &models.ScraperSettings{RetryCount: 0}
	s.SetRetryCountPresence(true)
	b := FromScraperSettings(s, nil, models.FlareSolverrConfig{})
	assert.True(t, b.config.retryCountExplicit)
	assert.Equal(t, 0, b.config.retryCount)
}

func TestFactoryGaps_NoProxyTransportError(t *testing.T) {
	client := NewRestyClientNoProxy(0, 0)
	assert.NotNil(t, client)
}

func TestFactoryGaps_FlareSolverrDisabled(t *testing.T) {
	result, err := NewRestyClientWithFlareSolverr(nil, models.FlareSolverrConfig{Enabled: false}, 0, 0)
	require.NoError(t, err)
	assert.NotNil(t, result.Client)
	assert.Nil(t, result.FlareSolverr)
}
