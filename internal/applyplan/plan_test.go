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
		{"metadata artwork", Default(VideoOperationMetadataArtwork, "/stale"), false, operationmode.OperationModeMetadataArtwork, "", false},
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

func TestMetadataArtworkPoliciesProject(t *testing.T) {
	plan := Default(VideoOperationMetadataArtwork, "")
	plan.NFOOutput = NFOOutputSkip
	projection, err := Project(plan)
	require.NoError(t, err)
	assert.False(t, projection.Update)
	assert.Equal(t, operationmode.OperationModeMetadataArtwork, projection.OperationMode)
	assert.True(t, projection.SkipNFO)
	assert.False(t, projection.SkipDownload)
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
		{"metadata artwork no-op", &Plan{Version: 1, VideoOperation: VideoOperationMetadataArtwork, NFOOutput: NFOOutputSkip, MediaPolicy: MediaPolicySkip}, true},
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

func TestPresetStrategies(t *testing.T) {
	tests := []struct {
		name    string
		preset  Preset
		scalar  ScalarStrategy
		array   ArrayStrategy
		wantErr bool
	}{
		{"conservative", PresetConservative, ScalarPreserveExisting, ArrayMerge, false},
		{"gap-fill", PresetGapFill, ScalarFillMissingOnly, ArrayMerge, false},
		{"aggressive", PresetAggressive, ScalarPreferScraper, ArrayReplace, false},
		{"unknown", Preset("unknown"), "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scalar, array, err := PresetStrategies(tc.preset)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.scalar, scalar)
			assert.Equal(t, tc.array, array)
		})
	}
}

func TestNormalizeDefaults(t *testing.T) {
	got, err := Normalize(nil)
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = Normalize(&Plan{Version: Version1, VideoOperation: VideoOperationLeaveInPlace})
	require.NoError(t, err)
	assert.Equal(t, NFOOutputWrite, got.NFOOutput)
	assert.Equal(t, MediaPolicyMissing, got.MediaPolicy)
	assert.Equal(t, ScalarPreferNFO, got.Merge.ScalarStrategy)
	assert.Equal(t, ArrayMerge, got.Merge.ArrayStrategy)

	got, err = Normalize(&Plan{Version: Version1, VideoOperation: VideoOperationLeaveInPlace, Merge: &MergePolicy{}})
	require.NoError(t, err)
	assert.Equal(t, ScalarPreferNFO, got.Merge.ScalarStrategy)
	assert.Equal(t, ArrayMerge, got.Merge.ArrayStrategy)

	got, err = Normalize(&Plan{Version: Version1, VideoOperation: VideoOperationRenameFile, Destination: "  /stale  "})
	require.NoError(t, err)
	assert.Empty(t, got.Destination)
	assert.Equal(t, NFOOutputWrite, got.NFOOutput)
	assert.Equal(t, MediaPolicyMissing, got.MediaPolicy)

	invalidPreset := Default(VideoOperationLeaveInPlace, "")
	invalidPreset.Merge.SourcePreset = Preset("unknown")
	_, err = Normalize(invalidPreset)
	assert.Error(t, err)
}

func TestValidateBranches(t *testing.T) {
	tests := []struct {
		name string
		plan *Plan
	}{
		{"nil", nil},
		{"destination on non-organize", &Plan{Version: Version1, VideoOperation: VideoOperationRenameFile, Destination: "/dest", NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyMissing}},
		{"invalid nfo", &Plan{Version: Version1, VideoOperation: VideoOperationOrganize, Destination: "/dest", NFOOutput: "bad", MediaPolicy: MediaPolicyMissing}},
		{"invalid media", &Plan{Version: Version1, VideoOperation: VideoOperationOrganize, Destination: "/dest", NFOOutput: NFOOutputWrite, MediaPolicy: "bad"}},
		{"leave without merge", &Plan{Version: Version1, VideoOperation: VideoOperationLeaveInPlace, NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyMissing}},
		{"invalid scalar", &Plan{Version: Version1, VideoOperation: VideoOperationLeaveInPlace, NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyMissing, Merge: &MergePolicy{ScalarStrategy: "bad", ArrayStrategy: ArrayMerge}}},
		{"invalid array", &Plan{Version: Version1, VideoOperation: VideoOperationLeaveInPlace, NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyMissing, Merge: &MergePolicy{ScalarStrategy: ScalarPreferNFO, ArrayStrategy: "bad"}}},
		{"merge on organize", &Plan{Version: Version1, VideoOperation: VideoOperationOrganize, Destination: "/dest", NFOOutput: NFOOutputWrite, MediaPolicy: MediaPolicyMissing, Merge: &MergePolicy{ScalarStrategy: ScalarPreferNFO, ArrayStrategy: ArrayMerge}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, Validate(tc.plan))
		})
	}
}

func TestProjectInvalidPlan(t *testing.T) {
	_, err := Project(&Plan{Version: Version1, VideoOperation: "bad"})
	assert.Error(t, err)
}

func TestFromLegacy(t *testing.T) {
	got, err := FromLegacy(true, "", "ignored", ScalarFillMissingOnly, ArrayReplace)
	require.NoError(t, err)
	assert.Equal(t, VideoOperationLeaveInPlace, got.VideoOperation)
	assert.Equal(t, ScalarFillMissingOnly, got.Merge.ScalarStrategy)
	assert.Equal(t, ArrayReplace, got.Merge.ArrayStrategy)

	cases := []struct {
		name string
		mode operationmode.OperationMode
		want VideoOperation
	}{
		{"organize", operationmode.OperationModeOrganize, VideoOperationOrganize},
		{"empty", "", VideoOperationOrganize},
		{"in-place", operationmode.OperationModeInPlace, VideoOperationRenameInPlace},
		{"rename-file", operationmode.OperationModeInPlaceNoRenameFolder, VideoOperationRenameFile},
		{"metadata-artwork", operationmode.OperationModeMetadataArtwork, VideoOperationMetadataArtwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FromLegacy(false, tc.mode, "/dest", "", "")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.VideoOperation)
		})
	}

	_, err = FromLegacy(false, operationmode.OperationModePreview, "", "", "")
	assert.Error(t, err)
}
