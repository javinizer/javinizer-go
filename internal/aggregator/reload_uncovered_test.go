package aggregator

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReloadReplacementCaches_NilAggregator(t *testing.T) {
	var a *Aggregator
	a.ReloadReplacementCaches(context.Background())
}

func TestNew_NilTemplateEngineCreatesDefault(t *testing.T) {
	cfg := &Config{
		TemplateEngine: nil,
		Metadata:       &MetadataConfig{},
	}
	agg := New(cfg, nil, nil, nil)
	require.NotNil(t, agg)
	assert.NotNil(t, agg.templateEngine, "Should use default template engine when nil")
}

func TestNew_WithAllSubModules(t *testing.T) {
	cfg := &Config{
		TemplateEngine: template.NewEngine(),
		Metadata:       &MetadataConfig{},
	}
	gp := NewGenreProcessor(cfg.Metadata, nil)
	wp := NewWordProcessor(cfg.Metadata, nil)
	ar := NewAliasResolver(cfg.Metadata, nil)

	agg := New(cfg, gp, wp, ar)
	require.NotNil(t, agg)
	assert.NotNil(t, agg.genreProcessor)
	assert.NotNil(t, agg.wordProcessor)
	assert.NotNil(t, agg.aliasResolver)
}

func TestReloadReplacementCaches_WithAllSubModules(t *testing.T) {
	cfg := &Config{
		TemplateEngine: template.NewEngine(),
		Metadata:       &MetadataConfig{},
	}
	gp := NewGenreProcessor(cfg.Metadata, nil)
	wp := NewWordProcessor(cfg.Metadata, nil)
	ar := NewAliasResolver(cfg.Metadata, nil)

	agg := New(cfg, gp, wp, ar)
	require.NotNil(t, agg)

	agg.ReloadReplacementCaches(context.Background())
}

func TestNew_WithScrapersPriority(t *testing.T) {
	cfg := &Config{
		ScrapersPriority: []string{"r18dev", "dmm"},
		TemplateEngine:   template.NewEngine(),
		Metadata:         &MetadataConfig{},
	}

	agg := New(cfg, nil, nil, nil)
	require.NotNil(t, agg)
}
