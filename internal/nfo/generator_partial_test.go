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

// --- Partial line coverage for Generate ---

// TestGenerate_TemplateErrorReturnsError covers template execution error
func TestGenerate_TemplateErrorReturnsError_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FilenameTemplate: "<IF:A><IF:B><IF:C><IF:D><IF:E><IF:F><IF:G><IF:H><IF:I><IF:J><IF:K><IF:L><IF:M><IF:N><IF:O><IF:P><IF:Q><IF:R><IF:S><IF:T><IF:U><IF:V><IF:W><IF:X><IF:Y><IF:Z><IF:AA><IF:AB><IF:AC><IF:AD><IF:AE><IF:AF><IF:AG>x</IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF>",
		FirstNameOrder:   true,
	}
	g := NewGenerator(fs, cfg)
	movie := &models.Movie{ID: "ERR-001", DisplayTitle: "Error Test"}

	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate NFO filename")
}

// TestGenerate_FilenameIsJustNfo covers template producing just "nfo" -> empty -> fallback
func TestGenerate_FilenameIsJustNfo_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FilenameTemplate: "nfo", // After stripping "nfo" extension -> empty
		FirstNameOrder:   true,
	}
	g := NewGenerator(fs, cfg)
	movie := &models.Movie{ID: "NFO-001", DisplayTitle: "NFO Test"}

	err := g.Generate(context.Background(), movie, "/output", "", "", nil)
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/output/NFO-001.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestGenerate_PartSuffixEnabled covers part suffix with PerFile=true
func TestGenerate_PartSuffixEnabled_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FilenameTemplate: "<ID>.nfo",
		FirstNameOrder:   true,
		PerFile:          true,
	}
	g := NewGenerator(fs, cfg)
	movie := &models.Movie{ID: "PART-001", DisplayTitle: "Part Test"}

	err := g.Generate(context.Background(), movie, "/output", "-pt1", "", nil)
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/output/PART-001-pt1.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestGenerate_SanitizedFilenameEmptyFallback covers empty sanitized filename fallback
func TestGenerate_SanitizedFilenameEmptyFallback_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FilenameTemplate: "<ID>",
		FirstNameOrder:   true,
	}
	g := NewGenerator(fs, cfg)

	err := g.Generate(context.Background(), &models.Movie{ID: "?*|", DisplayTitle: "Invalid ID"}, "/output", "", "", nil)
	require.NoError(t, err)

	// ?*| sanitizes to "-" which is a valid filename
	exists, err := afero.Exists(fs, "/output/-.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// --- Partial line coverage for GenerateAtPath ---

// TestGenerateAtPath_WritesNFO covers basic path generation
func TestGenerateAtPath_WritesNFO_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)
	movie := &models.Movie{ID: "PATH-001", DisplayTitle: "Path Test"}

	err := g.GenerateAtPath(context.Background(), movie, "/output/custom.nfo", "", nil)
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/output/custom.nfo")
	require.NoError(t, err)
	assert.True(t, exists)
}

// --- Partial line coverage for ResolveAndGenerate ---

// TestResolveAndGenerate_BrokenTemplateSkipsNFO covers broken template -> ("", nil)
func TestResolveAndGenerate_BrokenTemplateSkipsNFO_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FilenameTemplate: "<IF:A><IF:B><IF:C><IF:D><IF:E><IF:F><IF:G><IF:H><IF:I><IF:J><IF:K><IF:L><IF:M><IF:N><IF:O><IF:P><IF:Q><IF:R><IF:S><IF:T><IF:U><IF:V><IF:W><IF:X><IF:Y><IF:Z><IF:AA><IF:AB><IF:AC><IF:AD><IF:AE><IF:AF><IF:AG>x</IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF>",
		FirstNameOrder:   true,
	}
	g := NewGenerator(fs, cfg)
	movie := &models.Movie{ID: "SKIP-001", DisplayTitle: "Skip Test"}

	nfoPath, err := g.ResolveAndGenerate(context.Background(), movie, "/output", NFONameConfig{
		FilenameTemplate: cfg.FilenameTemplate,
		FirstNameOrder:   true,
	}, "", nil)
	assert.NoError(t, err)
	assert.Equal(t, "", nfoPath) // Skipped, not an error
}

