package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestAggregate_NilAggregator(t *testing.T) {
	var a *Aggregator
	_, _, err := a.Aggregate(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestAggregateWithPriority_NilAggregator(t *testing.T) {
	var a *Aggregator
	_, _, err := a.AggregateWithPriority(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestAggregate_NoResults(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{},
		TemplateEngine:   nil,
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	_, aggResult, err := a.Aggregate(nil)
	assert.Error(t, err)
	assert.Nil(t, aggResult)
}

func TestAggregateWithPriority_NoResults(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{},
		TemplateEngine:   nil,
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	_, _, err := a.AggregateWithPriority(nil, []string{"r18dev"})
	assert.Error(t, err)
}

func TestAggregate_SingleResult(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"mock"},
		TemplateEngine:   nil,
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{ID: "TEST-001", Title: "Test Movie", Source: "mock"},
	}

	movie, aggResult, err := a.Aggregate(results)
	assert.NoError(t, err)
	assert.NotNil(t, movie)
	assert.NotNil(t, aggResult)
	assert.Equal(t, "TEST-001", movie.ID)
}

func TestAggregate_MultipleResults(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev", "dmm"},
		TemplateEngine:   nil,
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{ID: "TEST-001", Title: "From R18Dev", Source: "r18dev"},
		{ID: "TEST-001", Title: "From DMM", Source: "dmm"},
	}

	movie, _, err := a.Aggregate(results)
	assert.NoError(t, err)
	assert.NotNil(t, movie)
}

func TestAggregateWithPriority_CustomPriority2(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev"},
		TemplateEngine:   nil,
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{ID: "TEST-001", Title: "From R18Dev", Source: "r18dev"},
		{ID: "TEST-001", Title: "From DMM", Source: "dmm"},
	}

	movie, _, err := a.AggregateWithPriority(results, []string{"dmm", "r18dev"})
	assert.NoError(t, err)
	assert.NotNil(t, movie)
}

func TestAggregate_WithEmptyScrapers(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{},
		TemplateEngine:   nil,
		Metadata:         &MetadataConfig{},
	}
	a := New(cfg, nil, nil, nil)

	results := []*models.ScraperResult{
		{ID: "TEST-001", Title: "Test", Source: "unknown"},
	}

	movie, _, err := a.Aggregate(results)
	assert.NoError(t, err)
	assert.NotNil(t, movie)
}
