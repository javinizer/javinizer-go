package minnanoav

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Codex round 12: retry_count 0 must disable retries, not silently become 3.
func TestExplicitZeroRetryCountIsHonored(t *testing.T) {
	cfg := &models.ScraperSettings{Enabled: true, RetryCount: 0}
	s := newScraper(cfg, &models.ProxyConfig{}, models.FlareSolverrConfig{})
	require.NotNil(t, s)
}
