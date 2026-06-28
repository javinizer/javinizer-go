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

	t.Run("deletes whole metadata.priority block when absent in new cfg", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `metadata:
    priority:
        series:
            - dmm
        priority:
            - dmm
            - r18dev
`)
		// New config omits metadata.priority entirely (e.g. user cleared all
		// priority settings including the global list).
		newDoc := mustParseYAMLDoc(t, `metadata:
    some_other_field: keep
`)

		mergeYAMLNode(existing, newDoc)

		prio := navigateToMapping(existing, "metadata", "priority")
		assert.Nil(t, prio, "metadata.priority block must be deleted when absent in new cfg")

		metadata := navigateToMapping(existing, "metadata")
		require.NotNil(t, metadata)
		assert.NotEqual(t, -1, findMappingValueIndex(metadata, "some_other_field"),
			"sibling metadata key must be preserved (only the priority subtree is deleted)")
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

// TestMergeYAMLNode_PruneMetadataPriorityEdgeCases covers the defensive early-return
// branches of pruneMetadataPriorityFields and navigateToMapping that the happy-path
// subtests above don't reach (scalar metadata/priority, missing priority key,
// scalar src priority, and an empty DocumentNode).
func TestMergeYAMLNode_PruneMetadataPriorityEdgeCases(t *testing.T) {
	t.Run("no-op when metadata exists but priority key is absent in dst", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `metadata:
    other: value
`)
		// src also has no priority key -> merge does not add one, so prune hits the
		// dstPriorityIdx == -1 guard and returns without touching metadata.
		newDoc := mustParseYAMLDoc(t, `metadata:
    another: value2
`)
		mergeYAMLNode(existing, newDoc)
		metadata := navigateToMapping(existing, "metadata")
		require.NotNil(t, metadata)
		assert.Equal(t, -1, findMappingValueIndex(metadata, "priority"),
			"no priority key should exist in dst after merge")
	})

	t.Run("no-op when dst metadata is a scalar and src has no metadata", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `metadata: justastring
server:
    port: 8080
`)
		newDoc := mustParseYAMLDoc(t, `server:
    port: 8080
`)
		mergeYAMLNode(existing, newDoc)
		// dst metadata is a scalar -> pruneMetadataPriorityFields returns at the
		// dstMetadata.Kind != MappingNode guard without touching anything.
		metadata := navigateToMapping(existing, "metadata")
		require.NotNil(t, metadata)
	})

	t.Run("no-op when dst metadata.priority is a scalar and src omits it", func(t *testing.T) {
		existing := mustParseYAMLDoc(t, `metadata:
    priority: notamapping
server:
    port: 8080
`)
		newDoc := mustParseYAMLDoc(t, `server:
    port: 8080
`)
		mergeYAMLNode(existing, newDoc)
		// dst priority is a scalar -> prune returns at the dstPriority.Kind guard;
		// the scalar value is left untouched (src has no priority to merge in).
		prio := navigateToMapping(existing, "metadata", "priority")
		require.NotNil(t, prio)
		assert.Equal(t, "notamapping", prio.Value)
	})
}

// TestPruneMetadataPriorityFields_SrcScalarPriority covers the srcPriority.Kind
// != MappingNode branch (the second operand of the nil-check) by calling the
// helper directly with a scalar src priority — the merge flow always converts
// dst to match src's kind first, so this branch is only reachable via a direct
// call with mismatched dst/src shapes.
func TestPruneMetadataPriorityFields_SrcScalarPriority(t *testing.T) {
	dst := mustParseYAMLDoc(t, `metadata:
    priority:
        series:
            - dmm
`)
	src := mustParseYAMLDoc(t, `metadata:
    priority: notamapping
`)

	pruneMetadataPriorityFields(dst, src)

	prio := navigateToMapping(dst, "metadata", "priority")
	assert.Nil(t, prio, "non-mapping src priority must delete dst's priority block")
}

// TestNavigateToMapping_EmptyDocumentNode covers the empty-DocumentNode branch
// (len(cur.Content) == 0) of navigateToMapping.
func TestNavigateToMapping_EmptyDocumentNode(t *testing.T) {
	emptyDoc := &yaml.Node{Kind: yaml.DocumentNode}
	assert.Nil(t, navigateToMapping(emptyDoc, "metadata"))
}

// TestNavigateToMapping_NonMappingDocumentRoot covers the cur.Kind != MappingNode
// guard inside the key loop: a document whose root is a scalar (not a mapping)
// cannot be descended into by key.
func TestNavigateToMapping_NonMappingDocumentRoot(t *testing.T) {
	doc := mustParseYAMLDoc(t, `justastring
`)
	assert.Nil(t, navigateToMapping(doc, "metadata"))
}

// TestNavigateToMapping_NilRoot covers the root == nil guard.
func TestNavigateToMapping_NilRoot(t *testing.T) {
	assert.Nil(t, navigateToMapping(nil, "metadata"))
}

// TestNavigateToMapping_NilValueInChain covers the cur == nil branch of the
// loop guard: descending into a mapping whose value is a nil node sets cur=nil,
// and the next key iteration returns nil at the cur == nil check.
func TestNavigateToMapping_NilValueInChain(t *testing.T) {
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "metadata", Tag: "!!str"},
		nil,
	}}
	assert.Nil(t, navigateToMapping(root, "metadata", "priority"))
}
