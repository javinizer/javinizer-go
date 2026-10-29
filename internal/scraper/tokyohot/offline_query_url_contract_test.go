package tokyohot

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestResolveSearchQueryUnderscoreProductURL(t *testing.T) {
	s := newScraper(testSettings("https://www.tokyo-hot.com"), nil, models.FlareSolverrConfig{})
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	for _, tc := range []struct {
		name, input, want string
		ok                bool
	}{
		{"underscore product", "https://www.tokyo-hot.com/product/AB_123", "AB-123", true},
		{"case and surrounding space", "  https://www.tokyo-hot.com/product/ab_123  ", "AB-123", true},
		{"short identifier", "AB_123", "AB-123", true},
		{"unrelated host", "https://example.test/product/AB_123", "", false},
		{"invalid product", "https://www.tokyo-hot.com/product/AB_invalid", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := s.ResolveSearchQuery(tc.input)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
