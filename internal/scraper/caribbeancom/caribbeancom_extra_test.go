package caribbeancom

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestResolveDownloadProxyForHost tests proxy resolution for Caribbeancom hosts
func TestResolveDownloadProxyForHost(t *testing.T) {
	settings := models.ScraperSettings{Enabled: true}
	scraper := newScraper(&settings, nil, models.FlareSolverrConfig{})

	tests := []struct {
		name   string
		host   string
		wantOk bool
	}{
		{
			name:   "caribbeancom.com host returns true",
			host:   "www.caribbeancom.com",
			wantOk: true,
		},
		{
			name:   "caribbeancom.com without www",
			host:   "caribbeancom.com",
			wantOk: true,
		},
		{
			name:   "non-caribbeancom host returns false",
			host:   "example.com",
			wantOk: false,
		},
		{
			name:   "empty host returns false",
			host:   "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := scraper.ResolveDownloadProxyForHost(tt.host)
			assert.Equal(t, tt.wantOk, ok)
		})
	}
}
