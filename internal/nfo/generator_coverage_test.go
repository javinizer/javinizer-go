package nfo

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_FilenameStripsNfoExtension(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "STRIP-001", ContentID: "s1", DisplayTitle: "Strip Test"}
	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/output/STRIP-001.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestGenerate_PartSuffixDisabledWhenPerFileFalse(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true, PerFile: false}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "PART-001", ContentID: "p1", DisplayTitle: "Part Test"}
	err := g.Generate(context.Background(), movie, "/output", "-pt1", "", nil)
	require.NoError(t, err)

	// Part suffix should be ignored when PerFile is false
	exists, err := afero.Exists(fs, "/output/PART-001.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestGenerate_BadTemplateReturnsError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: "<IF:A><IF:B><IF:C><IF:D><IF:E><IF:F><IF:G><IF:H><IF:I><IF:J><IF:K><IF:L><IF:M><IF:N><IF:O><IF:P><IF:Q><IF:R><IF:S><IF:T><IF:U><IF:V><IF:W><IF:X><IF:Y><IF:Z><IF:AA><IF:AB><IF:AC><IF:AD><IF:AE><IF:AF><IF:AG>x</IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF>"}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "BAD-001", ContentID: "b1", DisplayTitle: "Bad Template"}
	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	assert.Error(t, err)
}

func TestGenerate_EmptyIDFallsBackToDefault(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FilenameTemplate: ".nfo", FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	// Empty ID, template yields ".nfo" → should fall back to default name
	movie := &models.Movie{ID: "", ContentID: "", DisplayTitle: "Empty ID"}
	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/output/metadata.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestWriteNFO_SuccessWithMemMapFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, defaultConfig())

	nfo := &Movie{ID: "W-001", Title: "Write Test", Plot: "A plot"}
	err := g.WriteNFO(nfo, "/out/test.nfo")
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, "/out/test.nfo")
	require.NoError(t, err)
	assert.Contains(t, string(data), "<?xml")
	assert.Contains(t, string(data), "Write Test")
}

func TestWriteNFO_MkdirAllFailure(t *testing.T) {
	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
	g := NewGenerator(fs, defaultConfig())

	nfo := &Movie{ID: "W-002", Title: "Write Fail"}
	err := g.WriteNFO(nfo, "/readonly/sub/test.nfo")
	assert.Error(t, err)
}

func TestMovieToNFO_OriginalPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, IncludeOriginalPath: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "OP-001", ContentID: "op1", OriginalFileName: "original_file.mp4"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, "original_file.mp4", nfo.OriginalPath)
}

func TestMovieToNFO_Tagline(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Tagline: "Best Movie Ever"}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "TG-001", ContentID: "tg1"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, "Best Movie Ever", nfo.Tagline)
}

func TestMovieToNFO_Credits(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Credits: []string{"Director A", "Writer B"}}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "CR-001", ContentID: "cr1"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, "Director A, Writer B", nfo.Credits)
}

func TestMovieToNFO_AddGenericRole(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, AddGenericRole: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID: "GR-001", ContentID: "gr1",
		Actresses: []models.Actress{{FirstName: "Yui", LastName: "Hatano"}},
	}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	require.Len(t, nfo.Actors, 1)
	assert.Equal(t, "Actress", nfo.Actors[0].Role)
}

func TestMovieToNFO_AltNameRole(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, AltNameRole: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID: "ANR-001", ContentID: "anr1",
		Actresses: []models.Actress{{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}},
	}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	require.Len(t, nfo.Actors, 1)
	assert.Equal(t, "波多野結衣", nfo.Actors[0].Role)
}

func TestMovieToNFO_DuplicateActressDedup(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID: "DUP-001", ContentID: "d1",
		Actresses: []models.Actress{
			{DMMID: 100, FirstName: "Yui", LastName: "Hatano"},
			{DMMID: 100, FirstName: "Yui", LastName: "Hatano"},
		},
	}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Len(t, nfo.Actors, 1, "duplicate actresses by DMMID should be deduplicated")
}

func TestMovieToNFO_UnknownActressSkip(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FirstNameOrder:     true,
		ActressAsTag:       true,
		UnknownActressText: "Unknown",
		UnknownActressMode: models.UnknownActressModeSkip,
	}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID: "UAS-001", ContentID: "u1",
		Actresses: []models.Actress{{FirstName: "Unknown", LastName: ""}},
	}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.NotContains(t, nfo.Tags, "Unknown")
}

func TestMovieToNFO_UnknownActressFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FirstNameOrder:     true,
		ActressAsTag:       true,
		UnknownActressText: "Unknown",
		UnknownActressMode: models.UnknownActressModeFallback,
	}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{
		ID: "UAF-001", ContentID: "u2",
		Actresses: []models.Actress{{FirstName: "Unknown", LastName: ""}},
	}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Contains(t, nfo.Tags, "Unknown")
}

func TestMovieToNFO_ReleaseYearOnly(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "RY-001", ContentID: "ry1", ReleaseYear: 2023}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, 2023, nfo.Year)
	assert.Empty(t, nfo.ReleaseDate, "ReleaseDate should be empty when only ReleaseYear is set")
}

func TestMovieToNFO_ReleaseDateSetsYear(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	d := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{ID: "RD-001", ContentID: "rd1", ReleaseDate: &d}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, 2024, nfo.Year)
	assert.Equal(t, "2024-06-15", nfo.ReleaseDate)
}

func TestMovieToNFO_NoCoverURL(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "NC-001", ContentID: "nc1", Poster: models.PosterState{CoverURL: ""}}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Empty(t, nfo.Thumb)
}

func TestResolveAndGenerate_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, &Config{FirstNameOrder: true})

	movie := &models.Movie{ID: "RAG-002", ContentID: "rag2", DisplayTitle: "Test"}
	nameCfg := NFONameConfig{FilenameTemplate: "<ID>.nfo"}
	nfoPath, err := g.ResolveAndGenerate(context.Background(), movie, "/out", nameCfg, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "/out/RAG-002.nfo", nfoPath)
}

func TestResolveAndGenerate_BrokenTemplateSkips(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, &Config{FirstNameOrder: true})

	movie := &models.Movie{ID: "SKIP-001", ContentID: "s1", DisplayTitle: "Skip"}
	nameCfg := NFONameConfig{FilenameTemplate: "<IF:A><IF:B><IF:C><IF:D><IF:E><IF:F><IF:G><IF:H><IF:I><IF:J><IF:K><IF:L><IF:M><IF:N><IF:O><IF:P><IF:Q><IF:R><IF:S><IF:T><IF:U><IF:V><IF:W><IF:X><IF:Y><IF:Z><IF:AA><IF:AB><IF:AC><IF:AD><IF:AE><IF:AF><IF:AG>x</IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF>"}
	nfoPath, err := g.ResolveAndGenerate(context.Background(), movie, "/out", nameCfg, "", nil)
	require.NoError(t, err)
	assert.Empty(t, nfoPath)
}

func TestDefaultMediaAnalyzer_Analyze_NonExistent(t *testing.T) {
	analyzer := defaultMediaAnalyzer{}
	_, err := analyzer.Analyze(context.Background(), "/nonexistent/video.mp4")
	assert.Error(t, err)
}