// TestResolveAndGenerate_Success covers successful generation
func TestResolveAndGenerate_Success_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FilenameTemplate: "<ID>.nfo",
		FirstNameOrder:   true,
	}
	g := NewGenerator(fs, cfg)
	movie := &models.Movie{ID: "RESOLVE-001", DisplayTitle: "Resolve Test"}

	nfoPath, err := g.ResolveAndGenerate(context.Background(), movie, "/output", NFONameConfig{
		FilenameTemplate: "<ID>.nfo",
		FirstNameOrder:   true,
	}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "/output/RESOLVE-001.nfo", nfoPath)

	exists, err := afero.Exists(fs, nfoPath)
	require.NoError(t, err)
	assert.True(t, exists)
}

// --- Partial line coverage for movieToNFO ---

// TestMovieToNFO_DisplayTitleFallback covers DisplayTitle="" -> Title fallback
func TestMovieToNFO_DisplayTitleFallback_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "DT-001", Title: "Fallback Title", DisplayTitle: ""}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, "Fallback Title", nfo.Title)
}

// TestMovieToNFO_WithContentID covers contentID != "" branch
func TestMovieToNFO_WithContentID_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "CID-001", ContentID: "cid001", DisplayTitle: "CID Test"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	require.Len(t, nfo.UniqueID, 1)
	assert.Equal(t, "contentid", nfo.UniqueID[0].Type)
	assert.Equal(t, "cid001", nfo.UniqueID[0].Value)
	assert.True(t, nfo.UniqueID[0].Default)
}

// TestMovieToNFO_ReleaseDateNil_WithYear covers releaseYear > 0 fallback
func TestMovieToNFO_ReleaseDateNil_WithYear_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "YEAR-001", ReleaseYear: 2023, DisplayTitle: "Year Test"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, 2023, nfo.Year)
	assert.Equal(t, "", nfo.ReleaseDate) // No full date available
	assert.Equal(t, "", nfo.Premiered)   // No full date available
}

// TestMovieToNFO_WithReleaseDate covers full date rendering
func TestMovieToNFO_WithReleaseDate_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	rd := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)
	movie := &models.Movie{ID: "RD-001", ReleaseDate: &rd, DisplayTitle: "RD Test"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, "2023-06-15", nfo.ReleaseDate)
	assert.Equal(t, "2023-06-15", nfo.Premiered)
	assert.Equal(t, 2023, nfo.Year)
}

// TestMovieToNFO_RuntimeZero covers runtime <= 0
func TestMovieToNFO_RuntimeZero_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "RT-001", Runtime: 0, DisplayTitle: "RT Test"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, 0, nfo.Runtime)
}

// TestMovieToNFO_RuntimePositive covers runtime > 0
func TestMovieToNFO_RuntimePositive_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "RT-002", Runtime: 120, DisplayTitle: "RT Test"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Equal(t, 120, nfo.Runtime)
}

// TestMovieToNFO_RatingPositive covers ratingScore > 0
func TestMovieToNFO_RatingPositive_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, RatingSource: "themoviedb"}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "RATE-001", RatingScore: 8.5, RatingVotes: 100, DisplayTitle: "Rate Test"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	require.Len(t, nfo.Ratings.Rating, 1)
	assert.Equal(t, 8.5, nfo.Ratings.Rating[0].Value)
	assert.Equal(t, 100, nfo.Ratings.Rating[0].Votes)
	assert.Equal(t, "themoviedb", nfo.Ratings.Rating[0].Name)
	assert.Equal(t, 10, nfo.Ratings.Rating[0].Max)
	assert.True(t, nfo.Ratings.Rating[0].Default)
}

// TestMovieToNFO_RatingZero covers ratingScore <= 0
func TestMovieToNFO_RatingZero_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	movie := &models.Movie{ID: "RATE-002", RatingScore: 0, DisplayTitle: "No Rate"}
	nfo := g.movieToNFO(context.Background(), movie, "", nil)
	assert.Empty(t, nfo.Ratings.Rating)
}

// --- Partial line coverage for buildActors (extracted from buildNFO) ---

// TestBuildActors_DMMIDDedup covers DMMID dedup
func TestBuildActors_DMMIDDedup_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	// Two actresses with same DMMID should dedup
	actors := g.buildActors([]models.Actress{
		{DMMID: 10, FirstName: "Actress", LastName: "A"},
		{DMMID: 10, FirstName: "Actress", LastName: "A Duplicate"},
	})
	assert.Len(t, actors, 1)
	assert.Equal(t, "Actress A", actors[0].Name)
}

// TestBuildActors_NameDedup covers name-based dedup for non-DMMID actresses
func TestBuildActors_NameDedup_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	// Two actresses with same name (case-insensitive) should dedup
	actors := g.buildActors([]models.Actress{
		{FirstName: "Actress", LastName: "A"},
		{FirstName: "actress", LastName: "a"},
	})
	assert.Len(t, actors, 1)
}

