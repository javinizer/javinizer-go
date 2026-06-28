package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// priorityValueAt returns the encoded value of metadata.priority.<key> as a
// Go value decoded from the YAML document, or (<nil>, false) when the key is
// absent from the metadata.priority mapping.
func priorityValueAt(t *testing.T, doc *yaml.Node, key string) (any, bool) {
	t.Helper()
	prio := navigateToMapping(doc, "metadata", "priority")
	if prio == nil || prio.Kind != yaml.MappingNode {
		return nil, false
	}
	idx := findMappingValueIndex(prio, key)
	if idx == -1 {
		return nil, false
	}
	var out any
	require.NoError(t, prio.Content[idx].Decode(&out))
	return out, true
}

func TestMergeYAMLNode_PruneMetadataPriorityDeletedKeys(t *testing.T) {
	t.Run("deletes per-field priority key absent in new cfg", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `metadata:
    priority:
        series:
            - dmm
        priority:
            - dmm
            - r18dev
`)
		// New config: series override removed (reset to global); global priority kept.
		newDoc := mustParseYAMLDoc(t, `metadata:
    priority:
        priority:
            - dmm
            - r18dev
`)

		mergeYAMLNode(existing, newDoc)

		_, hasSeries := priorityValueAt(t, existing, "series")
		assert.False(t, hasSeries, "series override must be pruned from metadata.priority")

		prio, hasPrio := priorityValueAt(t, existing, "priority")
		assert.True(t, hasPrio, "global priority key must be retained")
		assert.Equal(t, []any{"dmm", "r18dev"}, prio)
	})

	t.Run("updates kept key and deletes absent sibling", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `metadata:
    priority:
        series:
            - dmm
        title:
            - r18dev
        priority:
            - dmm
`)
		newDoc := mustParseYAMLDoc(t, `metadata:
    priority:
        series:
            - r18dev
        priority:
            - dmm
`)

		mergeYAMLNode(existing, newDoc)

		series, hasSeries := priorityValueAt(t, existing, "series")
		assert.True(t, hasSeries, "series (present in new) must be kept and updated")
		assert.Equal(t, []any{"r18dev"}, series)

		_, hasTitle := priorityValueAt(t, existing, "title")
		assert.False(t, hasTitle, "title (absent in new) must be deleted from metadata.priority")
	})

	t.Run("preserves keys outside metadata.priority when absent in new", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `scrapers:
    user_agent: keepme
    foo:
        bar: 1
metadata:
    priority:
        series:
            - dmm
        priority:
            - dmm
`)
		newDoc := mustParseYAMLDoc(t, `metadata:
    priority:
        priority:
            - dmm
`)

		mergeYAMLNode(existing, newDoc)

		// Keys under scrapers (entirely absent in new) must survive — proves
		// pruning is scoped to metadata.priority only.
		scrapers := navigateToMapping(existing, "scrapers")
		require.NotNil(t, scrapers)
		assert.NotEqual(t, -1, findMappingValueIndex(scrapers, "user_agent"))
		assert.NotEqual(t, -1, findMappingValueIndex(scrapers, "foo"))

		// Meanwhile the per-field override still gets pruned.
		_, hasSeries := priorityValueAt(t, existing, "series")
		assert.False(t, hasSeries, "series must be pruned")
	})

	t.Run("preserves dst comment on a kept priority key", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `metadata:
    priority:
        # series override comment
        series:
            - dmm
        title:
            - r18dev
        priority:
            - dmm
`)
		newDoc := mustParseYAMLDoc(t, `metadata:
    priority:
        series:
            - r18dev
        priority:
            - dmm
`)

		mergeYAMLNode(existing, newDoc)

		prio := navigateToMapping(existing, "metadata", "priority")
		require.NotNil(t, prio)
		idx := findMappingValueIndex(prio, "series")
		require.NotEqual(t, -1, idx)
		keyNode := prio.Content[idx-1]
		assert.Equal(t, "# series override comment", keyNode.HeadComment,
			"head comment on kept priority key must be preserved")

		_, hasTitle := priorityValueAt(t, existing, "title")
		assert.False(t, hasTitle, "title (absent in new) must be deleted")
	})

	t.Run("no-op when metadata.priority absent in dst", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `server:
    port: 8080
`)
		newDoc := mustParseYAMLDoc(t, `metadata:
    priority:
        priority:
            - dmm
`)

		mergeYAMLNode(existing, newDoc)

		// After merge, metadata.priority is appended from src; pruning against
		// src (same keys) must not corrupt it.
		prio := navigateToMapping(existing, "metadata", "priority")
		require.NotNil(t, prio)
		assert.NotEqual(t, -1, findMappingValueIndex(prio, "priority"))
	})

	t.Run("Save persists deleted per-field priority override to disk", func(t *testing.T) {
		path := t.TempDir() + "/config.yaml"
		initial := DefaultConfig(nil, nil)
		initial.Metadata.Priority.Priority = []string{"dmm"}
		initial.Metadata.Priority.Fields = map[string][]string{"series": {"dmm"}}
		require.NoError(t, Save(initial, path))

		// Reload, then "reset to global" by deleting the per-field override.
		loaded, err := Load(path)
		require.NoError(t, err)
		delete(loaded.Metadata.Priority.Fields, "series")
		require.NoError(t, Save(loaded, path))

		reloaded, err := Load(path)
		require.NoError(t, err)
		assert.NotContains(t, reloaded.Metadata.Priority.Fields, "series",
			"deleted per-field override must not reappear after reload")
		assert.Equal(t, []string{"dmm"}, reloaded.Metadata.Priority.Priority)
	})
}
