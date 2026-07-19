package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuildScraperSettingsSchema_NonNilWithExpectedKeys(t *testing.T) {
	schema := buildScraperSettingsSchema()
	require.NotNil(t, schema)
	require.Equal(t, yaml.MappingNode, schema.Kind)

	keys := indexKeys(schema)
	for _, k := range []string{
		"enabled", "language", "timeout", "rate_limit", "retry_count",
		"user_agent", "proxy", "download_proxy", "base_url",
		"use_flaresolverr", "use_browser", "scrape_actress", "cookies",
		"placeholder_threshold", "extra_placeholder_hashes",
		"scrape_bonus_screens", "api_key", "respect_retry_after",
		"request_delay", "max_retries",
	} {
		assert.True(t, keys[k], "expected key %q in schema", k)
	}

	// Calling again returns the cached instance (sync.Once).
	assert.Same(t, schema, buildScraperSettingsSchema())
}

func TestDiffYAMLDocuments_NilActualReturnsNil(t *testing.T) {
	doc, err := diffYAMLDocuments(nil, nil)
	require.NoError(t, err)
	assert.Nil(t, doc)
}

func TestDiffYAMLDocuments_NilDefaultsFallsBackToDefaultConfig(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "debug"
	doc, err := diffYAMLDocuments(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, doc)
	root := mappingRoot(doc)
	require.NotNil(t, root)
	idx := findMappingValueIndex(root, "logging")
	require.NotEqual(t, -1, idx)
}

func TestNodesEqual_BothNil(t *testing.T) {
	assert.True(t, nodesEqual(nil, nil))
}

func TestNodesEqual_OneNil(t *testing.T) {
	a := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
	assert.False(t, nodesEqual(a, nil))
	assert.False(t, nodesEqual(nil, a))
}

func TestReconcileMappings_NilNodes(t *testing.T) {
	out := &yaml.Node{Kind: yaml.MappingNode}
	reconcileMappings(nil, nil, nil, nil, "")
	assert.Equal(t, 0, len(out.Content))

	dst := &yaml.Node{Kind: yaml.MappingNode}
	reconcileMappings(dst, nil, nil, nil, "")
	assert.Equal(t, 0, len(dst.Content))
}

func TestReconcileMappings_NonMappingReplacement(t *testing.T) {
	dst := &yaml.Node{Kind: yaml.ScalarNode, Value: "old"}
	src := &yaml.Node{Kind: yaml.ScalarNode, Value: "new"}
	reconcileMappings(dst, src, nil, nil, "")
	assert.Equal(t, "new", dst.Value)
}

func TestReconcileSparse_NilRootsNoPanic(t *testing.T) {
	reconcileSparse(nil, nil, nil, nil)
	reconcileSparse(&yaml.Node{Kind: yaml.DocumentNode}, nil, nil, nil)
}

func TestReconcileMappings_ScrapersStaticKeyMappingMerged(t *testing.T) {
	dst := mustParseYAML(t, "scrapers:\n    browser:\n        enabled: true\n        timeout: 30\n")
	src := mustParseYAML(t, "scrapers:\n    browser:\n        enabled: false\n        timeout: 45\n")
	known := map[string]bool{"dmm": true}
	reconcileSparse(dst, src, schemaDoc(t), known)
	root := mappingRoot(dst)
	require.NotNil(t, root)
	scrapersIdx := findMappingValueIndex(root, "scrapers")
	require.NotEqual(t, -1, scrapersIdx)
	scrapers := root.Content[scrapersIdx]
	browserIdx := findMappingValueIndex(scrapers, "browser")
	require.NotEqual(t, -1, browserIdx)
	browser := scrapers.Content[browserIdx]
	timeoutIdx := findMappingValueIndex(browser, "timeout")
	require.NotEqual(t, -1, timeoutIdx)
	assert.Equal(t, "45", browser.Content[timeoutIdx].Value)
}

