package nfo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Issue #184: docs/UI advertised the tagline (and custom tags) as templates,
// but they were written verbatim. They are now expanded per movie; static
// text stays byte-identical and broken templates are dropped with a warning
// instead of failing NFO generation.

func taglineTestMovie() *models.Movie {
	return &models.Movie{
		ID:    "IPX-535",
		Title: "Beautiful Day",
		Maker: "Idea Pocket",
		Actresses: []models.Actress{
			{FirstName: "Momo", LastName: "Sakura", JapaneseName: "桜空もも"},
			{FirstName: "Test", LastName: "Actress"},
		},
	}
}

func TestMovieToNFO_TaglineTemplating(t *testing.T) {
	testCases := []struct {
		name             string
		tagline          string
		firstNameOrder   bool
		actressDelimiter string
		releaseYear      int
		movie            *models.Movie
		want             string
	}{
		{"static passthrough", "Brought to you by Javinizer", false, "", 0, taglineTestMovie(), "Brought to you by Javinizer"},
		{"literal unmatched angle brackets", "A <3 B production", false, "", 0, taglineTestMovie(), "A <3 B production"},
		{"empty stays empty", "", false, "", 0, taglineTestMovie(), ""},
		{"ID tag", "<ID>", false, "", 0, taglineTestMovie(), "IPX-535"},
		{"composed tags", "<ID> [<MAKER>]", false, "", 0, taglineTestMovie(), "IPX-535 [Idea Pocket]"},
		{"actresses default order and delimiter", "<ACTRESSES>", false, "", 0, taglineTestMovie(), "Sakura Momo, Actress Test"},
		{"actresses first-name order and custom delimiter", "<ACTRESSES>", true, " / ", 0, taglineTestMovie(), "Momo Sakura / Test Actress"},
		{"conditional with year present", "<ID><IF:YEAR> (<YEAR>)</IF>", false, "", 2020, taglineTestMovie(), "IPX-535 (2020)"},
		{"conditional with year absent", "<ID><IF:YEAR> (<YEAR>)</IF>", false, "", 0, taglineTestMovie(), "IPX-535"},
		{"unknown tag dropped", "<NOTAREALTAG>", false, "", 0, taglineTestMovie(), ""},
		{"malformed conditional dropped", "<IF:ID>oops", false, "", 0, taglineTestMovie(), ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				Tagline:          tc.tagline,
				FirstNameOrder:   tc.firstNameOrder,
				ActressDelimiter: tc.actressDelimiter,
			}
			movie := tc.movie
			if tc.releaseYear != 0 {
				m := *movie
				m.ReleaseYear = tc.releaseYear
				movie = &m
			}
			g := NewGenerator(afero.NewMemMapFs(), cfg)
			nfo, _ := g.movieToNFO(context.Background(), movie, "", "", 0, false, nil)
			assert.Equal(t, tc.want, nfo.Tagline)
		})
	}
}

func TestMovieToNFO_TaglineTemplating_DefaultDelimiterRespected(t *testing.T) {
	// An empty ActressDelimiter config must behave like the other template
	// surfaces (folder/file formats): fall back to ", ", not render empty.
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<ACTRESSES>"})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "Sakura Momo, Actress Test", nfo.Tagline)
}