// TestBuildActors_DMMIDWithEmptyName covers DMMID with empty name for dedup
func TestBuildActors_DMMIDWithEmptyName_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	actors := g.buildActors([]models.Actress{
		{DMMID: 10}, // Empty name, has DMMID
	})
	assert.Len(t, actors, 1)
}

// TestBuildActors_EmptyNameDedup covers empty name dedup
func TestBuildActors_EmptyNameDedup_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, UnknownActressText: "Unknown", UnknownActressMode: models.UnknownActressModeFallback}
	g := NewGenerator(fs, cfg)

	// Two actresses with empty names — formatActressName returns "Unknown" for both,
	// so dedup keeps only one
	actors := g.buildActors([]models.Actress{
		{},
		{},
	})
	assert.Len(t, actors, 1)
}

// TestBuildActors_AddGenericRole covers AddGenericRole config
func TestBuildActors_AddGenericRole_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, AddGenericRole: true}
	g := NewGenerator(fs, cfg)

	actors := g.buildActors([]models.Actress{
		{FirstName: "Actress", LastName: "A"},
	})
	require.Len(t, actors, 1)
	assert.Equal(t, "Actress", actors[0].Role)
}

// TestBuildActors_AltNameRole covers AltNameRole config
func TestBuildActors_AltNameRole_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, AltNameRole: true}
	g := NewGenerator(fs, cfg)

	actors := g.buildActors([]models.Actress{
		{FirstName: "Actress", LastName: "A", JapaneseName: "日本名"},
	})
	require.Len(t, actors, 1)
	assert.Equal(t, "日本名", actors[0].Role)
}

// TestBuildActors_AltNameRole_NoJapaneseName covers AltNameRole with no JapaneseName
func TestBuildActors_AltNameRole_NoJapaneseName_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, AltNameRole: true}
	g := NewGenerator(fs, cfg)

	actors := g.buildActors([]models.Actress{
		{FirstName: "Actress", LastName: "A"},
	})
	require.Len(t, actors, 1)
	assert.Equal(t, "", actors[0].Role) // No JapaneseName, so AltNameRole doesn't apply
}

// TestBuildActors_ThumbURL covers thumbURL
func TestBuildActors_ThumbURL_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	actors := g.buildActors([]models.Actress{
		{FirstName: "Actress", LastName: "A", ThumbURL: "http://thumb.jpg"},
	})
	require.Len(t, actors, 1)
	assert.Equal(t, "http://thumb.jpg", actors[0].Thumb)
}

// TestBuildActors_NoThumbURL covers no thumbURL
func TestBuildActors_NoThumbURL_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	actors := g.buildActors([]models.Actress{
		{FirstName: "Actress", LastName: "A"},
	})
	require.Len(t, actors, 1)
	assert.Equal(t, "", actors[0].Thumb)
}

// --- Partial line coverage for buildNFO (thin mapper) ---

// TestBuildNFO_WithGenres covers genres mapping
func TestBuildNFO_WithGenres_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id:     "GEN-001",
		genres: []string{"Action", "Drama"},
	})
	assert.Equal(t, []string{"Action", "Drama"}, nfo.Genres)
}

// TestBuildNFO_NoGenres covers no genres
func TestBuildNFO_NoGenres_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id: "NOGEN-001",
	})
	assert.Nil(t, nfo.Genres)
}

// TestBuildNFO_WithThumbs covers poster thumbs mapping
func TestBuildNFO_WithThumbs_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id: "COVER-001",
		thumbs: []Thumb{
			{Aspect: "poster", Value: "http://cover.jpg"},
		},
	})
	require.Len(t, nfo.Thumb, 1)
	assert.Equal(t, "poster", nfo.Thumb[0].Aspect)
	assert.Equal(t, "http://cover.jpg", nfo.Thumb[0].Value)
}

// TestBuildNFO_NoThumbs covers no thumbs
func TestBuildNFO_NoThumbs_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id: "NOCOVER-001",
	})
	assert.Empty(t, nfo.Thumb)
}

// TestBuildNFO_Fanart covers fanart mapping
func TestBuildNFO_Fanart_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id: "FAN-001",
		fanart: &fanart{
			Thumbs: []Thumb{
				{Value: "http://ss1.jpg"},
				{Value: "http://ss2.jpg"},
			},
		},
	})
	require.NotNil(t, nfo.Fanart)
	require.Len(t, nfo.Fanart.Thumbs, 2)
	assert.Equal(t, "http://ss1.jpg", nfo.Fanart.Thumbs[0].Value)
	assert.Equal(t, "http://ss2.jpg", nfo.Fanart.Thumbs[1].Value)
}

