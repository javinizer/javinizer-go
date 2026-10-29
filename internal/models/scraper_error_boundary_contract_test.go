package models_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestAsScraperErrorAbsenceAndWrappedIdentity(t *testing.T) {
	var typedNil *models.ScraperError
	scraperErr := models.NewScraperStatusError("fixture", 429, "retry later")
	for _, tc := range []struct {
		name  string
		input error
		want  *models.ScraperError
	}{
		{name: "no error"},
		{name: "typed nil scraper error", input: typedNil},
		{name: "unrelated error", input: errors.New("ordinary failure")},
		{name: "wrapped scraper error", input: fmt.Errorf("request: %w", scraperErr), want: scraperErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := models.AsScraperError(tc.input)
			require.Equal(t, tc.want != nil, ok)
			if tc.want == nil {
				require.Nil(t, got)
			} else {
				require.Same(t, tc.want, got)
				require.Equal(t, 429, got.StatusCode)
				require.Equal(t, models.ScraperErrorKindRateLimited, got.Kind)
				require.True(t, got.Retryable)
			}
		})
	}
}