func TestMovieToNFO_TagsTemplating(t *testing.T) {
	t.Run("static tags pass through unchanged", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"Pool", "Collector"}, FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, []string{"Manual"})
		assert.Contains(t, nfo.Tags, "Pool")
		assert.Contains(t, nfo.Tags, "Collector")
		assert.Contains(t, nfo.Tags, "Manual")
		assert.Len(t, nfo.Tags, 3)
	})

	t.Run("config tag templates expand per movie", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<ID>", "Pool"}, FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
		assert.Contains(t, nfo.Tags, "IPX-535")
		assert.Contains(t, nfo.Tags, "Pool")
	})

	t.Run("caller (database) tags pass through verbatim", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, []string{"<MAKER>"})
		assert.Contains(t, nfo.Tags, "<MAKER>")
		assert.NotContains(t, nfo.Tags, "Idea Pocket")
	})

	t.Run("broken config tag templates dropped; caller tags kept verbatim", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<IF:ID>oops", "Kept"}, FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, []string{"<NOTAREALTAG>", "AlsoKept"})
		assert.Contains(t, nfo.Tags, "Kept")
		assert.Contains(t, nfo.Tags, "<NOTAREALTAG>")
		assert.Contains(t, nfo.Tags, "AlsoKept")
		assert.Len(t, nfo.Tags, 3)
	})

	t.Run("template result deduped against existing tag", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<ID>"}, FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, []string{"IPX-535"})
		assert.Equal(t, []string{"IPX-535"}, nfo.Tags)
	})
}

func TestMovieToNFO_CanceledContextPropagatesTagline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<ID>", FirstNameOrder: true})
	nfo, err := g.movieToNFO(ctx, taglineTestMovie(), "", "", 0, false, nil)
	assert.Error(t, err)
	assert.Nil(t, nfo)
}

func TestMovieToNFO_DeadlineExceededPropagatesConfigTag(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<ID>"}, FirstNameOrder: true})
	nfo, err := g.movieToNFO(ctx, taglineTestMovie(), "", "", 0, false, nil)
	assert.Error(t, err)
	assert.Nil(t, nfo)
}

func TestGenerateAtPath_CanceledContextReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<ID>", FirstNameOrder: true})
	err := g.GenerateAtPath(ctx, taglineTestMovie(), "/out/test.nfo", "", nil)
	assert.Error(t, err)
}

func TestMovieTemplateContext_ThreadsVideoFilePath(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{FirstNameOrder: true})
	ctx := g.movieTemplateContext(context.Background(), taglineTestMovie(), "/movies/IPX-535.mp4", "-pt1", 0, true)
	assert.Equal(t, "/movies/IPX-535.mp4", ctx.VideoFilePath)
	assert.Equal(t, "-pt1", ctx.PartSuffix)
	assert.True(t, ctx.IsMultiPart)

	ctxEmpty := g.movieTemplateContext(context.Background(), taglineTestMovie(), "", "", 0, false)
	assert.Equal(t, "", ctxEmpty.VideoFilePath)
	assert.False(t, ctxEmpty.IsMultiPart)
}

func TestMergeTags_MediaTagUnresolvableRendersEmpty(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<RESOLUTION>"}, FirstNameOrder: true})
	tmplCtx := g.movieTemplateContext(context.Background(), taglineTestMovie(), "/movies/IPX-535.mp4", "", 0, false)
	tags, err := g.mergeTags(context.Background(), tmplCtx, nil, nil)
	assert.NoError(t, err)
	assert.Empty(t, tags)
}

func TestMovieToNFO_ConfigTagWithUnknownTokenDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"Featured: <TITEL>", "Kept"}, FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.NotContains(t, nfo.Tags, "Featured: ")
	assert.NotContains(t, nfo.Tags, "Featured: <TITEL>")
	assert.Contains(t, nfo.Tags, "Kept")
}

func TestMovieToNFO_TaglineWithUnknownTokenDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "Featured: <TITEL>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestMovieToNFO_TaglineConditionalWithElseNotDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<IF:SERIES><SERIES><ELSE>Standalone</IF>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "Standalone", nfo.Tagline)
}

func TestMovieToNFO_TaglineMultipartSuffixRendered(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<PARTSUFFIX>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "-pt1", 0, true, nil)
	assert.Equal(t, "-pt1", nfo.Tagline)
}

func TestMovieToNFO_TaglineMultipartConditional(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<IF:MULTIPART>Part<ELSE>Single</IF>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "-pt1", 0, true, nil)
	assert.Equal(t, "Part", nfo.Tagline)

	nfoSingle, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "Single", nfoSingle.Tagline)
}

