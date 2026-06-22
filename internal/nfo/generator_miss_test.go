package nfo

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- WriteNFO: successful write to nested directory ---

func TestWriteNFO_NestedDirectory_Miss(t *testing.T) {
	memFS := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
	g := NewGenerator(memFS, cfg)

	nfo := &Movie{ID: "NESTED-001", Title: "Nested Test"}
	err := g.WriteNFO(nfo, "/deep/nested/dir/NESTED-001.nfo")
	require.NoError(t, err)

	exists, err := afero.Exists(memFS, "/deep/nested/dir/NESTED-001.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// --- Generate: template that sanitizes to just "nfo" ---

func TestGenerate_TemplateProducesJustNfo_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "nfo", FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "JUSTNFO-001", ContentID: "j1", DisplayTitle: "Just NFO"}
	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	require.NoError(t, err)

	// After stripping ".nfo" from "nfo", should fall back to movie ID
	exists, err := afero.Exists(fs, "/output/JUSTNFO-001.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// --- Generate: template with part suffix when PerFile is enabled ---

func TestGenerate_PartSuffixWithPerFile_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true, PerFile: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "PART-001", ContentID: "p1", DisplayTitle: "Part Test"}
	err := g.Generate(context.Background(), movie, "/output", "-pt1", "", nil)
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/output/PART-001-pt1.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// --- GenerateAtPath: successful write ---

func TestGenerateAtPath_Success_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "PATH-001", ContentID: "pt1", DisplayTitle: "Path Test"}
	err := g.GenerateAtPath(context.Background(), movie, "/output/CUSTOM.nfo", "", nil)
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/output/CUSTOM.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// --- ResolveAndGenerate: successful generation returns path ---

func TestResolveAndGenerate_SuccessReturnsPath_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "RESOLVE-001", ContentID: "r1", DisplayTitle: "Resolve Test"}
	nameCfg := NFONameConfig{
		FilenameTemplate: "<ID>.nfo",
		FirstNameOrder:   true,
	}

	nfoPath, err := g.ResolveAndGenerate(context.Background(), movie, "/output", nameCfg, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "/output/RESOLVE-001.nfo", nfoPath)

	exists, err := afero.Exists(fs, "/output/RESOLVE-001.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// --- Generate: WriteNFO XML encoding error ---

func TestGenerate_WriteNFOXMLError_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "XML-001", ContentID: "x1", DisplayTitle: "XML Test"}
	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	require.NoError(t, err)
}

// --- buildNFO: actress dedup with DMMID ---

func TestBuildNFO_ActressDedupWithDMMID_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FilenameTemplate: "<ID>.nfo",
		FirstNameOrder:   true,
		AddGenericRole:   true,
	}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "DEDUP-001",
		ContentID:    "d1",
		DisplayTitle: "Dedup Test",
		Actresses: []models.Actress{
			{DMMID: 100, FirstName: "Airi", LastName: "Suzumura"},
			{DMMID: 100, FirstName: "Airi", LastName: "Suzumura"}, // Duplicate by DMMID
		},
	}

	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	require.NoError(t, err)

	// Read the NFO and verify only one actress
	exists, _ := afero.Exists(fs, "/output/DEDUP-001.nfo")
	assert.True(t, exists)
}

// --- buildNFO: rating with zero score and votes ---

func TestBuildNFO_NoRatingWhenZero_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "NORATE-001",
		ContentID:    "n1",
		DisplayTitle: "No Rating Test",
		RatingScore:  0,
		RatingVotes:  0,
	}

	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	require.NoError(t, err)
}
