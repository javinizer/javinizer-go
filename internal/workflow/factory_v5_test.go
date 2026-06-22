package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

func TestCollectDownloadProxyResolvers_V5_NilRegistry(t *testing.T) {
	var registry *scraperutil.ScraperRegistry
	resolvers := registry.CollectDownloadProxyResolvers(nil)
	assert.Nil(t, resolvers)
}

func TestCollectDownloadProxyResolvers_V5_NilRegistryWithPriority(t *testing.T) {
	var registry *scraperutil.ScraperRegistry
	resolvers := registry.CollectDownloadProxyResolvers([]string{"r18dev"})
	assert.Nil(t, resolvers)
}

func TestCollectDownloadProxyResolvers_V5_EmptyPriorityEmptyRegistry(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	resolvers := registry.CollectDownloadProxyResolvers([]string{})
	assert.Empty(t, resolvers)
}

func TestWorkflowFactory_V5_NilFactoryReloadCaches(t *testing.T) {
	var f *WorkflowFactory
	// Should not panic on nil factory
	f.ReloadReplacementCaches(nil)
}

func TestWorkflowFactory_V5_NilAggregatorReloadCaches(t *testing.T) {
	f := &WorkflowFactory{}
	// Should not panic when aggregator is nil
	f.ReloadReplacementCaches(nil)
}

func TestApplyPreset_V5_InvalidPreset(t *testing.T) {
	s, a, err := nfo.ApplyPresetTyped("invalid", nfo.PreferScraper, true)
	assert.Error(t, err)
	assert.Equal(t, nfo.PreferScraper, s)
	assert.True(t, a)
}

func TestApplyPreset_V5_Conservative(t *testing.T) {
	s, a, err := nfo.ApplyPresetTyped("conservative", nfo.PreferScraper, false)
	assert.NoError(t, err)
	assert.Equal(t, nfo.PreserveExisting, s)
	assert.True(t, a)
}

func TestApplyPreset_V5_GapFill(t *testing.T) {
	s, a, err := nfo.ApplyPresetTyped("gap-fill", nfo.PreferScraper, false)
	assert.NoError(t, err)
	assert.Equal(t, nfo.FillMissingOnly, s)
	assert.True(t, a)
}

func TestApplyPreset_V5_Aggressive(t *testing.T) {
	s, a, err := nfo.ApplyPresetTyped("aggressive", nfo.PreferNFO, true)
	assert.NoError(t, err)
	assert.Equal(t, nfo.PreferScraper, s)
	assert.False(t, a)
}

func TestApplyPreset_V5_EmptyPreset(t *testing.T) {
	s, a, err := nfo.ApplyPresetTyped("", nfo.PreferNFO, false)
	assert.NoError(t, err)
	assert.Equal(t, nfo.PreferNFO, s)
	assert.False(t, a)
}

func TestResolveScalarStrategy_V5_ValidStrategies(t *testing.T) {
	tests := []struct {
		input string
		want  nfo.MergeStrategy
	}{
		{"prefer-scraper", nfo.PreferScraper},
		{"prefer-nfo", nfo.PreferNFO},
		{"merge-arrays", nfo.MergeArrays},
		{"preserve-existing", nfo.PreserveExisting},
		{"fill-missing-only", nfo.FillMissingOnly},
	}

	for _, tt := range tests {
		got, err := nfo.ParseScalarStrategy(tt.input)
		assert.NoError(t, err, "nfo.ParseScalarStrategy(%q)", tt.input)
		assert.Equal(t, tt.want, got, "nfo.ParseScalarStrategy(%q)", tt.input)
	}
}

func TestResolveScalarStrategy_V5_InvalidStrategy(t *testing.T) {
	_, err := nfo.ParseScalarStrategy("invalid-strategy")
	assert.Error(t, err)
}

func TestResolveScalarStrategy_V5_EmptyStrategy(t *testing.T) {
	got, err := nfo.ParseScalarStrategy("")
	assert.NoError(t, err)
	assert.Equal(t, nfo.PreferNFO, got)
}

func TestResolveArrayStrategy_V5_ValidStrategies(t *testing.T) {
	merge, err := nfo.ParseArrayStrategy("merge")
	assert.NoError(t, err)
	assert.True(t, merge)

	replace, err := nfo.ParseArrayStrategy("replace")
	assert.NoError(t, err)
	assert.False(t, replace)
}

func TestResolveArrayStrategy_V5_EmptyStrategy(t *testing.T) {
	got, err := nfo.ParseArrayStrategy("")
	assert.NoError(t, err)
	assert.True(t, got)
}

func TestResolveArrayStrategy_V5_InvalidStrategy(t *testing.T) {
	_, err := nfo.ParseArrayStrategy("invalid")
	assert.Error(t, err)
}

func TestValidateScalarStrategy_V5_ValidStrategies(t *testing.T) {
	for _, s := range []string{"prefer-scraper", "prefer-nfo", "merge-arrays", "preserve-existing", "fill-missing-only"} {
		_, err := nfo.ParseScalarStrategy(s)
		assert.NoError(t, err, "nfo.ParseScalarStrategy(%q)", s)
	}
}

func TestValidateScalarStrategy_V5_InvalidStrategy(t *testing.T) {
	_, err := nfo.ParseScalarStrategy("invalid")
	assert.Error(t, err)
}

func TestValidateArrayStrategy_V5_ValidStrategies(t *testing.T) {
	_, err := nfo.ParseArrayStrategy("merge")
	assert.NoError(t, err)
	_, err = nfo.ParseArrayStrategy("replace")
	assert.NoError(t, err)
}

func TestValidateArrayStrategy_V5_InvalidStrategy(t *testing.T) {
	_, err := nfo.ParseArrayStrategy("invalid")
	assert.Error(t, err)
}

func TestBuildSharedSubGraph_V5_NilRegistry(t *testing.T) {
	fc := workflowFactoryConfig{}
	_, err := NewWorkflowFactory(fc)
	assert.Error(t, err)
}

func TestNewWorkflowFactory_V5_NilBatchFileOpRepo(t *testing.T) {
	fc := workflowFactoryConfig{}
	_, err := NewWorkflowFactory(fc)
	assert.Error(t, err)
}

func TestApplyPresetString_V5_Conservative(t *testing.T) {
	s, a, err := nfo.ApplyPreset("conservative", "prefer-scraper", "replace")
	assert.NoError(t, err)
	assert.Equal(t, "preserve-existing", s)
	assert.Equal(t, "merge", a)
}

func TestApplyPresetString_V5_EmptyPreset(t *testing.T) {
	s, a, err := nfo.ApplyPreset("", "prefer-nfo", "merge")
	assert.NoError(t, err)
	assert.Equal(t, "prefer-nfo", s)
	assert.Equal(t, "merge", a)
}

func TestApplyPresetString_V5_InvalidPreset(t *testing.T) {
	_, _, err := nfo.ApplyPreset("invalid", "prefer-nfo", "merge")
	assert.Error(t, err)
}

func TestMergeStrategy_V5_Constants(t *testing.T) {
	assert.Equal(t, nfo.MergeStrategy("prefer-scraper"), nfo.PreferScraper)
	assert.Equal(t, nfo.MergeStrategy("prefer-nfo"), nfo.PreferNFO)
	assert.Equal(t, nfo.MergeStrategy("merge-arrays"), nfo.MergeArrays)
	assert.Equal(t, nfo.MergeStrategy("preserve-existing"), nfo.PreserveExisting)
	assert.Equal(t, nfo.MergeStrategy("fill-missing-only"), nfo.FillMissingOnly)
}
