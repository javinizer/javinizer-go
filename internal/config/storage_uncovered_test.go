package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseConfigLockMetadataUncovered(t *testing.T) {
	t.Run("valid metadata", func(t *testing.T) {
		pid, ts, ok := parseConfigLockMetadata("pid=1234,time=1700000000000000000")
		assert.True(t, ok)
		assert.Equal(t, 1234, pid)
		assert.Equal(t, int64(1700000000000000000), ts)
	})

	t.Run("invalid pid", func(t *testing.T) {
		_, _, ok := parseConfigLockMetadata("pid=abc,time=123")
		assert.False(t, ok)
	})

	t.Run("missing time", func(t *testing.T) {
		_, _, ok := parseConfigLockMetadata("pid=1234")
		assert.False(t, ok)
	})

	t.Run("empty string", func(t *testing.T) {
		_, _, ok := parseConfigLockMetadata("")
		assert.False(t, ok)
	})

	t.Run("whitespace-separated", func(t *testing.T) {
		pid, ts, ok := parseConfigLockMetadata("pid=99 time=456")
		assert.True(t, ok)
		assert.Equal(t, 99, pid)
		assert.Equal(t, int64(456), ts)
	})
}

func TestMakeConfigLockToken(t *testing.T) {
	token := makeConfigLockToken()
	assert.Contains(t, token, "pid=")
	assert.Contains(t, token, "time=")
}

func TestIsProcessAlive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	t.Run("current process is alive", func(t *testing.T) {
		assert.True(t, isProcessAlive(os.Getpid()))
	})

	t.Run("pid 0 is not alive", func(t *testing.T) {
		assert.False(t, isProcessAlive(0))
	})

	t.Run("negative pid is not alive", func(t *testing.T) {
		assert.False(t, isProcessAlive(-1))
	})

	t.Run("very large pid is not alive", func(t *testing.T) {
		assert.False(t, isProcessAlive(999999999))
	})
}

func TestShouldReapConfigLockUncovered(t *testing.T) {
	now := time.Now()

	t.Run("corrupt lock file is stale by mtime", func(t *testing.T) {
		oldTime := now.Add(-3 * time.Minute)
		content := []byte("corrupt-data")
		assert.True(t, shouldReapConfigLock(content, oldTime, now))
	})

	t.Run("recent valid lock is not stale", func(t *testing.T) {
		recentNano := now.Add(-30 * time.Second).UnixNano()
		content := []byte("pid=999999999,time=" + strconv.FormatInt(recentNano, 10))
		assert.False(t, shouldReapConfigLock(content, now, now))
	})
}

func TestCloneYAMLNode(t *testing.T) {
	t.Run("nil node returns nil", func(t *testing.T) {
		assert.Nil(t, cloneYAMLNode(nil))
	})

	t.Run("clones simple node", func(t *testing.T) {
		node := &yaml.Node{Kind: yaml.ScalarNode, Value: "test"}
		cloned := cloneYAMLNode(node)
		require.NotNil(t, cloned)
		assert.Equal(t, "test", cloned.Value)
		assert.NotSame(t, node, cloned)
	})
}

func TestMergeYAMLNode(t *testing.T) {
	t.Run("nil nodes are no-op", func(t *testing.T) {
		assert.NotPanics(t, func() { mergeYAMLNode(nil, nil) })
	})

	t.Run("merges mapping nodes", func(t *testing.T) {
		dst := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"}, {Kind: yaml.ScalarNode, Value: "val1"},
		}}
		src := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key2"}, {Kind: yaml.ScalarNode, Value: "val2"},
		}}
		mergeYAMLNode(dst, src)
		// Should have key1+key2
		assert.Len(t, dst.Content, 4)
	})
}

func TestFindMappingValueIndex(t *testing.T) {
	t.Run("nil node returns -1", func(t *testing.T) {
		assert.Equal(t, -1, findMappingValueIndex(nil, "key"))
	})

	t.Run("non-mapping returns -1", func(t *testing.T) {
		node := &yaml.Node{Kind: yaml.ScalarNode}
		assert.Equal(t, -1, findMappingValueIndex(node, "key"))
	})

	t.Run("finds existing key", func(t *testing.T) {
		node := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"}, {Kind: yaml.ScalarNode, Value: "val1"},
			{Kind: yaml.ScalarNode, Value: "key2"}, {Kind: yaml.ScalarNode, Value: "val2"},
		}}
		idx := findMappingValueIndex(node, "key2")
		assert.Equal(t, 3, idx)
	})

	t.Run("returns -1 for missing key", func(t *testing.T) {
		node := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"}, {Kind: yaml.ScalarNode, Value: "val1"},
		}}
		assert.Equal(t, -1, findMappingValueIndex(node, "missing"))
	})
}

func TestApplyNodeMetadataPreservingComments(t *testing.T) {
	t.Run("copies comments from dst when src is empty", func(t *testing.T) {
		dst := &yaml.Node{HeadComment: "head", LineComment: "line", FootComment: "foot", Style: yaml.DoubleQuotedStyle}
		src := &yaml.Node{}
		applyNodeMetadataPreservingComments(dst, src)
		assert.Equal(t, "head", src.HeadComment)
		assert.Equal(t, "line", src.LineComment)
		assert.Equal(t, "foot", src.FootComment)
		assert.Equal(t, yaml.DoubleQuotedStyle, src.Style)
	})

	t.Run("preserves src comments when already set", func(t *testing.T) {
		dst := &yaml.Node{HeadComment: "dst-head"}
		src := &yaml.Node{HeadComment: "src-head"}
		applyNodeMetadataPreservingComments(dst, src)
		assert.Equal(t, "src-head", src.HeadComment)
	})
}

func TestDecodeConfig(t *testing.T) {
	t.Run("empty YAML returns default config", func(t *testing.T) {
		cfg, err := decodeConfig([]byte("{}"))
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})

	t.Run("invalid YAML returns error", func(t *testing.T) {
		_, err := decodeConfig([]byte("not: valid: yaml: [[["))
		assert.Error(t, err)
	})
}

func TestLoad_NonexistentPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent.yaml")
	cfg, err := Load(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Priority = []string{"r18dev", "javdb"}

	err := Save(cfg, path)
	require.NoError(t, err)

	loaded, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, []string{"r18dev", "javdb"}, loaded.Scrapers.Priority)
}

func TestSyncDir(t *testing.T) {
	tmpDir := t.TempDir()
	err := syncDir(tmpDir)
	assert.NoError(t, err)
}

func TestEncodeYAMLDocument(t *testing.T) {
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
		{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key"}, {Kind: yaml.ScalarNode, Value: "value"},
		}},
	}}
	data, err := encodeYAMLDocument(doc)
	require.NoError(t, err)
	assert.Contains(t, string(data), "key")
	assert.Contains(t, string(data), "value")
}

func TestParseYAMLDocument(t *testing.T) {
	t.Run("valid YAML", func(t *testing.T) {
		doc, err := parseYAMLDocument([]byte("key: value\n"))
		require.NoError(t, err)
		assert.Equal(t, yaml.DocumentNode, doc.Kind)
	})

	t.Run("invalid YAML returns error", func(t *testing.T) {
		_, err := parseYAMLDocument([]byte("not: valid: yaml: [[["))
		assert.Error(t, err)
	})
}

func TestConfigToYAMLDocument(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	doc, err := configToYAMLDocument(cfg)
	require.NoError(t, err)
	assert.Equal(t, yaml.DocumentNode, doc.Kind)
}
