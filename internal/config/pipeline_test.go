package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/types"
)

func init() {
	RegisterTestScraperConfigs()
}

func TestNormalize_EmptyOperationModeDefaultsToOrganize(t *testing.T) {
	testCases := []struct {
		name         string
		explicitMode string
		wantMode     types.OperationMode
		wantChanged  bool
	}{
		{
			name:         "empty OperationMode defaults to organize",
			explicitMode: "",
			wantMode:     types.OperationModeOrganize,
			wantChanged:  true,
		},
		{
			name:         "explicit organize mode not overridden",
			explicitMode: "organize",
			wantMode:     types.OperationModeOrganize,
			wantChanged:  false,
		},
		{
			name:         "explicit in-place mode not overridden",
			explicitMode: "in-place",
			wantMode:     types.OperationModeInPlace,
			wantChanged:  false,
		},
		{
			name:         "explicit preview mode not overridden",
			explicitMode: "preview",
			wantMode:     types.OperationModePreview,
			wantChanged:  false,
		},
		{
			name:         "explicit metadata-only mode not overridden",
			explicitMode: "metadata-only",
			wantMode:     types.OperationModeMetadataOnly,
			wantChanged:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Output.RenameFile = true
			cfg.Output.OperationMode = types.OperationMode(tc.explicitMode)

			modeBefore := cfg.Output.OperationMode
			Normalize(cfg)

			assert.Equal(t, tc.wantMode, cfg.Output.OperationMode, "OperationMode mismatch")

			if tc.wantChanged {
				assert.NotEqual(t, modeBefore, cfg.Output.OperationMode,
					"expected OperationMode to change from %q to %q", modeBefore, cfg.Output.OperationMode)
			} else {
				assert.Equal(t, tc.wantMode, modeBefore,
					"explicit OperationMode should not be changed, was %q before normalization", modeBefore)
			}
		})
	}
}

func TestNormalize_EmptyOperationMode_SetsOrganize(t *testing.T) {
	t.Run("empty OperationMode is defaulted to organize", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Output.OperationMode = ""

		Normalize(cfg)

		assert.Equal(t, types.OperationModeOrganize, cfg.Output.OperationMode, "OperationMode should be defaulted to organize")
	})

	t.Run("already set OperationMode is preserved through normalization", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Output.OperationMode = types.OperationModePreview

		Normalize(cfg)

		assert.Equal(t, types.OperationModePreview, cfg.Output.OperationMode, "explicit mode should not be overridden")
	})
}
