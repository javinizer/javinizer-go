package downloader

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Record the actual caller context while still exercising the production engine.
type renderOptionsRecordingEngine struct {
	template.EngineInterface
	contexts []*template.Context
	err      error
}

func (e *renderOptionsRecordingEngine) Execute(format string, ctx *template.Context) (string, error) {
	e.contexts = append(e.contexts, ctx.Clone())
	if e.err != nil {
		return "", e.err
	}
	return e.EngineInterface.Execute(format, ctx)
}

func TestTemplateRenderOptionsDownloaderCallers(t *testing.T) {
	first := models.Actress{ID: 1, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true}
	second := models.Actress{ID: 2, FirstName: "Ai", LastName: "Uehara", JapaneseName: "上原亜衣", Verified: true}
	legacy := &models.Movie{ID: "POLICY-001", Actresses: []models.Actress{first, second}}
	credited := legacy.Clone()
	credited.Credits = []models.MovieCredit{
		{ActressID: first.ID, Actress: &first, CreditedName: "Stage Name"},
		{ActressID: second.ID, Actress: &second, UserOverride: true, OverrideName: "Pinned Name"},
		{Actress: &models.Actress{FirstName: "Hidden", Verified: true}, CreditedName: "Hidden Credit", Suppressed: true},
	}
	suppressed := legacy.Clone()
	suppressed.Credits = []models.MovieCredit{{Actress: &first, Suppressed: true}}

	cases := []struct {
		name      string
		movie     *models.Movie
		cfg       Config
		wantNames string
		wantList  []string
	}{
		{name: "zero policy", movie: legacy, wantNames: "Hatano Yui, Uehara Ai", wantList: []string{"Hatano Yui", "Uehara Ai"}},
		{name: "first order and delimiter", movie: legacy, cfg: Config{ActorFirstNameOrder: true, ActressDelimiter: " + "}, wantNames: "Yui Hatano + Ai Uehara", wantList: []string{"Yui Hatano", "Ai Uehara"}},
		{name: "Japanese rendering retains Latin list", movie: legacy, cfg: Config{ActorJapaneseNames: true, ActorFirstNameOrder: true, ActressDelimiter: " + "}, wantNames: "波多野結衣 + 上原亜衣", wantList: []string{"Yui Hatano", "Ai Uehara"}},
		{name: "default group threshold and name", movie: legacy, cfg: Config{MediaFormatConfig: organizer.MediaFormatConfig{GroupActress: true}}, wantNames: "@Group", wantList: []string{"Hatano Yui", "Uehara Ai"}},
		{name: "below custom threshold", movie: legacy, cfg: Config{MediaFormatConfig: organizer.MediaFormatConfig{GroupActress: true, GroupActressMin: 3, GroupActressName: "Cast"}}, wantNames: "Hatano Yui, Uehara Ai", wantList: []string{"Hatano Yui", "Uehara Ai"}},
		{name: "at custom threshold", movie: legacy, cfg: Config{MediaFormatConfig: organizer.MediaFormatConfig{GroupActress: true, GroupActressMin: 2, GroupActressName: "Cast"}}, wantNames: "Cast", wantList: []string{"Hatano Yui", "Uehara Ai"}},
		{name: "unknown defaults to skip", movie: &models.Movie{}, cfg: Config{MediaFormatConfig: organizer.MediaFormatConfig{GroupActress: true, GroupUnknownActressName: "Missing"}}, wantNames: ""},
		{name: "unknown explicit skip", movie: &models.Movie{}, cfg: Config{MediaFormatConfig: organizer.MediaFormatConfig{GroupActress: true, GroupUnknownActressName: "Missing"}, UnknownActressMode: models.UnknownActressModeSkip}, wantNames: ""},
		{name: "unknown default fallback name", movie: &models.Movie{}, cfg: Config{MediaFormatConfig: organizer.MediaFormatConfig{GroupActress: true}, UnknownActressMode: models.UnknownActressModeFallback}, wantNames: "@Unknown"},
		{name: "unknown custom fallback name", movie: &models.Movie{}, cfg: Config{MediaFormatConfig: organizer.MediaFormatConfig{GroupActress: true, GroupUnknownActressName: "Missing"}, UnknownActressMode: models.UnknownActressModeFallback}, wantNames: "Missing"},
		{name: "canonical and override", movie: credited, cfg: Config{ActorFirstNameOrder: true}, wantNames: "Yui Hatano, Pinned Name", wantList: []string{"Yui Hatano", "Pinned Name"}},
		{name: "credit selection outranks Japanese", movie: credited, cfg: Config{ActorJapaneseNames: true, UseCreditedName: true}, wantNames: "Stage Name, Pinned Name", wantList: []string{"Stage Name", "Pinned Name"}},
		{name: "suppressed credits do not revive legacy cast", movie: suppressed, cfg: Config{UseCreditedName: true}, wantNames: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := &renderOptionsRecordingEngine{EngineInterface: template.NewEngine()}
			d := NewDownloader(nil, afero.NewMemMapFs(), &tc.cfg, engine)
			before := tc.movie.Clone()
			media := d.buildTemplateContext(tc.movie, nil)
			require.Equal(t, tc.wantList, media.Actresses)
			require.Zero(t, media.Index)
			require.False(t, media.IsMultiPart)
			require.Zero(t, media.PartNumber)
			require.Empty(t, media.PartSuffix)
			require.Equal(t, tc.cfg.ActorFirstNameOrder, media.FirstNameOrder)
			require.Equal(t, tc.cfg.ActorJapaneseNames, media.ActressLanguageJa)
			require.Equal(t, tc.cfg.ActressDelimiter, media.ActressDelimiter)
			require.Equal(t, tc.cfg.GroupActress, media.GroupActress)
			require.Equal(t, tc.cfg.GroupActressMin, media.GroupActressMin)
			require.Equal(t, tc.cfg.GroupActressName, media.GroupActressName)
			require.Equal(t, tc.cfg.GroupUnknownActressName, media.GroupUnknownActressName)
			require.Equal(t, tc.cfg.UnknownActressMode, media.UnknownActressMode)
			got, err := engine.Execute("<ACTORS>", media)
			require.NoError(t, err)
			require.Equal(t, tc.wantNames, got)

			// The portrait caller adds an explicit image name but otherwise must
			// pass the same render policy and credit projection as other media.
			require.Equal(t, "Portrait--"+tc.wantNames+".jpg", d.generateActressFilename(tc.movie, "Portrait", "<ACTORNAME>--<ACTORS>.jpg"))
			require.Len(t, engine.contexts, 2)
			portrait := engine.contexts[1]
			require.Equal(t, "Portrait", portrait.ActressName)
			portrait.ActressName = ""
			require.Equal(t, media, portrait)
			require.Equal(t, before, tc.movie, "context construction must not mutate cast or credits")
		})
	}
}

