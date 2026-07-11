package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveOrganizeApplyConfig_OrganizeSkipGate pins the skip-gate decision
// in resolveOrganizeApplyConfig (apply_config_builder.go). The "Rename file
// only" mode (in-place-norenamefolder) exists specifically to rename the video
// file in place, so the organize step MUST run for it — OrganizeOptions.Skip
// must be false. The same holds for in-place. metadata-artwork genuinely does
// no file operations, so it must keep skipping. preview is rejected at the
// builder (it routes to the preview endpoint), so it never reaches organize.
//
// Regression guard: the original gate was `Skip: effectiveMode != Organize`,
// which skipped organize for EVERY non-organize mode — including
// in-place-norenamefolder — so the file was never renamed despite the strategy
// being correct. This test fails on that gate.
func TestResolveOrganizeApplyConfig_OrganizeSkipGate(t *testing.T) {
	rt := core.NewAPIRuntime(nil) // nil deps: in-place resolution never dereferences deps
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	tests := []struct {
		name      string
		mode      operationmode.OperationMode
		wantSkip  bool
		wantError bool
	}{
		{
			name:     "in-place-norenamefolder runs organize (Rename file only)",
			mode:     operationmode.OperationModeInPlaceNoRenameFolder,
			wantSkip: false,
		},
		{
			name:     "in-place runs organize",
			mode:     operationmode.OperationModeInPlace,
			wantSkip: false,
		},
		{
			name:     "metadata-artwork still skips organize",
			mode:     operationmode.OperationModeMetadataArtwork,
			wantSkip: true,
		},
		{
			name:      "preview rejected at builder (routes to preview endpoint)",
			mode:      operationmode.OperationModePreview,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyOpts, err := resolveOrganizeApplyConfig(
				core.NewSnapshotForTesting(rt, core.APIConfig{}),
				factory,
				job,
				contracts.OrganizeRequest{
					Destination:   "",
					OperationMode: string(tt.mode),
				},
			)

			if tt.wantError {
				require.Error(t, err, "preview must be rejected by the builder")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSkip, applyOpts.OrganizeOptions.Skip,
				"OrganizeOptions.Skip for mode %q", tt.mode)
		})
	}
}
