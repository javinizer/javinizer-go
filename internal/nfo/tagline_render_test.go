package nfo

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

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
			nfo, _ := g.movieToNFO(context.Background(), movie, "", nil)
			assert.Equal(t, tc.want, nfo.Tagline)
		})
	}
}

func TestMovieToNFO_TaglineTemplating_DefaultDelimiterRespected(t *testing.T) {
	// An empty ActressDelimiter config must behave like the other template
	// surfaces (folder/file formats): fall back to ", ", not render empty.
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<ACTRESSES>"})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", nil)
	assert.Equal(t, "Sakura Momo, Actress Test", nfo.Tagline)
}

func TestMovieToNFO_TagsTemplating(t *testing.T) {
	t.Run("static tags pass through unchanged", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"Pool", "Collector"}, FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", []string{"Manual"})
		assert.Contains(t, nfo.Tags, "Pool")
		assert.Contains(t, nfo.Tags, "Collector")
		assert.Contains(t, nfo.Tags, "Manual")
		assert.Len(t, nfo.Tags, 3)
	})

	t.Run("config tag templates expand per movie", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<ID>", "Pool"}, FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", nil)
		assert.Contains(t, nfo.Tags, "IPX-535")
		assert.Contains(t, nfo.Tags, "Pool")
	})

	t.Run("caller (database) tags pass through verbatim", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", []string{"<MAKER>"})
		assert.Contains(t, nfo.Tags, "<MAKER>")
		assert.NotContains(t, nfo.Tags, "Idea Pocket")
	})

	t.Run("broken config tag templates dropped; caller tags kept verbatim", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<IF:ID>oops", "Kept"}, FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", []string{"<NOTAREALTAG>", "AlsoKept"})
		assert.Contains(t, nfo.Tags, "Kept")
		assert.Contains(t, nfo.Tags, "<NOTAREALTAG>")
		assert.Contains(t, nfo.Tags, "AlsoKept")
		assert.Len(t, nfo.Tags, 3)
	})

	t.Run("template result deduped against existing tag", func(t *testing.T) {
		g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<ID>"}, FirstNameOrder: true})
		nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", []string{"IPX-535"})
		assert.Equal(t, []string{"IPX-535"}, nfo.Tags)
	})
}

func TestMovieToNFO_CanceledContextPropagatesTagline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "<ID>", FirstNameOrder: true})
	nfo, err := g.movieToNFO(ctx, taglineTestMovie(), "", nil)
	assert.Error(t, err)
	assert.Nil(t, nfo)
}

func TestMovieToNFO_DeadlineExceededPropagatesConfigTag(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<ID>"}, FirstNameOrder: true})
	nfo, err := g.movieToNFO(ctx, taglineTestMovie(), "", nil)
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
	ctx := g.movieTemplateContext(taglineTestMovie(), "/movies/IPX-535.mp4")
	assert.Equal(t, "/movies/IPX-535.mp4", ctx.VideoFilePath)

	ctxEmpty := g.movieTemplateContext(taglineTestMovie(), "")
	assert.Equal(t, "", ctxEmpty.VideoFilePath)
}

func TestMergeTags_ConfigTagThreadsVideoFilePath(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"<RESOLUTION>"}, FirstNameOrder: true})
	tmplCtx := g.movieTemplateContext(taglineTestMovie(), "/movies/IPX-535.mp4")
	tags, err := g.mergeTags(context.Background(), tmplCtx, nil, nil)
	assert.NoError(t, err)
	_ = tags
}

func TestMovieToNFO_ConfigTagWithUnknownTokenDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tag: []string{"Featured: <TITEL>", "Kept"}, FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", nil)
	assert.NotContains(t, nfo.Tags, "Featured: ")
	assert.NotContains(t, nfo.Tags, "Featured: <TITEL>")
	assert.Contains(t, nfo.Tags, "Kept")
}

func TestMovieToNFO_TaglineWithUnknownTokenDropped(t *testing.T) {
	g := NewGenerator(afero.NewMemMapFs(), &Config{Tagline: "Featured: <TITEL>", FirstNameOrder: true})
	nfo, _ := g.movieToNFO(context.Background(), taglineTestMovie(), "", nil)
	assert.Equal(t, "", nfo.Tagline)
}
