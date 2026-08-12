package actresscache

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinCacheLoads(t *testing.T) {
	cache, err := Builtin()
	require.NoError(t, err)
	assert.Equal(t, RuntimeSchemaVersion, cache.SchemaVersion)
	assert.Len(t, cache.Records, 25279)
	assert.NotNil(t, BuiltinData())
	assert.Less(t, len(builtinData), 2<<20)
	// Runtime projection may drop records whose identity is too weak (single-part
	// romanized names are not unique reachability owners under the new rules).
	assert.LessOrEqual(t, len(NewRuntimeCache(cache).Records), len(cache.Records))
}

func TestBuiltinDataIsJSON(t *testing.T) {
	var cache RuntimeCache
	require.NoError(t, json.Unmarshal(BuiltinData(), &cache))
	assert.Equal(t, RuntimeSchemaVersion, cache.SchemaVersion)
	assert.NotEmpty(t, cache.Records)
}

func TestBuiltinLookupFindsGeneratedRecord(t *testing.T) {
	record, ok := Lookup(28262, "", "", "")
	require.True(t, ok)
	assert.Equal(t, 28262, record.DMMID)
	assert.NotEmpty(t, record.ThumbURL)
}

func TestBuiltinLookupFallsBackToRomanizedWhenJapaneseIdentityMisses(t *testing.T) {
	originalData := builtinData
	defer func() {
		builtinData = originalData
		resetBuiltinIndex()
	}()
	data, err := json.Marshal(RuntimeCache{SchemaVersion: RuntimeSchemaVersion, Records: []RuntimeRecord{{DMMID: 1, FirstName: "Same", LastName: "Name", JapaneseName: "別人"}}})
	require.NoError(t, err)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err = writer.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	builtinData = compressed.Bytes()
	resetBuiltinIndex()

	fallback, ok := Lookup(0, "未収録", "Same", "Name")
	require.True(t, ok, "jp-name miss must fall through to the romanized index")
	assert.Equal(t, 1, fallback.DMMID)
	_, ok = Lookup(0, "", "", "")
	assert.False(t, ok)
	found, ok := Lookup(0, "", "Same", "Name")
	require.True(t, ok)
	assert.Equal(t, 1, found.DMMID)
}

func TestBuiltinLookupRejectsConflictingDMMIDFallback(t *testing.T) {
	originalData := builtinData
	defer func() {
		builtinData = originalData
		resetBuiltinIndex()
	}()
	data, err := json.Marshal(RuntimeCache{SchemaVersion: RuntimeSchemaVersion, Records: []RuntimeRecord{
		{BuiltinKey: "conflicting", DMMID: 100, JapaneseName: "新旧重複"},
		{BuiltinKey: "legacy", DMMID: 0, JapaneseName: "レガシー"},
		{BuiltinKey: "romanized-conflict", DMMID: 200, FirstName: "Jane", LastName: "Doe"},
		{BuiltinKey: "ambiguous-one", DMMID: 300, JapaneseName: "曖昧一"},
		{BuiltinKey: "ambiguous-two", DMMID: 300, JapaneseName: "曖昧二"},
	}})
	require.NoError(t, err)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err = writer.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	builtinData = compressed.Bytes()
	resetBuiltinIndex()

	// A positive-ID miss must not fall back to a record whose authoritative
	// DMM ID conflicts, via either the jp or the romanized index.
	_, ok := Lookup(999, "新旧重複", "", "")
	assert.False(t, ok, "conflicting positive DMM ID via jp index must be rejected")
	_, ok = Lookup(999, "", "Jane", "Doe")
	assert.False(t, ok, "conflicting positive DMM ID via romanized index must be rejected")

	// DMM-less legacy records remain reachable by name fallback.
	legacy, ok := Lookup(999, "レガシー", "", "")
	require.True(t, ok)
	assert.Equal(t, "legacy", legacy.BuiltinKey)

	// Same-ID records whose DMM index was dropped as ambiguous stay reachable.
	ambiguous, ok := Lookup(300, "曖昧一", "", "")
	require.True(t, ok)
	assert.Equal(t, "ambiguous-one", ambiguous.BuiltinKey)
}

func TestBuiltinLookupMissesEmptyPlaceholderCache(t *testing.T) {
	_, ok := Lookup(123, "花子", "Hanako", "Yamada")
	assert.False(t, ok)
}
