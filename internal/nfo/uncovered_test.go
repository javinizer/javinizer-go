package nfo

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGenerator_NilConfig_Defaults_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, nil)
	require.NotNil(t, g)
	assert.Equal(t, "Unknown", g.config.UnknownActressText)
	assert.Equal(t, models.UnknownActressModeSkip, g.config.UnknownActressMode)
	assert.Equal(t, "<ID>.nfo", g.config.FilenameTemplate)
}

func TestGenerator_Generate_WithPartSuffix_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FirstNameOrder: true,
		PerFile:        true,
	}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "TEST-001",
		ContentID:    "test001",
		DisplayTitle: "Test Movie",
	}

	err := g.Generate(context.Background(), movie, "/output", "-pt1", "", nil)
	require.NoError(t, err)

	// Verify the NFO file was created with part suffix
	exists, err := afero.Exists(fs, "/output/TEST-001-pt1.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestGenerator_GenerateAtPath_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "PATH-001",
		ContentID:    "path001",
		DisplayTitle: "Path Test",
	}

	err := g.GenerateAtPath(context.Background(), movie, "/custom/path.nfo", "", nil)
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/custom/path.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestGenerator_ResolveAndGenerate_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "RAG-001",
		ContentID:    "rag001",
		DisplayTitle: "Resolve And Generate",
	}

	nameCfg := NFONameConfig{FilenameTemplate: "<ID>.nfo"}
	nfoPath, err := g.ResolveAndGenerate(context.Background(), movie, "/output", nameCfg, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "/output/RAG-001.nfo", nfoPath)

	exists, err := afero.Exists(fs, nfoPath)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestGenerator_ResolveAndGenerate_BrokenTemplate_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "BROKEN-001",
		ContentID:    "broken001",
		DisplayTitle: "Broken Template",
	}

	// Template with excessive depth should fail validation
	nameCfg := NFONameConfig{FilenameTemplate: "<IF:A><IF:B><IF:C><IF:D><IF:E><IF:F><IF:G><IF:H><IF:I><IF:J><IF:K><IF:L><IF:M><IF:N><IF:O><IF:P><IF:Q><IF:R><IF:S><IF:T><IF:U><IF:V><IF:W><IF:X><IF:Y><IF:Z><IF:AA><IF:AB><IF:AC><IF:AD><IF:AE><IF:AF><IF:AG>x</IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF>"}
	nfoPath, err := g.ResolveAndGenerate(context.Background(), movie, "/output", nameCfg, "", nil)
	require.NoError(t, err)
	assert.Empty(t, nfoPath, "broken template should skip NFO generation, returning empty path")
}

func TestGenerator_MovieToNFO_WithContentID_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	releaseDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{
		ID:           "CID-001",
		ContentID:    "cid001",
		DisplayTitle: "Content ID Test",
		Description:  "A test movie",
		Director:     "Test Director",
		Maker:        "Test Studio",
		Label:        "Test Label",
		Series:       "Test Series",
		ReleaseDate:  &releaseDate,
		ReleaseYear:  2024,
		Runtime:      120,
		RatingScore:  8.5,
		RatingVotes:  200,
		Poster:       models.PosterState{CoverURL: "http://example.com/cover.jpg"},
		TrailerURL:   "http://example.com/trailer.mp4",
		Actresses:    []models.Actress{{DMMID: 1, JapaneseName: "テスト女優", FirstName: "Test", LastName: "Actress"}},
		Genres:       []models.Genre{{Name: "Drama"}, {Name: "Romance"}},
	}

	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	require.NotNil(t, nfo)
	assert.Equal(t, "CID-001", nfo.ID)
	assert.Equal(t, "Content ID Test", nfo.Title)
	assert.Equal(t, 120, nfo.Runtime)
	assert.Len(t, nfo.UniqueID, 1)
	assert.Equal(t, "contentid", nfo.UniqueID[0].Type)
	assert.Equal(t, "cid001", nfo.UniqueID[0].Value)
}

func TestGenerator_MovieToNFO_WithFanart_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, IncludeFanart: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "FAN-001",
		ContentID:    "fan001",
		DisplayTitle: "Fanart Test",
		Poster:       models.PosterState{CoverURL: "http://example.com/cover.jpg"},
		Screenshots:  []string{"http://example.com/ss1.jpg", "http://example.com/ss2.jpg"},
	}

	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	require.NotNil(t, nfo)
	assert.NotNil(t, nfo.Fanart)
	assert.GreaterOrEqual(t, len(nfo.Fanart.Thumbs), 2)
}

func TestGenerator_MovieToNFO_FanartDisabled_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, IncludeFanart: false}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "NOFAN-001",
		ContentID:    "nofan001",
		DisplayTitle: "No Fanart",
		Poster:       models.PosterState{CoverURL: "http://example.com/cover.jpg"},
		Screenshots:  []string{"http://example.com/ss1.jpg"},
	}

	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	require.NotNil(t, nfo)
	assert.Nil(t, nfo.Fanart)
}

