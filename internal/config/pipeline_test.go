package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func init() {
}

func TestNormalize_EmptyOperationModeDefaultsToOrganize(t *testing.T) {
	testCases := []struct {
		name         string
		explicitMode string
		wantMode     operationmode.OperationMode
		wantChanged  bool
	}{
		{
			name:         "empty OperationMode defaults to organize",
			explicitMode: "",
			wantMode:     operationmode.OperationModeOrganize,
			wantChanged:  true,
		},
		{
			name:         "explicit organize mode not overridden",
			explicitMode: "organize",
			wantMode:     operationmode.OperationModeOrganize,
			wantChanged:  false,
		},
		{
			name:         "explicit in-place mode not overridden",
			explicitMode: "in-place",
			wantMode:     operationmode.OperationModeInPlace,
			wantChanged:  false,
		},
		{
			name:         "explicit preview mode not overridden",
			explicitMode: "preview",
			wantMode:     operationmode.OperationModePreview,
			wantChanged:  false,
		},
		{
			name:         "explicit metadata-artwork mode not overridden",
			explicitMode: "metadata-artwork",
			wantMode:     operationmode.OperationModeMetadataArtwork,
			wantChanged:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig(nil, nil)
			cfg.Output.Operation.RenameFile = true
			cfg.Output.Operation.OperationMode = operationmode.OperationMode(tc.explicitMode)

			modeBefore := cfg.Output.Operation.OperationMode
			normalize(cfg)

			assert.Equal(t, tc.wantMode, cfg.Output.Operation.OperationMode, "OperationMode mismatch")

			if tc.wantChanged {
				assert.NotEqual(t, modeBefore, cfg.Output.Operation.OperationMode,
					"expected OperationMode to change from %q to %q", modeBefore, cfg.Output.Operation.OperationMode)
			} else {
				assert.Equal(t, tc.wantMode, modeBefore,
					"explicit OperationMode should not be changed, was %q before normalization", modeBefore)
			}
		})
	}
}

func TestNormalize_EmptyOperationMode_SetsOrganize(t *testing.T) {
	t.Run("empty OperationMode is defaulted to organize", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Output.Operation.OperationMode = ""

		normalize(cfg)

		assert.Equal(t, operationmode.OperationModeOrganize, cfg.Output.Operation.OperationMode, "OperationMode should be defaulted to organize")
	})

	t.Run("already set OperationMode is preserved through normalization", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.Output.Operation.OperationMode = operationmode.OperationModePreview

		normalize(cfg)

		assert.Equal(t, operationmode.OperationModePreview, cfg.Output.Operation.OperationMode, "explicit mode should not be overridden")
	})
}