func TestTemplateRenderOptionsDownloaderMultipartAndFallback(t *testing.T) {
	engine := &renderOptionsRecordingEngine{EngineInterface: template.NewEngine()}
	d := NewDownloader(nil, afero.NewMemMapFs(), &Config{ActorFirstNameOrder: true}, engine)
	movie := &models.Movie{ID: "PART-001", Actresses: []models.Actress{{FirstName: "Yui", LastName: "Hatano"}}}
	ctx := d.buildTemplateContext(movie, &MultipartInfo{IsMultiPart: true, PartNumber: 2, PartSuffix: "-B"})
	got, err := engine.Execute("<ID>-<PART:02><PARTSUFFIX>-<MULTIPART>-<ACTORS>", ctx)
	require.NoError(t, err)
	require.Equal(t, "PART-001-02-B-true-Yui Hatano", got)
	require.Zero(t, ctx.Index)

	require.Empty(t, d.generateActressFilename(movie, "Portrait", ""))
	require.Len(t, engine.contexts, 1, "an empty portrait format never invokes the engine")
	engine.err = errors.New("render failed")
	require.Equal(t, "Bad-Name.jpg", d.generateActressFilename(movie, "Bad/Name", "<ACTORS>"))
	require.Len(t, engine.contexts, 2)
	require.False(t, engine.contexts[1].IsMultiPart, "portrait context does not inherit media multipart state")
	require.Zero(t, engine.contexts[1].PartNumber)
}

func TestTemplateRenderOptionsDownloaderPolicySources(t *testing.T) {
	cfg := &Config{
		MediaFormatConfig: organizer.MediaFormatConfig{
			FirstNameOrder: false, ActressLanguageJA: false, ActressDelimiter: " WRONG ", UnknownActressMode: models.UnknownActressModeSkip,
			GroupActress: true, GroupActressMin: 3, GroupActressName: "Cast", GroupUnknownActressName: "Missing",
		},
		ActorFirstNameOrder: true, ActorJapaneseNames: true, ActressDelimiter: " + ", UnknownActressMode: models.UnknownActressModeFallback,
	}
	engine := &renderOptionsRecordingEngine{EngineInterface: template.NewEngine()}
	d := NewDownloader(nil, afero.NewMemMapFs(), cfg, engine)
	cfg.ActorFirstNameOrder = false // This field is snapshotted by NewDownloader.
	movie := &models.Movie{Actresses: []models.Actress{{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}, {FirstName: "Ai", LastName: "Uehara"}}}
	ctx := d.buildTemplateContext(movie, nil)
	require.True(t, ctx.FirstNameOrder)
	require.True(t, ctx.ActressLanguageJa)
	require.Equal(t, " + ", ctx.ActressDelimiter)
	require.Equal(t, models.UnknownActressModeFallback, ctx.UnknownActressMode)
	require.Equal(t, []string{"Yui Hatano", "Ai Uehara"}, ctx.Actresses)
	require.Equal(t, "Portrait--波多野結衣 + Ai Uehara.jpg", d.generateActressFilename(movie, "Portrait", "<ACTORNAME>--<ACTORS>.jpg"))
	require.Len(t, engine.contexts, 1)
	engine.contexts[0].ActressName = ""
	require.Equal(t, ctx, engine.contexts[0])
	require.Equal(t, "Portrait--Missing.jpg", d.generateActressFilename(&models.Movie{}, "Portrait", "<ACTORNAME>--<ACTORS>.jpg"))
}
