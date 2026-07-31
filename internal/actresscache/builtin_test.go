package actresscache

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinCacheLoads(t *testing.T) {
	cache, err := Builtin()
	require.NoError(t, err)
	assert.Equal(t, RuntimeSchemaVersion, cache.SchemaVersion)
	assert.Len(t, cache.Records, 25341)
	assert.NotNil(t, BuiltinData())
	assert.Less(t, len(builtinData), 2<<20)
	assert.Len(t, NewRuntimeCache(cache).Records, len(cache.Records))
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

func TestBuiltinLookupMissesEmptyPlaceholderCache(t *testing.T) {
	_, ok := Lookup(123, "花子", "Hanako", "Yamada")
	assert.False(t, ok)
}