func TestResolveAndGenerate_CanceledContextReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<ID>", FirstNameOrder: true})
	nameCfg := NFONameConfig{FilenameTemplate: "<ID>"}
	nfoPath, err := g.ResolveAndGenerate(ctx, taglineTestMovie(), "/out", nameCfg, "", nil)
	assert.Error(t, err)
	assert.Equal(t, "", nfoPath)
}

func TestMovieToNFO_TaglinePartNumberRendered(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "Part <PART>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "-pt1", 1, true, nil)
	assert.Equal(t, "Part 1", nfo.Tagline)
}

func TestMovieToNFO_TaglineNestedConditionalDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<IF:TITLE><IF:ID>deep</IF></IF>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestMovieToNFO_TaglineStrayElseDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "Featured<ELSE>Other", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestMovieToNFO_TaglineInvalidNumericModifierDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<PART:xyz>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "-pt1", 1, true, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestMovieToNFO_TaglineMultipleElseDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<IF:SERIES>a<ELSE>b<ELSE>c</IF>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestMovieToNFO_TaglineMultilineConditionalRendered(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<IF:TITLE>\nFeatured\n</IF>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "\nFeatured\n", nfo.Tagline)
}

func TestResolveAndGenerate_PerFileGating(t *testing.T) {
	t.Run("PerFile true threads part number into written tagline", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		g := NewGenerator(fs, &Config{Tagline: "Part <PART>", FirstNameOrder: true})
		nameCfg := NFONameConfig{FilenameTemplate: "<ID>", PerFile: true, PartSuffix: "-pt1", PartNumber: 1, IsMultiPart: true}
		path, err := g.ResolveAndGenerate(context.Background(), taglineTestMovie(), "/out", nameCfg, "", nil)
		require.NoError(t, err)
		require.NotEmpty(t, path)
		data, readErr := afero.ReadFile(fs, path)
		require.NoError(t, readErr)
		assert.Contains(t, string(data), "<tagline>Part 1</tagline>")
	})

	t.Run("PerFile false zeros part number in written tagline", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		g := NewGenerator(fs, &Config{Tagline: "Part <PART>", FirstNameOrder: true})
		nameCfg := NFONameConfig{FilenameTemplate: "<ID>", PerFile: false, PartSuffix: "-pt1", PartNumber: 1, IsMultiPart: true}
		path, err := g.ResolveAndGenerate(context.Background(), taglineTestMovie(), "/out", nameCfg, "", nil)
		require.NoError(t, err)
		require.NotEmpty(t, path)
		data, readErr := afero.ReadFile(fs, path)
		require.NoError(t, readErr)
		assert.Contains(t, string(data), "<tagline>Part </tagline>")
	})
}

func TestMovieToNFO_TaglineBareIfDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "A <IF> B", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestMovieToNFO_TaglineElseModifierDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<IF:SERIES>a<ELSE:b>c</IF>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestMovieToNFO_TaglineDegenerateIfColonDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<IF:>oops", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestMovieToNFO_TaglineSignedNumericModifierDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<PART:+2>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "-pt1", 1, true, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestResolveAndGenerate_PartNumberGatedOnIsMultiPart(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, &Config{Tagline: "Part <PART>", FirstNameOrder: true})
	nameCfg := NFONameConfig{FilenameTemplate: "<ID>", PerFile: true, PartSuffix: "", PartNumber: 1, IsMultiPart: false}
	path, err := g.ResolveAndGenerate(context.Background(), taglineTestMovie(), "/out", nameCfg, "", nil)
	require.NoError(t, err)
	data, readErr := afero.ReadFile(fs, path)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "<tagline>Part </tagline>")
}

func TestMovieToNFO_TaglineModifierWithAngleBracketDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<IF:TITLE><TITLE:</IF>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}

