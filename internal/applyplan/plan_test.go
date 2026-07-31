package applyplan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func TestNormalizeAndProject(t *testing.T) {
	tests := []struct {
		name      string
		plan      *Plan
		update    bool
		mode      operationmode.OperationMode
		dest      string
		forceName bool
	}{
		{"organize", Default(VideoOperationOrganize, "/dest"), false, operationmode.OperationModeOrganize, "/dest", false},
		{"rename in place", Default(VideoOperationRenameInPlace, "/stale"), false, operationmode.OperationModeInPlace, "", true},
		{"rename file", Default(VideoOperationRenameFile, "/stale"), false, operationmode.OperationModeInPlaceNoRenameFolder, "", true},
		{"leave in place", Default(VideoOperationLeaveInPlace, "/stale"), true, operationmode.OperationModeMetadataArtwork, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projection, err := Project(tc.plan)
			require.NoError(t, err)
			assert.Equal(t, tc.update, projection.Update)
			assert.Equal(t, tc.mode, projection.OperationMode)
			assert.Equal(t, tc.dest, projection.Destination)
			assert.Equal(t, tc.forceName, projection.ForceRenameFile)
		})
	}
}

func TestValidationMatrix(t *testing.T) {
	tests := []struct {
		name    string
		plan    *Plan
		wantErr bool
	}{
		{"organize valid", Default(VideoOperationOrganize, "/dest"), false},
		{"organize destination required", Default(VideoOperationOrganize, ""), true},
		{"rename outputs skipped remains valid", &Plan{Version: 1, VideoOperation: VideoOperationRenameFile, NFOOutput: NFOOutputSkip, MediaPolicy: MediaPolicySkip}, false},
		{"leave no-op", &Plan{Version: 1, VideoOperation: VideoOperationLeaveInPlace, NFOOutput: NFOOutputSkip, MediaPolicy: MediaPolicySkip, Merge: &MergePolicy{ScalarStrategy: ScalarPreferNFO, ArrayStrategy: ArrayMerge}}, true},
		{"replace organize", &Plan{Version: 1, VideoOperation: VideoOperationOrganize, Destination: "/dest", NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyReplace}, true},
		{"missing version", &Plan{VideoOperation: VideoOperationOrganize, Destination: "/dest", NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyMissing}, true},
		{"unknown version", &Plan{Version: 2, VideoOperation: VideoOperationOrganize, Destination: "/dest", NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyMissing}, true},
		{"unknown operation", &Plan{Version: 1, VideoOperation: "unknown", NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyMissing}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize(tc.plan)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPresetConsistency(t *testing.T) {
	plan := Default(VideoOperationLeaveInPlace, "")
	plan.Merge = &MergePolicy{ScalarStrategy: ScalarFillMissingOnly, ArrayStrategy: ArrayMerge, SourcePreset: PresetGapFill}
	normalized, err := Normalize(plan)
	require.NoError(t, err)
	assert.Equal(t, PresetGapFill, normalized.Merge.SourcePreset)

	plan.Merge.ArrayStrategy = ArrayReplace
	_, err = Normalize(plan)
	assert.Error(t, err)
}

func TestNonUpdateMergeRemoved(t *testing.T) {
	plan := Default(VideoOperationRenameFile, "")
	plan.Merge = &MergePolicy{ScalarStrategy: ScalarPreferScraper, ArrayStrategy: ArrayReplace}
	normalized, err := Normalize(plan)
	require.NoError(t, err)
	assert.Nil(t, normalized.Merge)
}

func TestClone(t *testing.T) {
	plan := Default(VideoOperationLeaveInPlace, "")
	clone := Clone(plan)
	clone.Merge.ScalarStrategy = ScalarPreferScraper
	assert.Equal(t, ScalarPreferNFO, plan.Merge.ScalarStrategy)
}
