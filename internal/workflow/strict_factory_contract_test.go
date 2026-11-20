package workflow

import (
	"errors"
	"regexp/syntax"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestStrictWorkflowFactoryRejectsPartialConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	registry := scraperutil.NewScraperRegistry()
	repos := database.Repositories{}

	// The same partial wiring can build a scan-only factory through the API
	// constructor. Strict callers get the original typed error and no factory.
	partial, err := NewWorkflowFactoryFromRepos(cfg, registry, repos)
	require.NotNil(t, partial)
	var scraperErr *ScraperConstructionError
	require.ErrorAs(t, err, &scraperErr)
	require.NotNil(t, partial.NewScanOnlyWorkflow())

	strict, err := NewStrictWorkflowFactoryFromRepos(cfg, registry, repos)
	require.Nil(t, strict)
	require.IsType(t, scraperErr, err)
	require.EqualError(t, err, scraperErr.Error())

	// Matcher validation is first even when the scraper would also fail.
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "[invalid"
	strict, err = NewStrictWorkflowFactoryFromRepos(cfg, registry, repos)
	require.Nil(t, strict)
	var syntaxErr *syntax.Error
	require.ErrorAs(t, err, &syntaxErr)
	require.Same(t, syntaxErr, errors.Unwrap(err))
}

func TestStrictWorkflowFactoryProducesFullWorkflow(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	registry := scraperutil.NewScraperRegistry()
	db := newInMemoryDB(t)

	factory, err := NewStrictWorkflowFactoryFromRepos(cfg, registry, db.Repositories())
	require.NoError(t, err)
	require.NotNil(t, factory)
	_, err = factory.NewWorkflow("fixture")
	require.NoError(t, err)
}