// TestBuildNFO_NoFanart covers no fanart
func TestBuildNFO_NoFanart_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id: "NOFAN-001",
	})
	assert.Nil(t, nfo.Fanart)
}

// TestBuildNFO_Trailer covers trailer mapping
func TestBuildNFO_Trailer_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id:         "TRAIL-001",
		trailerURL: "http://trailer.mp4",
	})
	assert.Equal(t, "http://trailer.mp4", nfo.Trailer)
}

// TestBuildNFO_NoTrailer covers no trailer
func TestBuildNFO_NoTrailer_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id: "NOTRAIL-001",
	})
	assert.Equal(t, "", nfo.Trailer)
}

// TestBuildNFO_OriginalPath covers original path mapping
func TestBuildNFO_OriginalPath_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id:           "ORIG-001",
		originalPath: "/original/path/video.mp4",
	})
	assert.Equal(t, "/original/path/video.mp4", nfo.OriginalPath)
}

// TestBuildNFO_NoOriginalPath covers no original path
func TestBuildNFO_NoOriginalPath_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	nfo := g.buildNFO(nfoInput{
		id: "ORIG-002",
	})
	assert.Equal(t, "", nfo.OriginalPath)
}

// --- Partial line coverage for mergeTags (extracted from buildNFO) ---

// TestMergeTags_ActressAsTag covers ActressAsTag config
func TestMergeTags_ActressAsTag_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, ActressAsTag: true}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags([]actor{{Name: "Actress A"}}, nil)
	assert.Contains(t, tags, "Actress A")
}

// TestMergeTags_ActressAsTag_SkipsUnknown covers ActressAsTag skipping unknown
func TestMergeTags_ActressAsTag_SkipsUnknown_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FirstNameOrder:     true,
		ActressAsTag:       true,
		UnknownActressText: "Unknown",
		UnknownActressMode: models.UnknownActressModeSkip,
	}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags([]actor{
		{Name: "Unknown"},
		{Name: "Actress B"},
	}, nil)
	assert.NotContains(t, tags, "Unknown")
	assert.Contains(t, tags, "Actress B")
}

// TestMergeTags_ActressAsTag_FallbackMode covers ActressAsTag with fallback mode (includes unknown)
func TestMergeTags_ActressAsTag_FallbackMode_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FirstNameOrder:     true,
		ActressAsTag:       true,
		UnknownActressText: "Unknown",
		UnknownActressMode: models.UnknownActressModeFallback,
	}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags([]actor{{Name: "Unknown"}}, nil)
	assert.Contains(t, tags, "Unknown")
}

// TestMergeTags_ActressAsTag_SkipsEmpty covers empty actress name skip
func TestMergeTags_ActressAsTag_SkipsEmpty_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, ActressAsTag: true}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags([]actor{{Name: ""}}, nil)
	assert.NotContains(t, tags, "")
}

// TestMergeTags_ActressDedup covers actress tag dedup with existing tags
func TestMergeTags_ActressDedup_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, ActressAsTag: true}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags([]actor{{Name: "Actress A"}}, []string{"Actress A"})
	// Should not duplicate
	count := 0
	for _, tag := range tags {
		if tag == "Actress A" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// TestMergeTags_CallerTags covers tags from caller
func TestMergeTags_CallerTags_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags(nil, []string{"Tag1", "Tag2"})
	assert.Contains(t, tags, "Tag1")
	assert.Contains(t, tags, "Tag2")
}

// TestMergeTags_SkipsEmpty covers empty tag skip
func TestMergeTags_SkipsEmpty_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags(nil, []string{"", "Tag1", ""})
	assert.NotContains(t, tags, "")
	assert.Contains(t, tags, "Tag1")
}

// TestMergeTags_CallerTagDedup covers tag dedup
func TestMergeTags_CallerTagDedup_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags(nil, []string{"Tag1", "Tag1"})
	count := 0
	for _, tag := range tags {
		if tag == "Tag1" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// TestMergeTags_ConfigTags covers tags from config
func TestMergeTags_ConfigTags_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Tag: []string{"ConfigTag1", "ConfigTag2"}}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags(nil, nil)
	assert.Contains(t, tags, "ConfigTag1")
	assert.Contains(t, tags, "ConfigTag2")
}

