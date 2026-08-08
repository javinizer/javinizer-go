package minnanoav

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestRejectsPrivateBaseURLs(t *testing.T) {
	for _, url := range []string{
		"http://127.0.0.1/", "http://localhost/", "http://169.254.169.254/",
		"http://10.0.0.1/", "http://192.168.0.1/", "http://[::1]/",
	} {
		err := validateScraperSettings(&models.ScraperSettings{BaseURL: url})
		require.Error(t, err, url+" should be rejected")
	}
	require.NoError(t, validateScraperSettings(&models.ScraperSettings{BaseURL: "https://www.minnano-av.com/"}))
}
