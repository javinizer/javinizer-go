package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type renderPolicyRecordingEngine struct {
	template.EngineInterface
	contexts    []*template.Context
	execContext context.Context
	err         error
}

func (e *renderPolicyRecordingEngine) Execute(format string, ctx *template.Context) (string, error) {
	e.contexts = append(e.contexts, ctx.Clone())
	if e.err != nil {
		return "", e.err
	}
	return e.EngineInterface.Execute(format, ctx)
}

func (e *renderPolicyRecordingEngine) ExecuteWithContext(execCtx context.Context, format string, ctx *template.Context) (string, error) {
	e.execContext = execCtx
	e.contexts = append(e.contexts, ctx.Clone())
	if e.err != nil {
		return "", e.err
	}
	return e.EngineInterface.ExecuteWithContext(execCtx, format, ctx)
}

type renderPolicyCase struct {
	name            string
	movie           *models.Movie
	cfg             nfo.NFONameConfig
	wantNames       string
	wantDisplayList []string
	wantMediaList   []string
}

func callerRenderPolicyCases() []renderPolicyCase {
	first := models.Actress{ID: 1, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true}
	second := models.Actress{ID: 2, FirstName: "Ai", LastName: "Uehara", JapaneseName: "上原亜衣", Verified: true}
	legacy := &models.Movie{ID: "POLICY-001", Title: "Scraped title", OriginalTitle: "Scraped original", Actresses: []models.Actress{first, second}}
	credited := legacy.Clone()
	credited.Credits = []models.MovieCredit{
		{ActressID: first.ID, Actress: &first, CreditedName: "Stage Name"},
		{ActressID: second.ID, Actress: &second, UserOverride: true, OverrideName: "Pinned Name"},
		{Actress: &models.Actress{FirstName: "Hidden", Verified: true}, CreditedName: "Hidden Credit", Suppressed: true},
	}
	forced := credited.Clone()
	forced.Credits[0].DisplayForceCanonical = true
	suppressed := legacy.Clone()
	suppressed.Credits = []models.MovieCredit{{Actress: &first, Suppressed: true}}
	last := []string{"Hatano Yui", "Uehara Ai"}
	firstOrder := []string{"Yui Hatano", "Ai Uehara"}
	return []renderPolicyCase{
		{name: "zero policy", movie: legacy, wantNames: "Hatano Yui, Uehara Ai", wantDisplayList: last, wantMediaList: last},
		{name: "first order and delimiter", movie: legacy, cfg: nfo.NFONameConfig{FirstNameOrder: true, ActressDelimiter: " + "}, wantNames: "Yui Hatano + Ai Uehara", wantDisplayList: last, wantMediaList: firstOrder},
		{name: "Japanese names", movie: legacy, cfg: nfo.NFONameConfig{FirstNameOrder: true, ActressLanguageJA: true, ActressDelimiter: " + "}, wantNames: "波多野結衣 + 上原亜衣", wantDisplayList: last, wantMediaList: firstOrder},
		{name: "default grouping", movie: legacy, cfg: nfo.NFONameConfig{GroupActress: true}, wantNames: "@Group", wantDisplayList: last, wantMediaList: last},
		{name: "below grouping threshold", movie: legacy, cfg: nfo.NFONameConfig{GroupActress: true, GroupActressMin: 3, GroupActressName: "Cast"}, wantNames: "Hatano Yui, Uehara Ai", wantDisplayList: last, wantMediaList: last},
		{name: "at grouping threshold", movie: legacy, cfg: nfo.NFONameConfig{GroupActress: true, GroupActressMin: 2, GroupActressName: "Cast"}, wantNames: "Cast", wantDisplayList: last, wantMediaList: last},
		{name: "unknown implicit skip", movie: &models.Movie{ID: "EMPTY-001"}, cfg: nfo.NFONameConfig{GroupActress: true, GroupUnknownActressName: "Missing"}},
		{name: "unknown explicit skip", movie: &models.Movie{ID: "EMPTY-001"}, cfg: nfo.NFONameConfig{GroupActress: true, GroupUnknownActressName: "Missing", UnknownActressMode: models.UnknownActressModeSkip}},
		{name: "unknown default fallback", movie: &models.Movie{ID: "EMPTY-001"}, cfg: nfo.NFONameConfig{GroupActress: true, UnknownActressMode: models.UnknownActressModeFallback}, wantNames: "@Unknown"},
		{name: "unknown custom fallback", movie: &models.Movie{ID: "EMPTY-001"}, cfg: nfo.NFONameConfig{GroupActress: true, GroupUnknownActressName: "Missing", UnknownActressMode: models.UnknownActressModeFallback}, wantNames: "Missing"},
		{name: "canonical and override", movie: credited, cfg: nfo.NFONameConfig{FirstNameOrder: true}, wantNames: "Yui Hatano, Pinned Name", wantDisplayList: []string{"Hatano Yui", "Pinned Name"}, wantMediaList: []string{"Yui Hatano", "Pinned Name"}},
		{name: "selected credit overrides Japanese", movie: credited, cfg: nfo.NFONameConfig{ActressLanguageJA: true, UseCreditedName: true}, wantNames: "Stage Name, Pinned Name", wantDisplayList: []string{"Stage Name", "Pinned Name"}, wantMediaList: []string{"Stage Name", "Pinned Name"}},
		{name: "forced canonical retains first order distinction", movie: forced, cfg: nfo.NFONameConfig{FirstNameOrder: true, UseCreditedName: true}, wantNames: "Yui Hatano, Pinned Name", wantDisplayList: []string{"Hatano Yui", "Pinned Name"}, wantMediaList: []string{"Yui Hatano", "Pinned Name"}},
		{name: "suppressed credits hide legacy cast", movie: suppressed, cfg: nfo.NFONameConfig{UseCreditedName: true}},
	}
}

