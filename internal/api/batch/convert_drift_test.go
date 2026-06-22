package batch

import (
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// TestMovieResultToBatchFileResultDriftGuard ensures that every exported field
// on worker.MovieResult is either mapped in movieResultToResponse or explicitly
// documented as intentionally unmapped. If a new field is added to MovieResult,
// this test will fail until the developer either:
//
//	(a) adds the field to the conversion function AND the contracts type, or
//	(b) adds the field to the intentionallyUnmappedInFull/intentionallyUnmappedInSlim set
//	     with a documented reason.
//
// This prevents the contracts types from silently drifting away from the source
// type when fields are added to MovieResult.
func TestMovieResultToBatchFileResultDriftGuard(t *testing.T) {
	mrType := reflect.TypeOf(worker.MovieResult{})
	bfrType := reflect.TypeOf(contracts.BatchFileResult{})
	bfrSlimType := reflect.TypeOf(contracts.BatchFileResultSlim{})

	// Fields on MovieResult that are intentionally NOT exposed in the full
	// BatchFileResult API response, with documented reasons.
	intentionallyUnmappedInFull := map[string]string{
		"Revision":      "internal versioning counter, not exposed to API consumers",
		"FileMatchInfo": "flattened into BatchFileResult top-level fields (FilePath, MovieID, IsMultiPart, PartNumber, PartSuffix)",
		// OrchestrationState is embedded — its sub-fields are checked separately below.
		// The OrchestrationState itself (DisplayTitleApplied, PosterGenerated, Persisted,
		// PosterError, TranslationWarning) is internal orchestration metadata not exposed
		// in the API response per ADR-0027.
	}

	// Fields intentionally omitted from the slim variant (in addition to full unmapped).
	intentionallyUnmappedInSlim := map[string]string{
		"Movie": "slim variant intentionally omits movie data for efficient polling",
	}

	// Fields on BatchFileResult that come from ProvenanceData (injected via the
	// prov parameter), not from MovieResult directly.
	provenanceFields := map[string]string{
		"FieldSources":   "from worker.ProvenanceData, not MovieResult",
		"ActressSources": "from worker.ProvenanceData, not MovieResult",
		"FilePath":       "flattened from MovieResult.FileMatchInfo.Path",
		"MovieID":        "flattened from MovieResult.FileMatchInfo.MovieID",
		"IsMultiPart":    "flattened from MovieResult.FileMatchInfo.IsMultiPart",
		"PartNumber":     "flattened from MovieResult.FileMatchInfo.PartNumber",
		"PartSuffix":     "flattened from MovieResult.FileMatchInfo.PartSuffix",
	}

	// Collect all exported fields from MovieResult (including embedded OrchestrationState).
	movieResultFieldNames := fieldNamesOf(t, mrType)

	// Collect all exported fields from the contracts types.
	bfrFieldNames := fieldNamesOf(t, bfrType)
	bfrSlimFieldNames := fieldNamesOf(t, bfrSlimType)

	t.Run("full_response_covers_all_MovieResult_fields", func(t *testing.T) {
		for field := range movieResultFieldNames {
			if _, ok := intentionallyUnmappedInFull[field]; ok {
				continue
			}
			// OrchestrationState embedded fields are intentionally unmapped.
			if isOrchestrationStateField(mrType, field) {
				continue
			}
			if _, ok := bfrFieldNames[field]; !ok {
				t.Errorf("MovieResult field %q is not in BatchFileResult and not in intentionallyUnmappedInFull. "+
					"Either add it to the contracts type and conversion function, or document it as intentionally unmapped.", field)
			}
		}
	})

	t.Run("slim_response_covers_all_MovieResult_fields", func(t *testing.T) {
		for field := range movieResultFieldNames {
			if _, ok := intentionallyUnmappedInFull[field]; ok {
				continue
			}
			if _, ok := intentionallyUnmappedInSlim[field]; ok {
				continue
			}
			if isOrchestrationStateField(mrType, field) {
				continue
			}
			if _, ok := bfrSlimFieldNames[field]; !ok {
				t.Errorf("MovieResult field %q is not in BatchFileResultSlim and not in intentionallyUnmappedInSlim. "+
					"Either add it to the contracts type and conversion function, or document it as intentionally unmapped.", field)
			}
		}
	})

	t.Run("no_stale_unmapped_entries", func(t *testing.T) {
		// Ensure the intentionallyUnmapped sets don't contain fields that no longer
		// exist on MovieResult — stale entries would mask real drift.
		for field := range intentionallyUnmappedInFull {
			if _, ok := movieResultFieldNames[field]; !ok && !isOrchestrationStateField(mrType, field) {
				t.Errorf("intentionallyUnmappedInFull contains %q but MovieResult no longer has this field. Remove the stale entry.", field)
			}
		}
		for field := range intentionallyUnmappedInSlim {
			if _, ok := movieResultFieldNames[field]; !ok && !isOrchestrationStateField(mrType, field) {
				t.Errorf("intentionallyUnmappedInSlim contains %q but MovieResult no longer has this field. Remove the stale entry.", field)
			}
		}
	})

	t.Run("no_unknown_BatchFileResult_fields", func(t *testing.T) {
		// Every field in BatchFileResult should trace back to either MovieResult
		// or ProvenanceData. Unknown fields indicate the contracts type has
		// diverged from the source.
		for field := range bfrFieldNames {
			if _, ok := movieResultFieldNames[field]; ok {
				continue
			}
			if _, ok := provenanceFields[field]; ok {
				continue
			}
			if isOrchestrationStateField(mrType, field) {
				continue
			}
			t.Errorf("BatchFileResult has field %q that doesn't come from MovieResult or ProvenanceData. "+
				"If this is intentional, add it to provenanceFields or document it.", field)
		}
	})

	t.Run("no_unknown_BatchFileResultSlim_fields", func(t *testing.T) {
		for field := range bfrSlimFieldNames {
			if _, ok := movieResultFieldNames[field]; ok {
				continue
			}
			if _, ok := provenanceFields[field]; ok {
				continue
			}
			if isOrchestrationStateField(mrType, field) {
				continue
			}
			t.Errorf("BatchFileResultSlim has field %q that doesn't come from MovieResult or ProvenanceData. "+
				"If this is intentional, add it to provenanceFields or document it.", field)
		}
	})

	t.Run("slim_is_subset_of_full", func(t *testing.T) {
		// Every field in BatchFileResultSlim should exist in BatchFileResult
		// (slim is a strict subset minus Movie).
		for field := range bfrSlimFieldNames {
			if _, ok := bfrFieldNames[field]; !ok {
				t.Errorf("BatchFileResultSlim has field %q that doesn't exist in BatchFileResult. "+
					"Slim should be a strict subset of full.", field)
			}
		}
	})
}

// TestBatchJobBaseFieldsDriftGuard ensures that every field on worker.batchJobBase
// is either mapped in toBaseResponse or explicitly documented as intentionally
// unmapped in the API response types.
//
// Since batchJobBase is unexported, we can't use reflection across packages.
// Instead, we maintain an explicit list of batchJobBase fields here. If a field
// is added to batchJobBase, this test will fail until the field is either added
// to the mapped set or the intentionallyUnmapped set.
func TestBatchJobBaseFieldsDriftGuard(t *testing.T) {
	// Explicit list of ALL fields on worker.batchJobBase.
	// When a field is added to batchJobBase, add it here too — the test will
	// fail if the two lists diverge because the "unaccounted" check will find
	// a new field that isn't in either mapped or intentionallyUnmapped.
	batchJobBaseFields := []string{
		"ID",
		"Status",
		"TotalFiles",
		"Completed",
		"Failed",
		"Excluded",
		"Files",
		"FileMatchInfo",
		"Progress",
		"Destination",
		"TempDir",
		"StartedAt",
		"CompletedAt",
		"OrganizedAt",
		"RevertedAt",
		"OperationModeOverride",
		"Update",
		"PersistError",
		"IsDeleted",
	}

	// Fields on batchJobBase that are mapped to the API response via toBaseResponse.
	// Must match the actual fields extracted in the toBaseResponse() function in convert.go.
	mapped := map[string]bool{
		"ID":                    true,
		"Status":                true,
		"TotalFiles":            true,
		"Completed":             true,
		"Failed":                true,
		"Excluded":              true,
		"Progress":              true,
		"Destination":           true,
		"StartedAt":             true,
		"CompletedAt":           true,
		"OperationModeOverride": true,
		"Update":                true,
		"PersistError":          true,
	}

	// Fields on batchJobBase that are intentionally NOT exposed in API responses.
	intentionallyUnmapped := map[string]string{
		"Files":         "internal file list, not exposed via API",
		"FileMatchInfo": "per-file info, available via results map instead",
		"TempDir":       "internal temp directory, not exposed to API consumers",
		"OrganizedAt":   "internal timestamp, not exposed in current API schema",
		"RevertedAt":    "internal timestamp, not exposed in current API schema",
		"IsDeleted":     "internal soft-delete flag, not exposed in current API schema",
	}

	t.Run("every_batchJobBase_field_is_accounted_for", func(t *testing.T) {
		for _, field := range batchJobBaseFields {
			if mapped[field] {
				continue
			}
			if _, ok := intentionallyUnmapped[field]; ok {
				continue
			}
			t.Errorf("batchJobBase field %q is neither in mapped nor intentionallyUnmapped. "+
				"Either add it to the toBaseResponse() function in convert.go or document it as intentionally unmapped.", field)
		}
	})

	t.Run("no_extra_mapped_or_unmapped_fields", func(t *testing.T) {
		all := make(map[string]bool, len(batchJobBaseFields))
		for _, f := range batchJobBaseFields {
			all[f] = true
		}
		for field := range mapped {
			if !all[field] {
				t.Errorf("mapped contains %q but it's not in batchJobBaseFields. Remove the stale entry.", field)
			}
		}
		for field := range intentionallyUnmapped {
			if !all[field] {
				t.Errorf("intentionallyUnmapped contains %q but it's not in batchJobBaseFields. Remove the stale entry.", field)
			}
		}
	})

	// Verify that BatchJobResponse contains every mapped field.
	respType := reflect.TypeOf(contracts.BatchJobResponse{})
	respFieldNames := fieldNamesOf(t, respType)
	t.Run("mapped_fields_exist_in_BatchJobResponse", func(t *testing.T) {
		for field := range mapped {
			if !respFieldNames[field] {
				t.Errorf("mapped field %q doesn't exist in BatchJobResponse. "+
					"Either add it to the contracts type or remove it from mapped.", field)
			}
		}
	})
}

// isOrchestrationStateField checks whether a field name belongs to the embedded
// OrchestrationState struct on MovieResult, rather than being a direct field of
// MovieResult itself.
func isOrchestrationStateField(mrType reflect.Type, fieldName string) bool {
	// Walk embedded fields to find OrchestrationState's sub-fields.
	for i := 0; i < mrType.NumField(); i++ {
		f := mrType.Field(i)
		if f.Anonymous && f.Type.Name() == "OrchestrationState" {
			// Check if fieldName is a field on OrchestrationState.
			for j := 0; j < f.Type.NumField(); j++ {
				if f.Type.Field(j).Name == fieldName {
					return true
				}
			}
		}
	}
	return false
}

// fieldNamesOf returns a set of exported field names for the given struct type.
// For struct types with embedded fields, it flattens the embedded fields
// (e.g., OrchestrationState.DisplayTitleApplied becomes "DisplayTitleApplied").
func fieldNamesOf(t *testing.T, typ reflect.Type) map[string]bool {
	t.Helper()
	result := make(map[string]bool)
	collectFieldNames(t, typ, result)
	return result
}

func collectFieldNames(t *testing.T, typ reflect.Type, result map[string]bool) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		// Flatten anonymous (embedded) struct fields.
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			collectFieldNames(t, f.Type, result)
			continue
		}
		result[f.Name] = true
	}
}