// TestMergeTags_ConfigTags_SkipsEmpty covers empty config tag skip
func TestMergeTags_ConfigTags_SkipsEmpty_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Tag: []string{"", "ConfigTag1"}}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags(nil, nil)
	assert.NotContains(t, tags, "")
	assert.Contains(t, tags, "ConfigTag1")
}

// TestMergeTags_ConfigTags_Dedup covers config tag dedup with existing
func TestMergeTags_ConfigTags_Dedup_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Tag: []string{"ExistingTag"}}
	g := NewGenerator(fs, cfg)

	tags := g.mergeTags(nil, []string{"ExistingTag"})
	count := 0
	for _, tag := range tags {
		if tag == "ExistingTag" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// --- Partial line coverage for transformMovieForNFO tagline/credits ---

// TestMovieToNFO_Tagline covers tagline config
func TestMovieToNFO_Tagline_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Tagline: "Best movie ever"}
	g := NewGenerator(fs, cfg)

	nfo := g.movieToNFO(context.Background(), &models.Movie{ID: "TL-001", DisplayTitle: "Test"}, "", nil)
	assert.Equal(t, "Best movie ever", nfo.Tagline)
}

// TestMovieToNFO_TaglineEmpty covers empty tagline
func TestMovieToNFO_TaglineEmpty_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Tagline: ""}
	g := NewGenerator(fs, cfg)

	nfo := g.movieToNFO(context.Background(), &models.Movie{ID: "TL-002", DisplayTitle: "Test"}, "", nil)
	assert.Equal(t, "", nfo.Tagline)
}

// TestMovieToNFO_Credits covers credits config
func TestMovieToNFO_Credits_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Credits: []string{"Writer A", "Writer B"}}
	g := NewGenerator(fs, cfg)

	nfo := g.movieToNFO(context.Background(), &models.Movie{ID: "CR-001", DisplayTitle: "Test"}, "", nil)
	assert.Equal(t, "Writer A, Writer B", nfo.Credits)
}

// TestMovieToNFO_NoCredits covers no credits
func TestMovieToNFO_NoCredits_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, Credits: nil}
	g := NewGenerator(fs, cfg)

	nfo := g.movieToNFO(context.Background(), &models.Movie{ID: "CR-002", DisplayTitle: "Test"}, "", nil)
	assert.Equal(t, "", nfo.Credits)
}

// --- Partial line coverage for extractStreamDetails ---

// TestExtractStreamDetails_CancelledContext covers ctx.Done()
func TestExtractStreamDetails_CancelledContext_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true, IncludeStreamDetails: true}
	g := NewGenerator(fs, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result := g.extractStreamDetails(ctx, "/some/video.mp4")
	assert.Nil(t, result)
}

// --- Partial line coverage for ResolveNFOFilename ---

// TestResolveNFOFilename_PartSuffixWithPerFile covers part suffix rendering
func TestResolveNFOFilename_PartSuffixWithPerFile_Partial(t *testing.T) {
	engine := template.NewEngine()
	movie := &models.Movie{ID: "PS-001"}

	result := ResolveNFOFilename(engine, movie, NFONameConfig{
		FilenameTemplate: "<ID>.nfo",
		PartSuffix:       "-pt1",
		PerFile:          true,
	})
	assert.Equal(t, "PS-001-pt1.nfo", result)
}

// TestResolveNFOFilename_PartSuffixWithoutPerFile covers part suffix not added when PerFile=false
func TestResolveNFOFilename_PartSuffixWithoutPerFile_Partial(t *testing.T) {
	engine := template.NewEngine()
	movie := &models.Movie{ID: "PS-002"}

	result := ResolveNFOFilename(engine, movie, NFONameConfig{
		FilenameTemplate: "<ID>.nfo",
		PartSuffix:       "-pt1",
		PerFile:          false,
	})
	assert.Equal(t, "PS-002.nfo", result)
}

// TestResolveNFOFilename_NilEngineFallback covers nil engine fallback
func TestResolveNFOFilename_NilEngineFallback_Partial(t *testing.T) {
	movie := &models.Movie{ID: "NIL-001"}

	result := ResolveNFOFilename(nil, movie, NFONameConfig{
		FilenameTemplate: "<ID>.nfo",
	})
	assert.Equal(t, "NIL-001.nfo", result)
}

