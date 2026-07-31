package batch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/worker"
)

func stringPtr(value string) *string { return &value }
func boolPtr(value bool) *bool       { return &value }

func statusWithPlan(t *testing.T, plan *applyplan.Plan) *worker.BatchJobStatus {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"apply_plan": plan})
	require.NoError(t, err)
	var status worker.BatchJobStatus
	require.NoError(t, json.Unmarshal(payload, &status))
	return &status
}

func TestBatchScrapeRequestPresence(t *testing.T) {
	var req contracts.BatchScrapeRequest
	require.NoError(t, json.Unmarshal([]byte(`{"files":["a.mp4"],"update":false,"destination":"","preset":null}`), &req))
	assert.True(t, req.Has("update"))
	assert.True(t, req.Has("destination"))
	assert.False(t, req.Has("preset"))
	assert.False(t, req.Has("operation_mode"))
}

func TestNormalizeScrapePlanMirrors(t *testing.T) {
	plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
	update := false
	_, _, err := normalizeScrapePlan(StartScrapeInput{
		ApplyPlan:      plan,
		Update:         &update,
		MirrorPresence: planMirrors{updatePresent: true},
	})
	assert.ErrorContains(t, err, "update contradicts")

	plan.Merge.ScalarStrategy = applyplan.ScalarPreferScraper
	plan.Merge.ArrayStrategy = applyplan.ArrayReplace
	_, _, err = normalizeScrapePlan(StartScrapeInput{
		ApplyPlan:      plan,
		ScalarStrategy: "prefer-scraper",
		MirrorPresence: planMirrors{scalarPresent: true},
	})
	require.NoError(t, err)
}

func TestEffectiveFromOverrides(t *testing.T) {
	base := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
	base.MediaPolicy = applyplan.MediaPolicyReplace

	effective, err := effectiveFromOverrides(base, &contracts.ReviewApplyOverrides{
		SkipDownload: boolPtr(false), OverwriteExistingMedia: boolPtr(false),
		ScalarStrategy: stringPtr("prefer-scraper"), ArrayStrategy: stringPtr("replace"),
	})
	require.NoError(t, err)
	assert.Equal(t, applyplan.MediaPolicyMissing, effective.Plan.MediaPolicy)
	assert.Equal(t, applyplan.ScalarPreferScraper, effective.Plan.Merge.ScalarStrategy)
	assert.Empty(t, effective.Plan.Merge.SourcePreset)

	_, err = effectiveFromOverrides(base, &contracts.ReviewApplyOverrides{
		SkipDownload: boolPtr(true), OverwriteExistingMedia: boolPtr(true),
	})
	assert.ErrorContains(t, err, "cannot both be true")
}

func TestReviewLegacyFieldsMergeByPresence(t *testing.T) {
	var req contracts.UpdateRequest
	require.NoError(t, json.Unmarshal([]byte(`{"skip_download":false,"overwrite_existing_media":false,"preset":""}`), &req))
	overrides, err := reviewOverridesForUpdate(req)
	require.NoError(t, err)
	require.NotNil(t, overrides.SkipDownload)
	assert.False(t, *overrides.SkipDownload)
	require.NotNil(t, overrides.Preset)

	base := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
	base.MediaPolicy = applyplan.MediaPolicyReplace
	base.Merge.ScalarStrategy = applyplan.ScalarPreferScraper
	base.Merge.ArrayStrategy = applyplan.ArrayReplace
	effective, err := effectiveFromOverrides(base, overrides)
	require.NoError(t, err)
	assert.Equal(t, applyplan.MediaPolicyMissing, effective.Plan.MediaPolicy)
	assert.Equal(t, applyplan.ScalarPreferNFO, effective.Plan.Merge.ScalarStrategy)
	assert.Equal(t, applyplan.ArrayMerge, effective.Plan.Merge.ArrayStrategy)
	assert.Empty(t, effective.Plan.Merge.SourcePreset)
}

func TestReviewLegacyAndNestedConflict(t *testing.T) {
	var req contracts.OrganizeRequest
	require.NoError(t, json.Unmarshal([]byte(`{"operation_mode":"in-place","overrides":{"operation_mode":"in-place-norenamefolder"}}`), &req))
	_, err := reviewOverridesForOrganize(req)
	assert.ErrorContains(t, err, "conflicts")
}
func TestEffectiveFromOverrides_RejectsMergeOverridesForNonUpdate(t *testing.T) {
	base := applyplan.Default(applyplan.VideoOperationOrganize, "/dest")
	_, err := effectiveFromOverrides(base, &contracts.ReviewApplyOverrides{ScalarStrategy: stringPtr("prefer-nfo")})
	assert.ErrorContains(t, err, "only for leave-in-place")
	_, err = effectiveFromOverrides(base, &contracts.ReviewApplyOverrides{ForceOverwrite: boolPtr(true)})
	assert.ErrorContains(t, err, "only for leave-in-place")
}

