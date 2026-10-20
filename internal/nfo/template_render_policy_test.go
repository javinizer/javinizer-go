package nfo

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Context construction must retain the preformatted name list while each NFO
// surface applies the configured tag-rendering policy independently.
func TestNFOTemplateRenderPolicy(t *testing.T) {
	yui := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true}
	aoi := models.Actress{FirstName: "Aoi", LastName: "Tsukasa", JapaneseName: "葵つかさ", Verified: true}
	movie := &models.Movie{ID: "MOVIE-123", Actresses: []models.Actress{yui, aoi}}
	credits := &models.Movie{
		ID: "MOVIE-123", Actresses: []models.Actress{{FirstName: "Stale projection"}},
		Credits: []models.MovieCredit{
			{Actress: &yui, CreditedName: "Screen Alias"},
			{Actress: &aoi, CreditedName: "Ignored Alias", DisplayForceCanonical: true},
			{Actress: &yui, UserOverride: true, OverrideName: "Editor Override"},
			{Actress: &yui, Suppressed: true},
		},
	}
	for _, tc := range []struct {
		name       string
		movie      *models.Movie
		credited   bool
		minimum    int
		mode       models.UnknownActressMode
		wantNames  []string
		wantRender string
	}{
		{"Japanese tags preserve constructor order", movie, false, 3, models.UnknownActressModeFallback, []string{"Yui Hatano", "Aoi Tsukasa"}, "波多野結衣 + 葵つかさ"},
		{"credit selection before Japanese formatting", credits, true, 4, models.UnknownActressModeFallback, []string{"Screen Alias", "Aoi Tsukasa", "Editor Override"}, "Screen Alias + 葵つかさ + Editor Override"},
		{"group threshold counts visible credits", credits, true, 3, models.UnknownActressModeFallback, []string{"Screen Alias", "Aoi Tsukasa", "Editor Override"}, "Ensemble"},
		{"unknown custom fallback", &models.Movie{ID: "MOVIE-123", Actresses: []models.Actress{{FirstName: "Unknown"}}}, false, 3, models.UnknownActressModeFallback, []string{"Unknown"}, "Uncredited"},
		{"empty skipped actors", &models.Movie{ID: "MOVIE-123"}, false, 3, models.UnknownActressModeSkip, nil, ""},
		{"suppressed credits do not revive legacy projection", &models.Movie{ID: "MOVIE-123", Actresses: movie.Actresses, Credits: []models.MovieCredit{{Actress: &yui, Suppressed: true}}}, true, 3, models.UnknownActressModeFallback, nil, "Uncredited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				FilenameTemplate: "<ACTORS>", FirstNameOrder: true, ActressLanguageJA: true,
				ActressDelimiter: " + ", UseCreditedName: tc.credited,
				GroupActress: true, GroupActressMin: tc.minimum, GroupActressName: "Ensemble",
				GroupUnknownActressName: "Uncredited", UnknownActressMode: tc.mode,
			}
			before := tc.movie.Clone()
			g := NewGenerator(afero.NewMemMapFs(), cfg)
			nameCfg := cfg.ToNFONameConfig(false, "", 0)
			for name, ctx := range map[string]*template.Context{
				"configured text": g.movieTemplateContext(t.Context(), tc.movie, "", "", 0, false),
				"filename":        newNFOFilenameContext(tc.movie, nameCfg),
			} {
				t.Run(name, func(t *testing.T) {
					require.Equal(t, tc.wantNames, ctx.Actresses)
					require.True(t, ctx.FirstNameOrder)
					require.True(t, ctx.ActressLanguageJa)
					require.Equal(t, " + ", ctx.ActressDelimiter)
					require.True(t, ctx.GroupActress)
					require.Equal(t, tc.minimum, ctx.GroupActressMin)
					require.Equal(t, "Ensemble", ctx.GroupActressName)
					require.Equal(t, "Uncredited", ctx.GroupUnknownActressName)
					require.Equal(t, tc.mode, ctx.UnknownActressMode)
					got, err := template.NewEngine().Execute("<ACTORS>", ctx)
					require.NoError(t, err)
					require.Equal(t, tc.wantRender, got)
				})
			}
			wantName := tc.wantRender
			if wantName == "" {
				wantName = tc.movie.ID
			}
			require.Equal(t, wantName+".nfo", ResolveNFOFilename(template.NewEngine(), tc.movie, nameCfg))
			require.Equal(t, before, tc.movie)
		})
	}
}

func TestNFOTemplateRenderPolicyKeepsExecutionAndMultipartLocal(t *testing.T) {
	movie := &models.Movie{ID: "MOVIE-123", Title: "A long title is not truncated here", OriginalFileName: "source.mkv"}
	cfg := &Config{FilenameTemplate: "<ID><PARTSUFFIX>.nfo", PerFile: true, Tagline: "<TITLE><PARTSUFFIX><IF:MULTIPART> multipart</IF>"}
	g := NewGenerator(afero.NewMemMapFs(), cfg)
	nameCfg := cfg.ToNFONameConfig(true, "-pt2", 2)
	textCtx := g.movieTemplateContext(t.Context(), movie, "/media/source-pt2.mkv", "-pt2", 2, true)
	filenameCtx := newNFOFilenameContext(movie, nameCfg)
	for _, ctx := range []*template.Context{textCtx, filenameCtx} {
		require.Equal(t, movie.Title, ctx.Title)
		require.Equal(t, "source.mkv", ctx.OriginalFilename)
	}
	require.Equal(t, "/media/source-pt2.mkv", textCtx.VideoFilePath)
	require.Equal(t, "-pt2", textCtx.PartSuffix)
	require.Equal(t, 2, textCtx.PartNumber)
	require.True(t, textCtx.IsMultiPart)
	require.Empty(t, filenameCtx.VideoFilePath)
	require.Empty(t, filenameCtx.PartSuffix)
	require.Zero(t, filenameCtx.PartNumber)
	require.False(t, filenameCtx.IsMultiPart)
	tagline, err := g.resolveTagline(t.Context(), textCtx)
	require.NoError(t, err)
	require.Equal(t, movie.Title+"-pt2 multipart", tagline)
	require.Equal(t, "MOVIE-123-pt2.nfo", ResolveNFOFilename(template.NewEngine(), movie, nameCfg), "filename suffix is appended once, outside template context")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = g.resolveTagline(cancelled, textCtx)
	require.ErrorIs(t, err, context.Canceled)
}
