package config

import (
	"errors"
	"testing"

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
	ctx, err := buildSparseSaveContextWithNames(
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