func TestGenerate_LegacyPathWritesNFOWithoutPartContent(t *testing.T) {
	fs := afero.NewMemMapFs()
	g := NewGenerator(fs, &Config{Tagline: "Part <PART>", PerFile: true, FirstNameOrder: true})
	movie := taglineTestMovie()
	err := g.Generate(context.Background(), movie, "/out", "-pt1", "", nil)
	assert.NoError(t, err)
	matches, _ := afero.Glob(fs, "/out/*.nfo")
	require.NotEmpty(t, matches)
	data, readErr := afero.ReadFile(fs, matches[0])
	require.NoError(t, readErr)
	// Legacy Generate path zeroes part content, so <PART> renders empty.
	assert.Contains(t, string(data), "<tagline>Part </tagline>")
}

func TestGenerate_LegacyPathPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<ID>", PerFile: true, FirstNameOrder: true, FilenameTemplate: "<ID>"})
	err := g.Generate(ctx, taglineTestMovie(), "/out", "", "", nil)
	assert.Error(t, err)
}

func TestMovieToNFO_TaglineResolutionReusesStreamAnalysis(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.mp4")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	require.NoError(t, os.WriteFile(tmpFile.Name(), []byte("fake video"), 0644))
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<RESOLUTION>", IncludeStreamDetails: true, FirstNameOrder: true})
	nfo, err := g.movieToNFO(context.Background(), taglineTestMovie(), tmpFile.Name(), "", 0, false, nil)
	assert.NoError(t, err)
	_ = nfo
}

func TestMovieToNFO_StreamDetailsEnabledSeedsMediaCache(t *testing.T) {
	stub := &stubMediaAnalyzer{Details: &streamDetails{Video: []videoStream{{Codec: "h264", Width: 1920, Height: 1080}}}}
	gen := newGeneratorWithAnalyzer(afero.NewMemMapFs(), &Config{Tagline: "<RESOLUTION>", IncludeStreamDetails: true, FirstNameOrder: true}, stub)
	nfo, err := g_movieToNFO(gen, taglineTestMovie(), "/fake/path.mp4", "", 0, false, nil)
	assert.NoError(t, err)
	_ = nfo
}

func g_movieToNFO(g *Generator, movie *models.Movie, videoFilePath, partSuffix string, partNumber int, isMultiPart bool, tags []string) (*Movie, error) {
	return g.movieToNFO(context.Background(), movie, videoFilePath, partSuffix, partNumber, isMultiPart, tags)
}

func TestMovieToNFO_SeedsSharedMediaInfoWhenStreamDetailsEnabled(t *testing.T) {
	stub := &stubMediaAnalyzer{Details: &streamDetails{Video: []videoStream{{Codec: "h264", Width: 1920, Height: 1080}}}}
	gen := newGeneratorWithAnalyzer(afero.NewMemMapFs(), &Config{Tagline: "<RESOLUTION>", IncludeStreamDetails: true, FirstNameOrder: true}, stub)
	gen.mediaInfoAnalyze = func(_ context.Context, _ string) (*mediainfo.VideoInfo, error) {
		return &mediainfo.VideoInfo{Width: 1920, Height: 1080}, nil
	}
	nfo, err := gen.movieToNFO(context.Background(), taglineTestMovie(), "/fake/path.mp4", "", 0, false, nil)
	assert.NoError(t, err)
	assert.Equal(t, "1080p", nfo.Tagline)
}

func TestSeedSharedMediaInfo_NilFileInfoNoOp(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{FirstNameOrder: true})
	tmplCtx := g.movieTemplateContext(context.Background(), taglineTestMovie(), "/fake.mp4", "", 0, false)
	g.seedSharedMediaInfo(context.Background(), nil, "/fake.mp4", tmplCtx)
	assert.True(t, true)
}

func TestMovieToNFO_TaglineMalformedTagWithDigitDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "Featured: <TITLE2>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", "", 0, false, nil)
	assert.Equal(t, "", nfo.Tagline)
}