func assertCallerRenderPolicy(t *testing.T, cfg nfo.NFONameConfig, ctx *template.Context) {
	t.Helper()
	require.Equal(t, cfg.FirstNameOrder, ctx.FirstNameOrder)
	require.Equal(t, cfg.ActressLanguageJA, ctx.ActressLanguageJa)
	require.Equal(t, cfg.ActressDelimiter, ctx.ActressDelimiter)
	require.Equal(t, cfg.GroupActress, ctx.GroupActress)
	require.Equal(t, cfg.GroupActressMin, ctx.GroupActressMin)
	require.Equal(t, cfg.GroupActressName, ctx.GroupActressName)
	require.Equal(t, cfg.GroupUnknownActressName, ctx.GroupUnknownActressName)
	require.Equal(t, cfg.UnknownActressMode, ctx.UnknownActressMode)
}

func TestTemplateRenderOptionsDisplayTitleCaller(t *testing.T) {
	for _, tc := range callerRenderPolicyCases() {
		t.Run(tc.name, func(t *testing.T) {
			movie := tc.movie.Clone()
			engine := &renderPolicyRecordingEngine{EngineInterface: template.NewEngine()}
			execCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &models.Movie{ID: "IGNORED-ID", Title: "Source title", OriginalTitle: "Source original", Actresses: []models.Actress{{FirstName: "Ignored cast"}}}
			ApplyDisplayTitleFromSource(execCtx, movie, source, "<ID>--<TITLE>--<ORIGINALTITLE>--<ACTORS>", engine, tc.cfg)
			require.Equal(t, movie.ID+"--Source title--Source original--"+tc.wantNames, movie.DisplayTitle)
			require.Same(t, execCtx, engine.execContext)
			require.Len(t, engine.contexts, 1)
			ctx := engine.contexts[0]
			assertCallerRenderPolicy(t, tc.cfg, ctx)
			// Display-title construction intentionally formats canonical Actresses
			// last-name-first before configuring FirstNameOrder for actual tags.
			require.Equal(t, tc.wantDisplayList, ctx.Actresses)
			require.Equal(t, "Source title", ctx.Title)
			require.Equal(t, "Source original", ctx.OriginalTitle)
			require.Equal(t, tc.movie.Actresses, movie.Actresses)
			require.Equal(t, tc.movie.Credits, movie.Credits)
		})
	}
}

func TestTemplateRenderOptionsDisplayTitleFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
		fail   bool
		source *models.Movie
		want   string
		calls  int
	}{
		{name: "empty source original retains movie original", format: "<TITLE>--<ORIGINALTITLE>", source: &models.Movie{Title: "Source"}, want: "Source--Original", calls: 1},
		{name: "empty format", source: &models.Movie{}, want: "Scraped"},
		{name: "nil source", format: "<ID>", want: "Scraped"},
		{name: "engine error replaces stale display title", format: "<ID>", source: &models.Movie{}, fail: true, want: "Scraped", calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &renderPolicyRecordingEngine{EngineInterface: template.NewEngine()}
			if tc.fail {
				engine.err = errors.New("render failed")
			}
			movie := &models.Movie{ID: "TITLE-001", Title: "Scraped", OriginalTitle: "Original", DisplayTitle: "Stale"}
			ApplyDisplayTitleFromSource(context.Background(), movie, tc.source, tc.format, engine, nfo.NFONameConfig{})
			require.Equal(t, tc.want, movie.DisplayTitle)
			require.Len(t, engine.contexts, tc.calls)
		})
	}
}

