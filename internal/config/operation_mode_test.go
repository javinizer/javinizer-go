package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/javinizer/javinizer-go/internal/types"
)

func init() {
	RegisterTestScraperConfigs()
}

func TestOperationModeConstants(t *testing.T) {
	testCases := []struct {
		name     string
		constant types.OperationMode
		want     string
	}{
		{
			name:     "organize constant",
			constant: types.OperationModeOrganize,
			want:     "organize",
		},
		{
			name:     "in-place constant",
			constant: types.OperationModeInPlace,
			want:     "in-place",
		},
		{
			name:     "metadata-artwork constant",
			constant: types.OperationModeMetadataArtwork,
			want:     "metadata-artwork",
		},
		{
			name:     "preview constant",
			constant: types.OperationModePreview,
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
			name:  "empty string is invalid",
			input: "",
			want:  false,
		},
		{
			name:  "case sensitive - Organize is invalid",
			input: "Organize",
			want:  false,
		},
		{
			name:  "underscore variant is invalid",
			input: "in_place",
			want:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := types.IsValidOperationMode(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOutputConfigOperationModeField(t *testing.T) {
	t.Run("OperationMode field has correct yaml tag", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Output.OperationMode = types.OperationModeOrganize
		data, err := yaml.Marshal(cfg)
		require.NoError(t, err)
		assert.Contains(t, string(data), "operation_mode:")
	})

	t.Run("empty OperationMode serializes correctly", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Output.OperationMode = ""
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
		assert.Equal(t, types.OperationMode("organize"), cfg.Output.OperationMode)
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
		assert.Equal(t, types.OperationModeInPlace, cfg.Output.OperationMode)
	})

	t.Run("default OperationMode is empty string", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.Equal(t, types.OperationMode(""), cfg.Output.OperationMode)
	})
}

func TestGetOperationMode(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  types.OperationMode
	}{
		{
			name:  "organize returns organize",
			input: "organize",
			want:  types.OperationModeOrganize,
		},
		{
			name:  "in-place returns in-place",
			input: "in-place",
			want:  types.OperationModeInPlace,
		},
		{
			name:  "metadata-artwork returns metadata-artwork",
			input: "metadata-artwork",
			want:  types.OperationModeMetadataArtwork,
		},
		{
			name:  "preview returns preview",
			input: "preview",
			want:  types.OperationModePreview,
		},
		{
			name:  "empty string defaults to organize",
			input: "",
			want:  types.OperationModeOrganize,
		},
		{
			name:  "invalid string defaults to organize",
			input: "invalid",
			want:  types.OperationModeOrganize,
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
		cfg := DefaultConfig()
		cfg.Output.OperationMode = types.OperationModeInPlace
		got := cfg.Output.GetOperationMode()
		assert.Equal(t, types.OperationModeInPlace, got)
	})

	t.Run("empty OperationMode defaults to organize", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Output.OperationMode = ""
		got := cfg.Output.GetOperationMode()
		assert.Equal(t, types.OperationModeOrganize, got)
	})
}
