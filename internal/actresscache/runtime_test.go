package actresscache

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRuntimeCacheDropsAuditMetadata(t *testing.T) {
	cache := Cache{
		SchemaVersion: 1,
		Records: []Record{{
			BuiltinKey: "actress:dmm:1", DMMID: 1, FirstName: "A", LastName: "B",
			JapaneseName: "名前", Aliases: []string{"旧名"}, ThumbURL: "https://example.test/a.jpg",
			Thumbnail: ThumbnailValidation{SHA256: "hash"}, PrimarySource: "test",
			Sources: []SourceRecord{{Source: "test", SourceID: "1", Thumbnail: ThumbnailValidation{SHA256: "hash"}}},
		}, {BuiltinKey: "unreachable", ThumbURL: "https://www.minnano-av.com/p_actress_125_125/000/np.gif"},
			// Single romanized token with no other identity: Lookup refuses a
			// single-part romanized fallback, so this record would be embedded
			// but unfindable — projection must drop it.
			{BuiltinKey: "name-only", FirstName: "OnlyFirst", ThumbURL: "https://example.test/n.jpg"},
			{BuiltinKey: "placeholder", DMMID: 2, JapaneseName: "占位", ThumbURL: "https://www.minnano-av.com/p_actress_125_125/000/np.gif"}},
	}
	runtime := NewRuntimeCache(cache)
	require.Equal(t, RuntimeSchemaVersion, runtime.SchemaVersion)
	require.Len(t, runtime.Records, 2)
	assert.Equal(t, cache.Records[0].BuiltinKey, runtime.Records[0].BuiltinKey)
	assert.Empty(t, runtime.Records[1].ThumbURL)
	assert.Equal(t, cache.Records[0].ThumbURL, runtime.Records[0].ThumbURL)
	assert.Empty(t, runtime.Records[0].Aliases[1:])
}

func TestWriteRuntimeFileIsDeterministicAndCompact(t *testing.T) {
	cache := Cache{SchemaVersion: 1, Records: []Record{{BuiltinKey: "actress:dmm:1", DMMID: 1, FirstName: "A", LastName: "B"}}}
	first := filepath.Join(t.TempDir(), "one.json.gz")
	second := filepath.Join(t.TempDir(), "two.json.gz")
	require.NoError(t, WriteRuntimeFile(first, cache))
	require.NoError(t, WriteRuntimeFile(second, cache))
	firstData, err := os.ReadFile(first)
	require.NoError(t, err)
	secondData, err := os.ReadFile(second)
	require.NoError(t, err)
	assert.Equal(t, firstData, secondData)
	info, err := os.Stat(first)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	}

	file, err := os.Open(first)
	require.NoError(t, err)
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	var decoded RuntimeCache
	require.NoError(t, json.NewDecoder(reader).Decode(&decoded))
	require.NoError(t, reader.Close())
	require.NoError(t, file.Close())
	assert.Equal(t, RuntimeSchemaVersion, decoded.SchemaVersion)
	assert.Len(t, decoded.Records, 1)
}

func TestWriteRuntimeFileRejectsEmptyPath(t *testing.T) {
	assert.Error(t, WriteRuntimeFile("", Cache{}))
}

func TestDecodeRuntimeCacheRejectsOversizedPayload(t *testing.T) {
	// A gzipped payload decompressing past the decoder cap must be rejected
	// instead of materializing unboundedly.
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	data, err := json.Marshal(RuntimeCache{SchemaVersion: RuntimeSchemaVersion, Records: []RuntimeRecord{{BuiltinKey: "x", ThumbURL: string(make([]byte, maxRuntimeCacheDecodedBytes))}}})
	require.NoError(t, err)
	_, err = writer.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	_, err = decodeRuntimeCache(bytes.NewReader(compressed.Bytes()))
	require.Error(t, err, "over-cap payload must not decode successfully")
}
func TestDecodeRuntimeCacheRejectsInvalidDataAndSchema(t *testing.T) {
	_, err := decodeRuntimeCache(bytes.NewReader([]byte("not gzip")))
	require.Error(t, err)

	var data bytes.Buffer
	writer := gzip.NewWriter(&data)
	require.NoError(t, json.NewEncoder(writer).Encode(RuntimeCache{SchemaVersion: RuntimeSchemaVersion + 1}))
	require.NoError(t, writer.Close())
	_, err = decodeRuntimeCache(&data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestStringIndexRejectsAmbiguousNames(t *testing.T) {
	index := make(map[string]int)
	ambiguous := make(map[string]struct{})
	addStringIndex(index, ambiguous, "同名", 1)
	addStringIndex(index, ambiguous, "同名", 2)
	_, ok := index["同名"]
	assert.False(t, ok)
	_, ok = ambiguous["同名"]
	assert.True(t, ok)
}

func TestStringIndexKeepsAliasesFromSameRecord(t *testing.T) {
	index := make(map[string]int)
	ambiguous := make(map[string]struct{})
	addStringIndex(index, ambiguous, "同名", 1)
	addStringIndex(index, ambiguous, "同名", 1)
	assert.Equal(t, 1, index["同名"])
	assert.NotContains(t, ambiguous, "同名")
}
