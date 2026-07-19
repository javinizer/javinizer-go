package config

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDiffYAMLDocumentsWith_ActualDocumentError(t *testing.T) {
	expected := errors.New("marshal failed")
	_, err := diffYAMLDocumentsWith(
		DefaultConfig(nil, nil),
		DefaultConfig(nil, nil),
		func(*Config) (*yaml.Node, error) { return nil, expected },
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
}

func TestDiffYAMLDocumentsWith_DefaultDocumentError(t *testing.T) {
	expected := errors.New("marshal failed")
	calls := 0
	_, err := diffYAMLDocumentsWith(
		DefaultConfig(nil, nil),
		DefaultConfig(nil, nil),
		func(cfg *Config) (*yaml.Node, error) {
			calls++
			if calls == 2 {
				return nil, expected
			}
			return configToYAMLDocument(cfg)
		},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
	assert.Equal(t, 2, calls)
}

func TestBuildSparseSaveContextWithNames_DocumentError(t *testing.T) {
	expected := errors.New("marshal failed")
	ctx, err := buildSparseSaveContextWithScrapers(
		nil,
		nil,
		func(*Config) (*yaml.Node, error) { return nil, expected },
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
	assert.Equal(t, SparseSaveContext{}, ctx)
}

func TestBuildScraperSettingsSchemaWith_MarshalError(t *testing.T) {
	expected := errors.New("marshal failed")
	schema := buildScraperSettingsSchemaWith(
		func(any) ([]byte, error) { return nil, expected },
		func([]byte) (*yaml.Node, error) { return nil, nil },
	)
	assert.Nil(t, schema)
}

func TestBuildScraperSettingsSchemaWith_ParseError(t *testing.T) {
	expected := errors.New("parse failed")
	schema := buildScraperSettingsSchemaWith(
		yaml.Marshal,
		func([]byte) (*yaml.Node, error) { return nil, expected },
	)
	assert.Nil(t, schema)
}

func TestMustSparseSaveContext_PanicsOnError(t *testing.T) {
	expected := errors.New("build failed")
	assert.PanicsWithValue(t, expected, func() {
		mustSparseSaveContext(SparseSaveContext{}, expected)
	})
}

func TestMustSparseSaveContext_ReturnsContext(t *testing.T) {
	ctx := SparseSaveContext{KnownScraperNames: map[string]bool{"dmm": true}}
	result := mustSparseSaveContext(ctx, nil)
	assert.Equal(t, ctx, result)
}

func TestDiffYAMLDocumentsWith_NonMappingRoot(t *testing.T) {
	scalarDoc := &yaml.Node{Kind: yaml.ScalarNode, Value: "test"}
	_, err := diffYAMLDocumentsWith(
		DefaultConfig(nil, nil),
		DefaultConfig(nil, nil),
		func(*Config) (*yaml.Node, error) { return scalarDoc, nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected mapping document roots")
}

func TestConfigToYAMLDocumentWith_MarshalError(t *testing.T) {
	expected := errors.New("marshal failed")
	_, err := configToYAMLDocumentWith(
		DefaultConfig(nil, nil),
		func(any) ([]byte, error) { return nil, expected },
		yaml.Unmarshal,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
	assert.Contains(t, err.Error(), "failed to marshal config")
}

func TestConfigToYAMLDocumentWith_UnmarshalError(t *testing.T) {
	expected := errors.New("unmarshal failed")
	_, err := configToYAMLDocumentWith(
		DefaultConfig(nil, nil),
		yaml.Marshal,
		func([]byte, any) error { return expected },
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
	assert.Contains(t, err.Error(), "failed to parse marshaled config")
}

func TestConfigToYAMLDocumentWith_NonDocumentResult(t *testing.T) {
	// A no-op unmarshal leaves the node at its zero value (Kind 0), which is not a
	// DocumentNode, exercising the invalid-document guard branch.
	_, err := configToYAMLDocumentWith(
		DefaultConfig(nil, nil),
		yaml.Marshal,
		func([]byte, any) error { return nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid marshaled YAML document")
}

// failingEncoder is a yamlEncoder mock whose Encode and/or Close fail on demand.
type failingEncoder struct {
	encodeErr error
	closeErr  error
}

func (e *failingEncoder) Encode(any) error { return e.encodeErr }
func (e *failingEncoder) Close() error     { return e.closeErr }

func TestEncodeYAMLDocumentWith_EncodeError(t *testing.T) {
	expected := errors.New("encode failed")
	factory := func(io.Writer) yamlEncoder {
		return &failingEncoder{encodeErr: expected}
	}
	var buf bytes.Buffer
	err := encodeYAMLDocumentWith(&buf, &yaml.Node{Kind: yaml.DocumentNode}, factory)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
	assert.Contains(t, err.Error(), "failed to encode YAML document")
}

func TestEncodeYAMLDocumentWith_CloseError(t *testing.T) {
	expected := errors.New("close failed")
	factory := func(io.Writer) yamlEncoder {
		return &failingEncoder{closeErr: expected}
	}
	var buf bytes.Buffer
	err := encodeYAMLDocumentWith(&buf, &yaml.Node{Kind: yaml.DocumentNode}, factory)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
	assert.Contains(t, err.Error(), "failed to finalize YAML encoding")
}

func TestEncodeYAMLDocumentWith_Success(t *testing.T) {
	doc, err := configToYAMLDocument(DefaultConfig(nil, nil))
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, encodeYAMLDocumentWith(&buf, doc, defaultYAMLEncoderFactory))
	assert.NotEmpty(t, buf.Bytes())
}

func TestSaveSparseWith_DiffError(t *testing.T) {
	cs := NewConfigStorage(afero.NewMemMapFs(), func(string) (func(), error) { return func() {}, nil })
	expected := errors.New("diff failed")
	err := cs.saveSparseWith(
		DefaultConfig(nil, nil),
		"/cfg/config.yml",
		BuildSparseSaveContext(),
		func(*Config) (*yaml.Node, error) { return nil, expected },
		encodeYAMLDocument,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
	assert.Contains(t, err.Error(), "failed to diff config")
}

func TestSaveSparseWith_EncodeError(t *testing.T) {
	cs := NewConfigStorage(afero.NewMemMapFs(), func(string) (func(), error) { return func() {}, nil })
	expected := errors.New("encode failed")
	err := cs.saveSparseWith(
		DefaultConfig(nil, nil),
		"/cfg/config.yml",
		BuildSparseSaveContext(),
		configToYAMLDocument,
		func(*yaml.Node) ([]byte, error) { return nil, expected },
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
	assert.Contains(t, err.Error(), "failed to encode config")
}

// renameFailingFs wraps an afero.Fs but always fails Rename so atomicReplace's
// final rename step errors out.
type renameFailingFs struct {
	afero.Fs
}

func (f *renameFailingFs) Rename(oldname, newname string) error {
	return errors.New("rename not allowed")
}

func TestSaveSparseWith_AtomicReplaceError(t *testing.T) {
	cs := NewConfigStorage(&renameFailingFs{Fs: afero.NewMemMapFs()}, func(string) (func(), error) { return func() {}, nil })
	err := cs.SaveSparse(DefaultConfig(nil, nil), "/cfg/config.yml", BuildSparseSaveContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write sparse config")
}