func TestEffectiveMergeOverridePrecedence(t *testing.T) {
	base := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
	effective, err := effectiveFromOverrides(base, &contracts.ReviewApplyOverrides{ForceOverwrite: boolPtr(true)})
	require.NoError(t, err)
	assert.Equal(t, applyplan.MergeOverrideForceOverwrite, effective.MergeOverride)

	_, err = effectiveFromOverrides(base, &contracts.ReviewApplyOverrides{ForceOverwrite: boolPtr(true), PreserveNFO: boolPtr(true)})
	assert.Error(t, err)
}

func TestResolveUpdateApplyConfig_LegacyJobUsesNestedReviewOverrides(t *testing.T) {
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	cfg, err := resolveUpdateApplyConfig(
		core.NewSnapshotForTesting(core.NewAPIRuntime(nil), core.APIConfig{}),
		factory,
		&stubControlledJob{},
		contracts.UpdateRequest{Overrides: &contracts.ReviewApplyOverrides{
			SkipNFO: boolPtr(true), SkipDownload: boolPtr(true),
			OverwriteExistingMedia: boolPtr(false),
			ScalarStrategy:         stringPtr("prefer-scraper"), ArrayStrategy: stringPtr("replace"),
			ForceOverwrite: boolPtr(false), PreserveNFO: boolPtr(false),
		}},
	)
	require.NoError(t, err)
	assert.False(t, cfg.GenerateNFO)
	assert.False(t, cfg.Download)
	assert.False(t, cfg.OverwriteExistingMedia)
	assert.Equal(t, "prefer-scraper", string(cfg.MergeOptions.ScalarStrategy))
	assert.False(t, cfg.MergeOptions.ArrayStrategy)
}
func TestResolveUpdateApplyConfig_UsesPersistedPlan(t *testing.T) {
	plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
	plan.Merge.ScalarStrategy = applyplan.ScalarPreferScraper
	plan.Merge.ArrayStrategy = applyplan.ArrayReplace
	plan.MediaPolicy = applyplan.MediaPolicyReplace
	plan.NFOOutput = applyplan.NFOOutputSkip
	job := &stubControlledJob{status: statusWithPlan(t, plan)}
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)

	cfg, err := resolveUpdateApplyConfig(core.NewSnapshotForTesting(core.NewAPIRuntime(nil), core.APIConfig{}), factory, job, contracts.UpdateRequest{})
	require.NoError(t, err)
	assert.Equal(t, "prefer-scraper", string(cfg.MergeOptions.ScalarStrategy))
	assert.False(t, cfg.MergeOptions.ArrayStrategy)
	assert.True(t, cfg.OverwriteExistingMedia)
	assert.False(t, cfg.GenerateNFO)
	assert.True(t, cfg.Download)
}

func TestResolveApplyConfig_RejectsEndpointConflict(t *testing.T) {
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	snapshot := core.NewSnapshotForTesting(core.NewAPIRuntime(nil), core.APIConfig{})

	updateJob := &stubControlledJob{status: statusWithPlan(t, applyplan.Default(applyplan.VideoOperationRenameFile, ""))}
	_, err := resolveUpdateApplyConfig(snapshot, factory, updateJob, contracts.UpdateRequest{})
	assert.ErrorIs(t, err, ErrApplyEndpointConflict)

	organizeJob := &stubControlledJob{status: statusWithPlan(t, applyplan.Default(applyplan.VideoOperationLeaveInPlace, ""))}
	_, err = resolveOrganizeApplyConfig(snapshot, factory, organizeJob, contracts.OrganizeRequest{})
	assert.ErrorIs(t, err, ErrApplyEndpointConflict)
}
func TestEffectiveEndpointOperationContradiction(t *testing.T) {
	base := applyplan.Default(applyplan.VideoOperationRenameInPlace, "")
	_, err := effectiveFromOverrides(base, &contracts.ReviewApplyOverrides{OperationMode: stringPtr("in-place-norenamefolder")})
	assert.ErrorContains(t, err, "contradicts")
}
