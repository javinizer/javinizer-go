package batch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBulkExcludeMaxMoviesConstant_Uncovered(t *testing.T) {
	assert.Equal(t, 100, bulkExcludeMaxMovies)
}

func TestBulkRescrapeConstants_Uncovered(t *testing.T) {
	assert.Equal(t, 100, bulkRescrapeMaxMovies)
	assert.Equal(t, 5, bulkRescrapeWorkers)
}
