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

func TestApplyPlanAPIMatrix(t *testing.T) {
	t.Run("plan-only leave-in-place", func(t *testing.T) {
		plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
		got, projection, err := normalizeScrapePlan(StartScrapeInput{ApplyPlan: plan})
		require.NoError(t, err)
		assert.Equal(t, plan, got)
		assert.True(t, projection.Update)
	})

	t.Run("omitted null and empty review fields", func(t *testing.T) {
		var omitted, nullValue, empty contracts.OrganizeRequest
		require.NoError(t, json.Unmarshal([]byte(`{}`), &omitted))
		require.NoError(t, json.Unmarshal([]byte(`{"destination":null,"operation_mode":null}`), &nullValue))
		require.NoError(t, json.Unmarshal([]byte(`{"destination":"","operation_mode":""}`), &empty))
		assert.False(t, omitted.Has("destination"))
		assert.False(t, nullValue.Has("destination"))
		assert.False(t, nullValue.Has("operation_mode"))
		assert.True(t, empty.Has("destination"))
		assert.True(t, empty.Has("operation_mode"))
	})

	t.Run("dimension-only mirrors", func(t *testing.T) {
		plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
		plan.Merge.ScalarStrategy = applyplan.ScalarPreferScraper
		plan.Merge.ArrayStrategy = applyplan.ArrayReplace
		_, _, err := normalizeScrapePlan(StartScrapeInput{
			ApplyPlan: plan, ScalarStrategy: "prefer-scraper",
			MirrorPresence: planMirrors{scalarPresent: true},
		})
		require.NoError(t, err)
		_, _, err = normalizeScrapePlan(StartScrapeInput{
			ApplyPlan: plan, ArrayStrategy: "replace",
			MirrorPresence: planMirrors{arrayPresent: true},
		})
		require.NoError(t, err)
		_, _, err = normalizeScrapePlan(StartScrapeInput{
			ApplyPlan: plan, ScalarStrategy: "prefer-nfo",
			MirrorPresence: planMirrors{scalarPresent: true},
		})
		assert.ErrorContains(t, err, "scalar_strategy contradicts")
	})

	t.Run("preset mirror takes precedence over strategy mirrors", func(t *testing.T) {
		plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
		plan.Merge.ScalarStrategy = applyplan.ScalarPreferScraper
		plan.Merge.ArrayStrategy = applyplan.ArrayReplace
		plan.Merge.SourcePreset = applyplan.PresetAggressive
		_, _, err := normalizeScrapePlan(StartScrapeInput{
			ApplyPlan: plan, Preset: "aggressive", ScalarStrategy: "prefer-nfo", ArrayStrategy: "merge",
			MirrorPresence: planMirrors{presetPresent: true, scalarPresent: true, arrayPresent: true},
		})
		require.NoError(t, err)
	})

	t.Run("mode contradiction", func(t *testing.T) {
		plan := applyplan.Default(applyplan.VideoOperationRenameFile, "")
		_, _, err := normalizeScrapePlan(StartScrapeInput{
			ApplyPlan: plan, OperationMode: "in-place",
			MirrorPresence: planMirrors{operationPresent: true},
		})
		assert.ErrorContains(t, err, "operation_mode contradicts")
	})

	t.Run("true policies restore to false", func(t *testing.T) {
		plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
		plan.NFOOutput = applyplan.NFOOutputSkip
		plan.MediaPolicy = applyplan.MediaPolicyReplace
		effective, err := effectiveFromOverrides(plan, &contracts.ReviewApplyOverrides{
			SkipNFO: boolPtr(false), SkipDownload: boolPtr(false), OverwriteExistingMedia: boolPtr(false),
		})
		require.NoError(t, err)
		assert.Equal(t, applyplan.NFOOutputWrite, effective.Plan.NFOOutput)
		assert.Equal(t, applyplan.MediaPolicyMissing, effective.Plan.MediaPolicy)
	})

	t.Run("legacy update media contradiction", func(t *testing.T) {
		factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
		_, err := resolveUpdateApplyConfig(core.NewSnapshotForTesting(core.NewAPIRuntime(nil), core.APIConfig{}), factory, &stubControlledJob{}, contracts.UpdateRequest{Overrides: &contracts.ReviewApplyOverrides{SkipDownload: boolPtr(true), OverwriteExistingMedia: boolPtr(true)}})
		assert.ErrorContains(t, err, "cannot both be true")
	})

	t.Run("media contradiction", func(t *testing.T) {
		plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
		_, err := effectiveFromOverrides(plan, &contracts.ReviewApplyOverrides{
			SkipDownload: boolPtr(true), OverwriteExistingMedia: boolPtr(true),
		})
		assert.ErrorContains(t, err, "cannot both be true")
	})

	t.Run("preset strategy contradiction", func(t *testing.T) {
		plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
		_, err := effectiveFromOverrides(plan, &contracts.ReviewApplyOverrides{
			Preset: stringPtr("aggressive"), ScalarStrategy: stringPtr("prefer-nfo"),
		})
		assert.ErrorContains(t, err, "preset contradicts scalar_strategy")
	})

	t.Run("leave-in-place no-op is revalidated", func(t *testing.T) {
		plan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
		_, err := effectiveFromOverrides(plan, &contracts.ReviewApplyOverrides{
			SkipNFO: boolPtr(true), SkipDownload: boolPtr(true),
		})
		assert.ErrorContains(t, err, "no output")
	})
}