func TestGenerator_MovieToNFO_TrailerDisabled_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, IncludeTrailer: false}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID:           "NOTRAIL-001",
		ContentID:    "notrail001",
		DisplayTitle: "No Trailer",
		TrailerURL:   "http://example.com/trailer.mp4",
	}

	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	require.NotNil(t, nfo)
	assert.Empty(t, nfo.Trailer)
}

func TestGenerator_WriteNFO_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := &Movie{
		ID:    "WRITE-001",
		Title: "Write Test",
	}

	err := g.WriteNFO(nfo, "/output/write.nfo")
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/output/write.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestMergeWithExistingNFO_ForceOverwrite_Uncovered(t *testing.T) {
	nfoImpl := nfoImplementor{}

	movie := &models.Movie{ID: "FORCE-001", Title: "Force Test"}
	opts := MergeWithExistingOptions{ForceOverwrite: true}

	result := nfoImpl.MergeWithExistingNFO(movie, opts)
	assert.Equal(t, movie, result.Movie)
	assert.False(t, result.Merged)
}

func TestMergeWithExistingNFO_NilFS_Uncovered(t *testing.T) {
	nfoImpl := nfoImplementor{} // fs is nil

	movie := &models.Movie{ID: "NILFS-001", Title: "Nil FS Test"}
	opts := MergeWithExistingOptions{
		Match: models.FileMatchInfo{Path: "/some/file.mp4"},
	}

	result := nfoImpl.MergeWithExistingNFO(movie, opts)
	assert.Equal(t, movie, result.Movie)
	assert.False(t, result.Merged)
}

func TestMergeWithExistingNFO_PreserveNFO_Uncovered(t *testing.T) {
	fs := afero.NewMemMapFs()
	nfoImpl := nfoImplementor{
		fs:             fs,
		nfoConfig:      &Config{FirstNameOrder: true},
		templateEngine: template.NewEngine(),
	}

	// Create an existing NFO file
	nfoContent := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<movie>\n  <id>EXISTING-001</id>\n  <title>Existing Title</title>\n</movie>"
	require.NoError(t, afero.WriteFile(fs, "/media/EXISTING-001.nfo", []byte(nfoContent), 0644))

	movie := &models.Movie{ID: "EXISTING-001", Title: "Scraped Title"}
	opts := MergeWithExistingOptions{
		Match:       models.FileMatchInfo{Path: "/media/EXISTING-001.mp4"},
		PreserveNFO: true,
	}

	result := nfoImpl.MergeWithExistingNFO(movie, opts)
	// With PreserveNFO, the merge should use PreserveExisting strategy,
	// so the existing NFO title should be preserved
	assert.True(t, result.Merged, "should have merged with existing NFO")
	assert.Equal(t, "Existing Title", result.Movie.Title, "PreserveNFO should keep existing title")
}

func TestDefaultMediaAnalyzer_Analyze_Uncovered(t *testing.T) {
	analyzer := defaultMediaAnalyzer{}
	// Analyzing a non-existent file should return an error
	_, err := analyzer.Analyze(context.Background(), "/nonexistent/video.mp4")
	assert.Error(t, err)
}

func TestResolveNFOFilename_NilEngine_Uncovered(t *testing.T) {
	movie := &models.Movie{ID: "RESOLVE-001"}
	cfg := NFONameConfig{FilenameTemplate: "<ID>.nfo"}

	filename := ResolveNFOFilename(nil, movie, cfg)
	assert.Equal(t, "RESOLVE-001.nfo", filename)
}

func TestResolveNFOFilename_InvalidTemplate_Uncovered(t *testing.T) {
	movie := &models.Movie{ID: "BAD-001"}
	cfg := NFONameConfig{FilenameTemplate: "<UNKNOWN_TAG>"}

	filename := ResolveNFOFilename(nil, movie, cfg)
	// Should fall back to movie.ID.nfo
	assert.Equal(t, "BAD-001.nfo", filename)
}

func TestResolveNFOFilename_PartSuffix_Uncovered(t *testing.T) {
	movie := &models.Movie{ID: "PART-001"}
	cfg := NFONameConfig{
		FilenameTemplate: "<ID>.nfo",
		PerFile:          true,
		PartSuffix:       "-pt1",
	}

	filename := ResolveNFOFilename(nil, movie, cfg)
	assert.Equal(t, "PART-001-pt1.nfo", filename)
}

func TestNormalizeActressNameForDedup_Uncovered(t *testing.T) {
	assert.Equal(t, "test name", normalizeActressNameForDedup("  Test   Name  "))
	assert.Equal(t, "", normalizeActressNameForDedup(""))
	assert.Equal(t, "", normalizeActressNameForDedup("   "))
}
