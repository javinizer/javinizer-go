package organizer

import (
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStrategy_UsesOperationModeExclusively(t *testing.T) {
	fs := afero.NewMemMapFs()

	testCases := []struct {
		name          string
		operationMode operationmode.OperationMode
		expectedType  strategyType
	}{
		{
			name:          "organize mode selects OrganizeStrategy",
			operationMode: operationmode.OperationModeOrganize,
			expectedType:  strategyOrganize,
		},
		{
			name:          "in-place mode selects InPlaceStrategy",
			operationMode: operationmode.OperationModeInPlace,
			expectedType:  strategyInPlace,
		},
		{
			name:          "in-place-norenamefolder mode selects InPlaceNoRenameFolderStrategy",
			operationMode: operationmode.OperationModeInPlaceNoRenameFolder,
			expectedType:  strategyInPlaceNoRenameFolder,
		},
		{
			name:          "metadata-artwork mode selects MetadataArtworkStrategy",
			operationMode: operationmode.OperationModeMetadataArtwork,
			expectedType:  strategyMetadataArtwork,
		},
		{
			name:          "preview mode selects MetadataArtworkStrategy",
			operationMode: operationmode.OperationModePreview,
			expectedType:  strategyMetadataArtwork,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				FolderFormat:  "<ID>",
				FileFormat:    "<ID>",
				RenameFile:    true,
				OperationMode: tc.operationMode,
			}
			org := NewOrganizer(fs, cfg, nil, nil)

			sourcePath := "/source/TEST-001.mp4"
			err := afero.WriteFile(fs, sourcePath, []byte("test"), 0644)
			require.NoError(t, err)

			movie := testutil.NewMovieBuilder().
				WithID("TEST-001").
				WithTitle("Test Movie").
				Build()

			match := models.FileMatchInfo{
				Path: sourcePath, Name: "TEST-001.mp4", Extension: ".mp4",
				MovieID: "TEST-001",
			}

			plan, err := org.plan(match, movie, "/movies", false, "")
			require.NoError(t, err)
			assert.Equal(t, tc.expectedType, plan.strategy,
				"resolveStrategy() should select %v for OperationMode=%q", tc.expectedType, tc.operationMode)
		})
	}
}

func TestResolveStrategy_DefaultsToOrganizeWhenEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()

	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: "",
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	sourcePath := "/source/TEST-001.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("test"), 0644)
	require.NoError(t, err)

	movie := testutil.NewMovieBuilder().
		WithID("TEST-001").
		WithTitle("Test Movie").
		Build()

	match := models.FileMatchInfo{
		Path: sourcePath, Name: "TEST-001.mp4", Extension: ".mp4",
		MovieID: "TEST-001",
	}

	plan, err := org.plan(match, movie, "/movies", false, "")
	require.NoError(t, err)
	assert.Equal(t, strategyOrganize, plan.strategy,
		"resolveStrategy() should default to OrganizeStrategy when OperationMode is empty via GetOperationMode()")
}

func TestOutputConfig_HasNoLegacyBooleanFields(t *testing.T) {
	cfgType := reflect.TypeOf(Config{})

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