func TestReconcileMappings_ScrapersKnownScraperMappingMerged(t *testing.T) {
	dst := mustParseYAML(t, "scrapers:\n    dmm:\n        enabled: true\n        timeout: 30\n")
	src := mustParseYAML(t, "scrapers:\n    dmm:\n        enabled: false\n        timeout: 45\n")
	known := map[string]bool{"dmm": true}
	reconcileSparse(dst, src, schemaDoc(t), known)
	root := mappingRoot(dst)
	require.NotNil(t, root)
	scrapers := root.Content[findMappingValueIndex(root, "scrapers")]
	dmm := scrapers.Content[findMappingValueIndex(scrapers, "dmm")]
	timeoutIdx := findMappingValueIndex(dmm, "timeout")
	require.NotEqual(t, -1, timeoutIdx)
	assert.Equal(t, "45", dmm.Content[timeoutIdx].Value)
}

func TestMappingRoot_NonMappingReturnsNil(t *testing.T) {
	assert.Nil(t, mappingRoot(&yaml.Node{Kind: yaml.ScalarNode, Value: "x"}))
	assert.Nil(t, mappingRoot(&yaml.Node{Kind: yaml.SequenceNode}))
	assert.Nil(t, mappingRoot(&yaml.Node{Kind: yaml.DocumentNode, Content: nil}))
	root := mappingRoot(mustParseYAML(t, "foo: bar\n"))
	require.NotNil(t, root)
	assert.Equal(t, yaml.MappingNode, root.Kind)
	// Top-level MappingNode (not wrapped in a DocumentNode).
	bareMap := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "k"},
		{Kind: yaml.ScalarNode, Value: "v"},
	}}
	assert.Same(t, bareMap, mappingRoot(bareMap))
}

func TestDiffMappings_NilOrNonMappingActualNoOp(t *testing.T) {
	out := &yaml.Node{Kind: yaml.MappingNode}
	diffMappings(nil, mustParseYAML(t, "foo: bar\n"), out, "")
	assert.Equal(t, 0, len(out.Content))

	diffMappings(&yaml.Node{Kind: yaml.ScalarNode, Value: "x"}, mustParseYAML(t, "foo: bar\n"), out, "")
	assert.Equal(t, 0, len(out.Content))
}

func TestDiffMappings_DuplicateKeyEmittedOnce(t *testing.T) {
	actual := mustParseYAML(t, "foo: 1\nfoo: 2\n")
	defaults := mustParseYAML(t, "foo: 9\n")
	out := &yaml.Node{Kind: yaml.MappingNode}
	diffMappings(mappingRoot(actual), mappingRoot(defaults), out, "")
	count := 0
	for i := 0; i+1 < len(out.Content); i += 2 {
		if out.Content[i].Value == "foo" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestDiffMappings_DefaultKeyMissingInActualEmittedAsNull(t *testing.T) {
	actual := mustParseYAML(t, "config_version: 3\n")
	defaults := mustParseYAML(t, "scrapers:\n    request_timeout_seconds: 30\nconfig_version: 3\n")
	out := &yaml.Node{Kind: yaml.MappingNode}
	diffMappings(mappingRoot(actual), mappingRoot(defaults), out, "")
	idx := findMappingValueIndex(out, "scrapers")
	require.NotEqual(t, -1, idx)
	assert.Equal(t, "!!null", out.Content[idx].Tag)
}

func TestDiffMappings_DefaultAlwaysEmitKeyMissingInActualSkipped(t *testing.T) {
	actual := mustParseYAML(t, "foo: bar\n")
	defaults := mustParseYAML(t, "config_version: 3\nfoo: bar\n")
	out := &yaml.Node{Kind: yaml.MappingNode}
	diffMappings(mappingRoot(actual), mappingRoot(defaults), out, "")
	// config_version is always-emit and missing from actual → skipped (no null).
	assert.Equal(t, -1, findMappingValueIndex(out, "config_version"))
}

func TestDiffMappings_DefaultScalarKeyMissingInActualSkipped(t *testing.T) {
	actual := mustParseYAML(t, "config_version: 3\n")
	defaults := mustParseYAML(t, "config_version: 3\nsolo: scalar\n")
	out := &yaml.Node{Kind: yaml.MappingNode}
	diffMappings(mappingRoot(actual), mappingRoot(defaults), out, "")
	// solo is a scalar default missing from actual → skipped (no null emitted).
	assert.Equal(t, -1, findMappingValueIndex(out, "solo"))
}

func TestIndexKeys_NilOrNonMapping(t *testing.T) {
	assert.Equal(t, 0, len(indexKeys(nil)))
	assert.Equal(t, 0, len(indexKeys(&yaml.Node{Kind: yaml.ScalarNode, Value: "x"})))
}
