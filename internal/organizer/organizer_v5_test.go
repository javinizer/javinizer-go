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

func newMatcherV5(t *testing.T) matcher.MatcherInterface {
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	return m
}

func TestResolveStrategy_V5_OrganizeMode(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeOrganize}
	engine := template.NewEngine()
	m := newMatcherV5(t)
	o := NewOrganizer(fs, cfg, engine, m)

	strategy := o.resolveStrategy()
	assert.NotNil(t, strategy)
}

func TestResolveStrategy_V5_InPlaceMode(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace}
	engine := template.NewEngine()
	m := newMatcherV5(t)
	o := NewOrganizer(fs, cfg, engine, m)

	strategy := o.resolveStrategy()
	assert.NotNil(t, strategy)
}

func TestResolveStrategy_V5_MetadataArtworkMode(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeMetadataArtwork}
	engine := template.NewEngine()
	m := newMatcherV5(t)
	o := NewOrganizer(fs, cfg, engine, m)

	strategy := o.resolveStrategy()
	assert.NotNil(t, strategy)
}

func TestResolveStrategy_V5_InPlaceNoRenameFolderMode(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}
	engine := template.NewEngine()
	m := newMatcherV5(t)
	o := NewOrganizer(fs, cfg, engine, m)

	strategy := o.resolveStrategy()
	assert.NotNil(t, strategy)
}

func TestStrategyFromType_V5_AllTypes(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	engine := template.NewEngine()
	m := newMatcherV5(t)
	o := NewOrganizer(fs, cfg, engine, m)

	for _, st := range []strategyType{strategyInPlace, strategyInPlaceNoRenameFolder, strategyMetadataArtwork, strategyOrganize} {
		strategy := o.strategyFromType(st)
		assert.NotNil(t, strategy, "strategyFromType(%v) should not be nil", st)
	}
}

func TestResolveBaseFileName_V5_NoRename(t *testing.T) {
	cfg := &Config{RenameFile: false}
	engine := template.NewEngine()

	match := models.FileMatchInfo{
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}

	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: "ABC-123"}, match)
	assert.Equal(t, "ABC-123", result)
}

func TestResolveBaseFileName_V5_WithRename(t *testing.T) {
	cfg := &Config{RenameFile: true, FileFormat: "<ID>"}
	engine := template.NewEngine()

	match := models.FileMatchInfo{
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}

	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: "ABC-123"}, match)
	assert.Equal(t, "ABC-123", result)
}

func TestResolveBaseFileName_V5_RenameWithEmptyTemplate(t *testing.T) {
	cfg := &Config{RenameFile: true, FileFormat: ""}
	engine := template.NewEngine()

	match := models.FileMatchInfo{
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}

	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: "ABC-123"}, match)
	// Should fall back to MovieID
	assert.Equal(t, "ABC-123", result)
}

func TestResolveBaseFileName_V5_NoRenameEmptyName(t *testing.T) {
	cfg := &Config{RenameFile: false}
	engine := template.NewEngine()

	match := models.FileMatchInfo{
		Name:      ".mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
		Path:      "/input/ABC-123.mp4",
	}

	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: "ABC-123"}, match)
	// Should fall back to path basename
	assert.NotEmpty(t, result)
}
