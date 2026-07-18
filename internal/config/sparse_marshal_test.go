package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func mustParseYAML(t *testing.T, data string) *yaml.Node {
	t.Helper()
	doc, err := parseYAMLDocument([]byte(data))
	require.NoError(t, err)
	return doc
}

func mustEncode(t *testing.T, doc *yaml.Node) []byte {
	t.Helper()
	data, err := encodeYAMLDocument(doc)
	require.NoError(t, err)
	return data
}

func schemaDoc(t *testing.T) *yaml.Node {
	t.Helper()
	doc, err := configToYAMLDocument(DefaultConfig(nil, nil))
	require.NoError(t, err)
	return doc
}

func TestDiffYAMLDocuments_DefaultConfig(t *testing.T) {
	doc, err := diffYAMLDocuments(DefaultConfig(nil, nil), nil)
	require.NoError(t, err)
	require.NotNil(t, doc)

	root := mappingRoot(doc)
	require.NotNil(t, root)
	require.Equal(t, yaml.MappingNode, root.Kind)
	require.Len(t, root.Content, 2)
	assert.Equal(t, "config_version", root.Content[0].Value)
}

func TestDiffYAMLDocuments_OneScalarChanged(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.RequestTimeoutSeconds = 120

	doc, err := diffYAMLDocuments(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, doc)

	root := mappingRoot(doc)
	require.NotNil(t, root)
	require.Len(t, root.Content, 4)

	keys := []string{root.Content[0].Value, root.Content[2].Value}
	assert.ElementsMatch(t, []string{"config_version", "scrapers"}, keys)

	scrapersIdx := findMappingValueIndex(root, "scrapers")
	require.NotEqual(t, -1, scrapersIdx)
	scrapers := root.Content[scrapersIdx]
	require.Equal(t, yaml.MappingNode, scrapers.Kind)
	require.Len(t, scrapers.Content, 2)
	assert.Equal(t, "request_timeout_seconds", scrapers.Content[0].Value)
	assert.Equal(t, "120", scrapers.Content[1].Value)
}

func TestDiffYAMLDocuments_RoundTrip(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.RequestTimeoutSeconds = 120
	cfg.Logging.Level = "debug"
	cfg.Output.Template.FolderFormat = "custom/<ID>"

	doc, err := diffYAMLDocuments(cfg, nil)
	require.NoError(t, err)

	data := mustEncode(t, doc)
	decoded, err := decodeConfig(data)
	require.NoError(t, err)

	assert.Equal(t, cfg.ConfigVersion, decoded.ConfigVersion)
	assert.Equal(t, cfg.Scrapers.RequestTimeoutSeconds, decoded.Scrapers.RequestTimeoutSeconds)
	assert.Equal(t, cfg.Logging.Level, decoded.Logging.Level)
	assert.Equal(t, cfg.Output.Template.FolderFormat, decoded.Output.Template.FolderFormat)
}

func TestReconcileSparse_SourceOnlyKeyAdded(t *testing.T) {
	dst := mustParseYAML(t, "ui:\n    theme: dark\n")
	sparse := mustParseYAML(t, "config_version: 3\n")

	reconcileSparse(dst, sparse, schemaDoc(t))

	root := mappingRoot(dst)
	require.NotNil(t, root)
	idx := findMappingValueIndex(root, "config_version")
	require.NotEqual(t, -1, idx)
	assert.Equal(t, "3", root.Content[idx].Value)
	assert.NotEqual(t, -1, findMappingValueIndex(root, "ui"))
}

func TestReconcileSparse_KnownStaleKeyRemoved(t *testing.T) {
	dst := mustParseYAML(t, "scrapers:\n    request_timeout_seconds: 60\n")
	sparse := mustParseYAML(t, "config_version: 3\n")

	reconcileSparse(dst, sparse, schemaDoc(t))

	root := mappingRoot(dst)
	require.NotNil(t, root)
	assert.Equal(t, -1, findMappingValueIndex(root, "scrapers"))
	assert.NotEqual(t, -1, findMappingValueIndex(root, "config_version"))
}

func TestReconcileSparse_UnknownKeyPreserved(t *testing.T) {
	dst := mustParseYAML(t, "my_custom_key: custom_value\nconfig_version: 2\n")
	sparse := mustParseYAML(t, "config_version: 3\n")

	reconcileSparse(dst, sparse, schemaDoc(t))

	root := mappingRoot(dst)
	require.NotNil(t, root)
	idx := findMappingValueIndex(root, "my_custom_key")
	require.NotEqual(t, -1, idx)
	assert.Equal(t, "custom_value", root.Content[idx].Value)
	cvIdx := findMappingValueIndex(root, "config_version")
	require.NotEqual(t, -1, cvIdx)
	assert.Equal(t, "3", root.Content[cvIdx].Value)
}

func TestReconcileSparse_NilSchemaDisablesDeletion(t *testing.T) {
	dst := mustParseYAML(t, "scrapers:\n    request_timeout_seconds: 60\nextra_key: keepme\n")
	sparse := mustParseYAML(t, "config_version: 3\n")

	reconcileSparse(dst, sparse, nil)

	root := mappingRoot(dst)
	require.NotNil(t, root)
	assert.NotEqual(t, -1, findMappingValueIndex(root, "scrapers"))
	assert.NotEqual(t, -1, findMappingValueIndex(root, "extra_key"))
	assert.NotEqual(t, -1, findMappingValueIndex(root, "config_version"))
}