// TestResolveNFOFilename_InvalidTemplateFallback covers template error fallback
func TestResolveNFOFilename_InvalidTemplateFallback_Partial(t *testing.T) {
	engine := template.NewEngine()
	movie := &models.Movie{ID: "FALL-001"}

	result := ResolveNFOFilename(engine, movie, NFONameConfig{
		FilenameTemplate: "<IF:A><IF:B><IF:C><IF:D><IF:E><IF:F><IF:G><IF:H><IF:I><IF:J><IF:K><IF:L><IF:M><IF:N><IF:O><IF:P><IF:Q><IF:R><IF:S><IF:T><IF:U><IF:V><IF:W><IF:X><IF:Y><IF:Z><IF:AA><IF:AB><IF:AC><IF:AD><IF:AE><IF:AF><IF:AG>x</IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF>",
	})
	assert.Equal(t, "FALL-001.nfo", result)
}

// TestResolveNFOFilename_InvalidTemplateEmptyID covers template error with empty ID
func TestResolveNFOFilename_InvalidTemplateEmptyID_Partial(t *testing.T) {
	engine := template.NewEngine()
	movie := &models.Movie{ID: ""}

	result := ResolveNFOFilename(engine, movie, NFONameConfig{
		FilenameTemplate: "<IF:A><IF:B><IF:C><IF:D><IF:E><IF:F><IF:G><IF:H><IF:I><IF:J><IF:K><IF:L><IF:M><IF:N><IF:O><IF:P><IF:Q><IF:R><IF:S><IF:T><IF:U><IF:V><IF:W><IF:X><IF:Y><IF:Z><IF:AA><IF:AB><IF:AC><IF:AD><IF:AE><IF:AF><IF:AG>x</IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF></IF>",
	})
	assert.Equal(t, "metadata.nfo", result) // Falls back to default
}

// TestResolveNFOFilename_SanitizedEmptyFallback covers sanitized empty filename fallback
func TestResolveNFOFilename_SanitizedEmptyFallback_Partial(t *testing.T) {
	engine := template.NewEngine()
	movie := &models.Movie{ID: "SE-001"}

	// Template that produces only a dash after sanitization
	result := ResolveNFOFilename(engine, movie, NFONameConfig{
		FilenameTemplate: "?*|",
	})
	// "?*|" sanitizes to "-" (| -> -), which is non-empty
	assert.Equal(t, "-.nfo", result)
}

// --- Partial line coverage for normalizeActressNameForDedup ---

// TestNormalizeActressNameForDedup_Cases covers various inputs
func TestNormalizeActressNameForDedup_Cases_Partial(t *testing.T) {
	assert.Equal(t, "", normalizeActressNameForDedup(""))
	assert.Equal(t, "", normalizeActressNameForDedup("   "))
	assert.Equal(t, "alice", normalizeActressNameForDedup("Alice"))
	assert.Equal(t, "alice smith", normalizeActressNameForDedup("  Alice   Smith  "))
}

// --- Partial line coverage for NewGenerator ---

// TestNewGenerator_NilConfigDefaults covers nil config defaulting
func TestNewGenerator_NilConfigDefaults_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, nil)
	require.NotNil(t, g)
	assert.Equal(t, "Unknown", g.config.UnknownActressText)
	assert.Equal(t, models.UnknownActressModeSkip, g.config.UnknownActressMode)
	assert.Equal(t, "<ID>.nfo", g.config.FilenameTemplate)
}

// TestNewGenerator_CustomTemplateEngine covers custom template engine injection
func TestNewGenerator_CustomTemplateEngine_Partial(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		TemplateEngine: template.NewEngine(),
	}
	g := NewGenerator(fs, cfg)
	require.NotNil(t, g)
}

// --- Partial line coverage for WriteNFO ---

// TestWriteNFO_MkdirFails covers directory creation failure
func TestWriteNFO_MkdirFails_Partial(t *testing.T) {
	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	err := g.WriteNFO(&Movie{ID: "test"}, "/readonly/output.nfo")
	assert.Error(t, err)
}

// TestWriteNFO_CreateFileFails covers file creation failure
func TestWriteNFO_CreateFileFails_Partial(t *testing.T) {
	// Use a base FS and wrap it to fail on Create
	fs := afero.NewMemMapFs()
	cfg := &Config{FirstNameOrder: true}
	g := NewGenerator(fs, cfg)

	// Create a read-only FS to trigger Create failure
	rofs := afero.NewReadOnlyFs(fs)
	g.fs = rofs

	err := g.WriteNFO(&Movie{ID: "test"}, "/output/test.nfo")
	assert.Error(t, err)
}
