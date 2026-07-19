package config

import (
	"errors"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type countingFs struct {
	afero.Fs
	openFailAfter   int
	renameFailAfter int
	openCount       int
	renameCount     int
}

func (f *countingFs) Open(name string) (afero.File, error) {
	f.openCount++
	if f.openFailAfter >= 0 && f.openCount > f.openFailAfter {
		return nil, errors.New("open boom")
	}
	return f.Fs.Open(name)
}

func (f *countingFs) Rename(old, new string) error {
	f.renameCount++
	if f.renameFailAfter >= 0 && f.renameCount > f.renameFailAfter {
		return errors.New("rename boom")
	}
	return f.Fs.Rename(old, new)
}

func errLockFactory(string) (func(), error) {
	return nil, errors.New("lock boom")
}

func TestNodesEqual_EncodeErrorOnA(t *testing.T) {
	bad := &yaml.Node{Kind: yaml.AliasNode, Alias: nil}
	good := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
	assert.False(t, nodesEqual(bad, good))
}

func TestNodesEqual_EncodeErrorOnB(t *testing.T) {
	bad := &yaml.Node{Kind: yaml.AliasNode, Alias: nil}
	good := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
	assert.False(t, nodesEqual(good, bad))
}

func TestReconcileMappings_MappingValueReplacedWhenUnknown(t *testing.T) {
	dst := mustParseYAML(t, "custom:\n    a: 1\n")
	src := mustParseYAML(t, "custom:\n    b: 2\n")
	reconcileSparse(dst, src, nil, nil)
	root := mappingRoot(dst)
	require.NotNil(t, root)
	customIdx := findMappingValueIndex(root, "custom")
	require.NotEqual(t, -1, customIdx)
	custom := root.Content[customIdx]
	bIdx := findMappingValueIndex(custom, "b")
	require.NotEqual(t, -1, bIdx)
	assert.Equal(t, "2", custom.Content[bIdx].Value)
	assert.Equal(t, -1, findMappingValueIndex(custom, "a"))
}

func TestNewConfigStorage_NilFsUsesOsFs(t *testing.T) {
	cs := NewConfigStorage(nil, noopLockFactory)
	require.NotNil(t, cs)
	_, isOs := cs.fs.(*afero.OsFs)
	assert.True(t, isOs, "nil fs should fall back to afero.NewOsFs")
}

func TestNewConfigStorage_NilLockFactoryFallsBackToRealLock(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	cs := NewConfigStorage(afero.NewOsFs(), nil)
	require.NotNil(t, cs)
	assert.Nil(t, cs.lockFactory)
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "warn"
	require.NoError(t, cs.SaveSparse(cfg, path, BuildSparseSaveContext()))
	loaded, err := cs.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "warn", loaded.Logging.Level)
}

func TestSaveSparse_Method_AcquireLockErrorReturnsError(t *testing.T) {
	cs := NewConfigStorage(afero.NewMemMapFs(), errLockFactory)
	cfg := DefaultConfig(nil, nil)
	err := cs.SaveSparse(cfg, "/cfg/config.yaml", BuildSparseSaveContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock boom")
}

func TestSaveSparse_Method_AtomicReplaceErrorReturnsError(t *testing.T) {
	base := afero.NewMemMapFs()
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: -1, renameFailAfter: 0}, noopLockFactory)
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "info"
	err := cs.SaveSparse(cfg, "/cfg/config.yaml", BuildSparseSaveContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write sparse config")
}

func TestCreateFromEmbedded_LoadFailsReturnsError(t *testing.T) {
	base := afero.NewMemMapFs()
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: 0, renameFailAfter: -1}, noopLockFactory)
	_, err := cs.createFromEmbedded("/cfg/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load newly created config")
}

func TestCreateFromEmbedded_SaveSparseFailsReturnsError(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	base := afero.NewMemMapFs()
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: -1, renameFailAfter: 1}, noopLockFactory)
	_, err := cs.createFromEmbedded("/cfg/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save config with environment overrides")
}

func TestCreateFromEmbedded_ReloadFailsReturnsError(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	base := afero.NewMemMapFs()
	cs := NewConfigStorage(&countingFs{Fs: base, openFailAfter: 2, renameFailAfter: -1}, noopLockFactory)
	_, err := cs.createFromEmbedded("/cfg/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to re-load config")
}

func TestNormalize_EmptyLoggingOutputSetsDefault(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Output = ""
	changed := normalize(cfg)
	assert.True(t, changed)
	assert.NotEmpty(t, cfg.Logging.Output)
}

func TestPrepareForPersistence_NilConfigReturnsFalseNil(t *testing.T) {
	changed, err := PrepareForPersistence(nil)
	assert.NoError(t, err)
	assert.False(t, changed)
}

func TestPrepare_PrepareRuntimeErrorPropagates(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	_, _ = Prepare(cfg)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "openai"
	cfg.Metadata.Translation.OpenAI.APIKey = ""
	changed, err := Prepare(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
	_ = changed
}

func TestValidateConfigExcludingTranslationCredentials_TranslationProviderError(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "bogus"
	err := validateConfigExcludingTranslationCredentials(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "translation.provider")
}

type reloadInvalidFs struct {
	afero.Fs
	invalidFs  afero.Fs
	openCount  int
	failOpenAt int
}

func (f *reloadInvalidFs) Open(name string) (afero.File, error) {
	f.openCount++
	if f.openCount == f.failOpenAt {
		return f.invalidFs.Open(name)
	}
	return f.Fs.Open(name)
}

func TestCreateFromEmbedded_PrepareFailsReturnsError(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
	path := "/cfg/config.yaml"
	invalid := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(invalid, path,
		[]byte("config_version: "+strconv.Itoa(CurrentConfigVersion)+"\ndatabase:\n    type: postgres\n"), 0o644))
	base := afero.NewMemMapFs()
	wrapper := &reloadInvalidFs{Fs: base, invalidFs: invalid, failOpenAt: 3}
	cs := NewConfigStorage(wrapper, noopLockFactory)
	_, err := cs.createFromEmbedded(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.type must be 'sqlite'")
}
