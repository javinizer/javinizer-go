package worker_test

import (
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
)

func TestBatchScrapeRequest_HasNoLegacyBooleanFields(t *testing.T) {
	reqType := reflect.TypeOf(contracts.BatchScrapeRequest{})

	_, hasMoveToFolder := reqType.FieldByName("MoveToFolder")
	assert.False(t, hasMoveToFolder,
		"BatchScrapeRequest should not have MoveToFolder field — removed in LGCY-01")

	_, hasRenameFolderInPlace := reqType.FieldByName("RenameFolderInPlace")
	assert.False(t, hasRenameFolderInPlace,
		"BatchScrapeRequest should not have RenameFolderInPlace field — removed in LGCY-01")

	_, hasOperationMode := reqType.FieldByName("OperationMode")
	assert.True(t, hasOperationMode,
		"BatchScrapeRequest must have OperationMode field — the replacement for legacy boolean fields")
}

func TestNFOComparisonRequest_HasNoMergeStrategy(t *testing.T) {
	reqType := reflect.TypeOf(contracts.NFOComparisonRequest{})

	_, hasMergeStrategy := reqType.FieldByName("MergeStrategy")
	assert.False(t, hasMergeStrategy,
		"NFOComparisonRequest should not have MergeStrategy field — removed in DEAD-03")

	_, hasPreset := reqType.FieldByName("Preset")
	assert.True(t, hasPreset,
		"NFOComparisonRequest must have Preset field — replacement for MergeStrategy")

	_, hasScalarStrategy := reqType.FieldByName("ScalarStrategy")
	assert.True(t, hasScalarStrategy,
		"NFOComparisonRequest must have ScalarStrategy field — replacement for MergeStrategy")

	_, hasArrayStrategy := reqType.FieldByName("ArrayStrategy")
	assert.True(t, hasArrayStrategy,
		"NFOComparisonRequest must have ArrayStrategy field — replacement for MergeStrategy")
}

// Verify BatchJob and batchJobSlim legacy field checks still work from internal test.
// These tests were moved to this external test package because contracts now imports worker,
// which creates an import cycle when the worker test package imports contracts.
func TestBatchJob_ReflectChecksFromExternal(t *testing.T) {
	jobType := reflect.TypeOf(worker.BatchJob{})

	_, hasMoveToFolderOverride := jobType.FieldByName("MoveToFolderOverride")
	assert.False(t, hasMoveToFolderOverride,
		"BatchJob should not have MoveToFolderOverride field — removed in LGCY-01")

	_, hasRenameFolderInPlaceOverride := jobType.FieldByName("RenameFolderInPlaceOverride")
	assert.False(t, hasRenameFolderInPlaceOverride,
		"BatchJob should not have RenameFolderInPlaceOverride field — removed in LGCY-01")

	jobPtrType := reflect.TypeOf((*worker.BatchJob)(nil))
	_, hasGetOperationModeOverride := jobPtrType.MethodByName("GetOperationModeOverride")
	assert.True(t, hasGetOperationModeOverride,
		"BatchJob must have GetOperationModeOverride method — the replacement for legacy boolean overrides")
}
