package httpclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCovFinal_BuildDefaultTimeoutFallback(t *testing.T) {
	b := newScraperClientBuilder()
	b.config.timeout = 0
	b.config.timeoutExplicit = false
	b.config.retryCount = 0
	b.config.retryCountExplicit = false
	client, err := b.BuildClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}
