package organizer

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPlanContext_SetsTemplateFields(t *testing.T) {
	cfg := &Config{
		FolderFormat:      "<ID>",
		FileFormat:        "<ID>",
		RenameFile:        true,
		MediaFormatConfig: MediaFormatConfig{GroupActress: true},
		OperationMode:     operationmode.OperationModeOrganize,
	}
	engine := template.NewEngine()

	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie",
		Actresses: []models.Actress{
			{FirstName: "Momo", LastName: "Sakura"},
		},
	}

	match := models.FileMatchInfo{
		MovieID:     "ABC-123",
		PartNumber:  2,
		PartSuffix:  "-pt2",
		IsMultiPart: true,
		Path:        "/source/ABC-123-pt2.mp4", Name: "ABC-123-pt2.mp4", Extension: ".mp4",
	}

	pc := buildPlanContext(cfg, engine, movie, match)

	assert.NotNil(t, pc.Ctx)
	assert.True(t, pc.Ctx.GroupActress, "GroupActress should be set from config")
	assert.Equal(t, 2, pc.Ctx.PartNumber, "PartNumber should be set from match")
	assert.Equal(t, "-pt2", pc.Ctx.PartSuffix, "PartSuffix should be set from match")
	assert.True(t, pc.Ctx.IsMultiPart, "IsMultiPart should be set from match")
}

func TestBuildPlanContext_TitleTruncation(t *testing.T) {
	cfg := &Config{
		FolderFormat:   "<ID> <TITLE>",
		FileFormat:     "<ID> <TITLE>",
		RenameFile:     true,
		MaxTitleLength: 20,
		OperationMode:  operationmode.OperationModeOrganize,
	}
	engine := template.NewEngine()

	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "This is a very long title that should be truncated",
	}

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}

	pc := buildPlanContext(cfg, engine, movie, match)

	assert.NotNil(t, pc.Ctx)
	assert.Less(t, len(pc.Ctx.Title), len("This is a very long title that should be truncated"),
		"Title should be truncated when MaxTitleLength > 0")
}

func TestBuildPlanContext_FileNameRenameTrue(t *testing.T) {
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	engine := template.NewEngine()

	movie := &models.Movie{
		ID: "ABC-123",
	}

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/old-name.mp4", Name: "old-name.mp4", Extension: ".mp4",
	}

	pc := buildPlanContext(cfg, engine, movie, match)

	assert.Equal(t, "ABC-123.mp4", pc.FileName, "FileName should use template when RenameFile=true")
	assert.NoError(t, pc.Err)
}

func TestBuildPlanContext_FileNameRenameFalse(t *testing.T) {
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    false,
		OperationMode: operationmode.OperationModeOrganize,
	}
	engine := template.NewEngine()

	movie := &models.Movie{
		ID: "ABC-123",
	}

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/old-name.mp4", Name: "old-name.mp4", Extension: ".mp4",
	}

	pc := buildPlanContext(cfg, engine, movie, match)

	assert.Equal(t, "old-name.mp4", pc.FileName, "FileName should use original name when RenameFile=false")
	assert.NoError(t, pc.Err)
}

func TestBuildPlanContext_FolderName(t *testing.T) {
	cfg := &Config{
		FolderFormat:  "<ID> <TITLE>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	engine := template.NewEngine()

	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie",
	}

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}

	pc := buildPlanContext(cfg, engine, movie, match)

	assert.Equal(t, "ABC-123 Test Movie", pc.FolderName, "FolderName should be generated from template")
	assert.NoError(t, pc.Err)
}

func TestBuildPlanContext_FolderNameFallsBackToID(t *testing.T) {
	cfg := &Config{
		FolderFormat:  "<UNKNOWN_FIELD>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	engine := template.NewEngine()

	movie := &models.Movie{
		ID: "ABC-123",
	}

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}

	pc := buildPlanContext(cfg, engine, movie, match)

	assert.Equal(t, "ABC-123", pc.FolderName, "FolderName should fall back to match ID when template produces empty")
}

func TestResolveStrategy_ReturnsCorrectStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	// in-place strategy inspects the source directory; create it so
	// isDedicatedFolder's ReadDir succeeds (it now propagates read errors).
	require.NoError(t, fs.MkdirAll("/source", 0777))
	fileMatcher, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)

	tests := []struct {
		name          string
		operationMode operationmode.OperationMode
		expectedType  strategyType
	}{
		{
			name:          "organize mode",
			operationMode: operationmode.OperationModeOrganize,
			expectedType:  strategyOrganize,
		},
		{
			name:          "in-place mode",
			operationMode: operationmode.OperationModeInPlace,
			expectedType:  strategyInPlace,
		},
		{
			name:          "in-place-norenamefolder mode",
			operationMode: operationmode.OperationModeInPlaceNoRenameFolder,
			expectedType:  strategyInPlaceNoRenameFolder,
		},
		{
			name:          "metadata-artwork mode",
			operationMode: operationmode.OperationModeMetadataArtwork,
			expectedType:  strategyMetadataArtwork,
		},
		{
			name:          "preview mode",
			operationMode: operationmode.OperationModePreview,
			expectedType:  strategyMetadataArtwork,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				OperationMode: tt.operationMode,
			}
			engine := template.NewEngine()
			strategy := ResolveStrategy(fs, cfg, fileMatcher, engine)
			assert.NotNil(t, strategy)

			plan, err := strategy.Plan(models.FileMatchInfo{
				MovieID: "TEST-001",
				Path:    "/source/TEST-001.mp4", Name: "TEST-001.mp4", Extension: ".mp4",
			}, &models.Movie{ID: "TEST-001"}, "/dest", false)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedType, plan.strategy, "ResolveStrategy should return correct strategy type for %s", tt.operationMode)
		})
	}
}

func TestResolveStrategy_OrganizeStrategy_WhenModeNotSet(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	engine := template.NewEngine()
	fileMatcher, _ := matcher.NewMatcher(&matcher.Config{})

	strategy := ResolveStrategy(fs, cfg, fileMatcher, engine)
	assert.NotNil(t, strategy)

	plan, err := strategy.Plan(models.FileMatchInfo{
		MovieID: "TEST-001",
		Path:    "/source/TEST-001.mp4", Name: "TEST-001.mp4", Extension: ".mp4",
	}, &models.Movie{ID: "TEST-001"}, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, strategyOrganize, plan.strategy, "Default mode should return OrganizeStrategy")
}