func TestTemplateRenderOptionsPreviewMediaCaller(t *testing.T) {
	for _, tc := range callerRenderPolicyCases() {
		t.Run(tc.name, func(t *testing.T) {
			movie := tc.movie.Clone()
			movie.TrailerURL = "https://example.invalid/trailer.mp4"
			movie.Screenshots = []string{"https://example.invalid/shot.jpg"}
			format := organizer.MediaFormatConfig{
				PosterFormat: "poster-<ACTORS>-<PART:02><PARTSUFFIX>.jpg", FanartFormat: "fanart-<ACTORS>-<PART:02><PARTSUFFIX>.jpg",
				TrailerFormat: "trailer-<ACTORS>.mp4", ScreenshotFormat: "shot-<ACTORS>-<INDEX:02>.jpg", ScreenshotFolder: "shots",
				GroupActress: tc.cfg.GroupActress, GroupActressMin: tc.cfg.GroupActressMin, GroupActressName: tc.cfg.GroupActressName,
				GroupUnknownActressName: tc.cfg.GroupUnknownActressName, UnknownActressMode: tc.cfg.UnknownActressMode,
				FirstNameOrder: tc.cfg.FirstNameOrder, ActressLanguageJA: tc.cfg.ActressLanguageJA, ActressDelimiter: tc.cfg.ActressDelimiter,
			}
			cfg := PreviewConfig{PathCfg: PreviewPathConfig{MediaFormatConfig: format}, UseCreditedName: tc.cfg.UseCreditedName, Downloads: downloadToggles{Poster: true, Cover: true, Trailer: true, Extrafanart: true}}
			engine := &renderPolicyRecordingEngine{EngineInterface: template.NewEngine()}
			// Deliberately conflicting NFO settings must not supply preview media policy.
			otherNFO := nfo.NFONameConfig{FirstNameOrder: !tc.cfg.FirstNameOrder, ActressLanguageJA: !tc.cfg.ActressLanguageJA, UseCreditedName: !tc.cfg.UseCreditedName, GroupActress: !tc.cfg.GroupActress, GroupActressName: "Wrong group", ActressDelimiter: " WRONG "}
			preview := newPreviewOrchestrator(afero.NewMemMapFs(), nil, cfg, otherNFO, engine, nil, nil).(*previewOrchImpl)
			files := []models.FileMatchInfo{{Path: "/source/part-B.mp4", IsMultiPart: true, PartNumber: 2, PartSuffix: "-B"}}
			paths := preview.resolveMediaPaths(movie, files, &organizer.OrganizePlan{}, organizer.EncodedPaths{TargetDir: "/library"}, nil, operationmode.OperationModeOrganize, true, false)
			require.Equal(t, filepath.Join("/library", "poster-"+tc.wantNames+"-02-B.jpg"), paths.PosterPath)
			require.Equal(t, filepath.Join("/library", "fanart-"+tc.wantNames+"-02-B.jpg"), paths.FanartPath)
			require.Equal(t, filepath.Join("/library", "trailer-"+tc.wantNames+".mp4"), paths.TrailerPath)
			require.Equal(t, []string{"shot-" + tc.wantNames + "-01.jpg"}, paths.Screenshots)
			require.Equal(t, filepath.Join("/library", "shots"), paths.ExtrafanartPath)
			require.Len(t, engine.contexts, 4)
			for _, ctx := range engine.contexts {
				assertCallerRenderPolicy(t, tc.cfg, ctx)
				require.Equal(t, tc.wantMediaList, ctx.Actresses)
			}
			for _, ctx := range engine.contexts[:2] {
				require.True(t, ctx.IsMultiPart)
				require.Equal(t, 2, ctx.PartNumber)
				require.Equal(t, "-B", ctx.PartSuffix)
			}
			require.Equal(t, 1, engine.contexts[2].Index)
			require.Zero(t, engine.contexts[3].Index)
			require.False(t, engine.contexts[2].IsMultiPart, "preview screenshot context is not seeded with poster multipart data")
			require.False(t, engine.contexts[3].IsMultiPart, "preview trailer context is not seeded with poster multipart data")
		})
	}
}
