package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func init() {
}

func TestOperationModeConstants(t *testing.T) {
	testCases := []struct {
		name     string
		constant operationmode.OperationMode
		want     string
	}{
		{
			name:     "organize constant",
			constant: operationmode.OperationModeOrganize,
			want:     "organize",
		},
		{
			name:     "in-place constant",
			constant: operationmode.OperationModeInPlace,
			want:     "in-place",
		},
		{
			name:     "metadata-artwork constant",
			constant: operationmode.OperationModeMetadataArtwork,
			want:     "metadata-artwork",
		},
		{
			name:     "preview constant",
			constant: operationmode.OperationModePreview,
			want:     "preview",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, string(tc.constant))
		})
	}
}

func TestIsValidOperationMode(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "organize is valid",
			input: "organize",
			want:  true,
		},
		{
			name:  "in-place is valid",
			input: "in-place",
			want:  true,
		},
		{
			name:  "metadata-artwork is valid",
			input: "metadata-artwork",
			want:  true,
		},
		{
			name:  "preview is valid",
			input: "preview",
			want:  true,
		},
		{
			name:  "invalid mode",
			input: "invalid",
			want:  false,
		},
		{
			name:  "empty string is valid (defaults to organize)",
			input: "",
			want:  true,
		},
		{
			name:  "case insensitive - Organize is valid",
			input: "Organize",
			want:  true,
		},
		{
			name:  "underscore variant is invalid",
			input: "in_place",
			want:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := operationmode.ParseOperationMode(tc.input)
			got := err == nil || tc.input == ""
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOutputConfigOperationModeField(t *testing.T) {
	t.Run("OperationMode field has correct yaml tag", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Output.Operation.OperationMode = operationmode.OperationModeOrganize
		data, err := yaml.Marshal(cfg)
		require.NoError(t, err)
		assert.Contains(t, string(data), "operation_mode:")
	})

	t.Run("empty OperationMode serializes correctly", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Output.Operation.OperationMode = ""
		data, err := yaml.Marshal(cfg)
		require.NoError(t, err)
		assert.Contains(t, string(data), "operation_mode:")
	})

	t.Run("OperationMode organize deserializes correctly", func(t *testing.T) {
		yamlContent := `
output:
  operation_mode: organize
`
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")
		err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
		require.NoError(t, err)

		cfg, err := Load(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, operationmode.OperationMode("organize"), cfg.Output.Operation.OperationMode)
	})

	t.Run("OperationMode in-place deserializes correctly", func(t *testing.T) {
		yamlContent := `
output:
  operation_mode: in-place
`
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")
		err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
		require.NoError(t, err)

		cfg, err := Load(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, operationmode.OperationModeInPlace, cfg.Output.Operation.OperationMode)
	})

	t.Run("default OperationMode is empty string", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		assert.Equal(t, operationmode.OperationMode(""), cfg.Output.Operation.OperationMode)
	})
}

func TestGetOperationMode(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  operationmode.OperationMode
	}{
		{
			name:  "organize returns organize",
			input: "organize",
			want:  operationmode.OperationModeOrganize,
		},
		{
			name:  "in-place returns in-place",
			input: "in-place",
			want:  operationmode.OperationModeInPlace,
		},
		{
			name:  "metadata-artwork returns metadata-artwork",
			input: "metadata-artwork",
			want:  operationmode.OperationModeMetadataArtwork,
		},
		{
			name:  "preview returns preview",
			input: "preview",
			want:  operationmode.OperationModePreview,
		},
		{
			name:  "empty string defaults to organize",
			input: "",
			want:  operationmode.OperationModeOrganize,
		},
		{
			name:  "invalid string defaults to organize",
			input: "invalid",
			want:  operationmode.OperationModeOrganize,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := GetOperationMode(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOutputConfigGetOperationMode(t *testing.T) {
	t.Run("OutputConfig.GetOperationMode delegates to GetOperationMode", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Output.Operation.OperationMode = operationmode.OperationModeInPlace
		got := cfg.Output.GetOperationMode()
		assert.Equal(t, operationmode.OperationModeInPlace, got)
	})

	t.Run("empty OperationMode defaults to organize", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Output.Operation.OperationMode = ""
		got := cfg.Output.GetOperationMode()
		assert.Equal(t, operationmode.OperationModeOrganize, got)
	})
}
