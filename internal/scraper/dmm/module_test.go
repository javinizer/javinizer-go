package dmm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
)

func TestOptions(t *testing.T) {
	m := &scraperModule{}
	options := m.Options().([]any)

	assert.Len(t, options, 4, "should have 4 options")

	optionMap := make(map[string]contracts.ScraperOption)
	for _, opt := range options {
		so := opt.(contracts.ScraperOption)
		optionMap[so.Key] = so
	}

	t.Run("placeholder_threshold option exists", func(t *testing.T) {
		opt, exists := optionMap["placeholder_threshold"]
		assert.True(t, exists, "placeholder_threshold option should exist")
		assert.Equal(t, "Placeholder Threshold", opt.Label)
		assert.Equal(t, "number", opt.Type)
		assert.Equal(t, 10, opt.Default)
		assert.NotNil(t, opt.Min)
		assert.Equal(t, 1, *opt.Min)
		assert.NotNil(t, opt.Max)
		assert.Equal(t, 1000, *opt.Max)
		assert.Equal(t, "KB", opt.Unit)
	})

	t.Run("extra_placeholder_hashes option exists", func(t *testing.T) {
		opt, exists := optionMap["extra_placeholder_hashes"]
		assert.True(t, exists, "extra_placeholder_hashes option should exist")
		assert.Equal(t, "Extra Placeholder Hashes", opt.Label)
		assert.Equal(t, "string", opt.Type)
	})

	t.Run("use_browser option still exists", func(t *testing.T) {
		_, exists := optionMap["use_browser"]
		assert.True(t, exists, "use_browser option should still exist")
	})

	t.Run("scrape_actress option still exists", func(t *testing.T) {
		_, exists := optionMap["scrape_actress"]
		assert.True(t, exists, "scrape_actress option should still exist")
	})
}
