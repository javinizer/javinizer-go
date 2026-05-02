package organizer

import (
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/javinizer/javinizer-go/internal/types"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStrategy_UsesOperationModeExclusively(t *testing.T) {
	fs := afero.NewMemMapFs()

	testCases := []struct {
		name          string
		operationMode types.OperationMode
		expectedType  StrategyType
	}{
		{
			name:          "organize mode selects OrganizeStrategy",
			operationMode: types.OperationModeOrganize,
			expectedType:  StrategyTypeOrganize,
		},
		{
			name:          "in-place mode selects InPlaceStrategy",
			operationMode: types.OperationModeInPlace,
			expectedType:  StrategyTypeInPlace,
		},
		{
			name:          "in-place-norenamefolder mode selects InPlaceNoRenameFolderStrategy",
			operationMode: types.OperationModeInPlaceNoRenameFolder,
			expectedType:  StrategyTypeInPlaceNoRenameFolder,
		},
		{
			name:          "metadata-artwork mode selects MetadataArtworkStrategy",
			operationMode: types.OperationModeMetadataArtwork,
			expectedType:  StrategyTypeMetadataArtwork,
		},
		{
			name:          "preview mode selects MetadataArtworkStrategy",
			operationMode: types.OperationModePreview,
			expectedType:  StrategyTypeMetadataArtwork,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.OutputConfig{
				FolderFormat:  "<ID>",
				FileFormat:    "<ID>",
				RenameFile:    true,
				OperationMode: tc.operationMode,
			}
			org := NewOrganizer(fs, cfg, nil)

			sourcePath := "/source/TEST-001.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("test"), 0644)
			require.NoError(t, err)

			movie := testutil.NewMovieBuilder().
				WithID("TEST-001").
				WithTitle("Test Movie").
				Build()

			match := matcher.MatchResult{
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      "TEST-001.mp4",
					Extension: ".mp4",
				},
				ID: "TEST-001",
			}

			plan, err := org.Plan(match, movie, "/movies", false)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedType, plan.Strategy,
				"resolveStrategy() should select %v for OperationMode=%q", tc.expectedType, tc.operationMode)
		})
	}
}

func TestResolveStrategy_DefaultsToOrganizeWhenEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: "",
	}
	org := NewOrganizer(fs, cfg, nil)

	sourcePath := "/source/TEST-001.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("test"), 0644)
	require.NoError(t, err)

	movie := testutil.NewMovieBuilder().
		WithID("TEST-001").
		WithTitle("Test Movie").
		Build()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "TEST-001.mp4",
			Extension: ".mp4",
		},
		ID: "TEST-001",
	}

	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)
	assert.Equal(t, StrategyTypeOrganize, plan.Strategy,
		"resolveStrategy() should default to OrganizeStrategy when OperationMode is empty via GetOperationMode()")
}

func TestOutputConfig_HasNoLegacyBooleanFields(t *testing.T) {
	cfgType := reflect.TypeOf(config.OutputConfig{})

	_, hasMoveToFolder := cfgType.FieldByName("MoveToFolder")
	assert.False(t, hasMoveToFolder,
		"OutputConfig should not have MoveToFolder field — removed in LGCY-01")

	_, hasRenameFolderInPlace := cfgType.FieldByName("RenameFolderInPlace")
	assert.False(t, hasRenameFolderInPlace,
		"OutputConfig should not have RenameFolderInPlace field — removed in LGCY-01")

	_, hasOperationMode := cfgType.FieldByName("OperationMode")
	assert.True(t, hasOperationMode,
		"OutputConfig must have OperationMode field — the replacement for legacy boolean fields")
}
