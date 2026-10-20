package organizer

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/require"
)

type renderPolicyRecordingEngine struct {
	template.EngineInterface
	contexts []*template.Context
}

func (e *renderPolicyRecordingEngine) Execute(format string, ctx *template.Context) (string, error) {
	e.contexts = append(e.contexts, ctx.Clone())
	return e.EngineInterface.Execute(format, ctx)
}

// Pin both organizer callers, including the outer Config fields that shadow
// identically named MediaFormatConfig fields. Constructor names and rendered
// tags deliberately differ under Japanese preference and group substitution.
func TestOrganizerTemplateRenderPolicy(t *testing.T) {
	yui := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true}
	aoi := models.Actress{FirstName: "Aoi", LastName: "Tsukasa", JapaneseName: "葵つかさ", Verified: true}
	legacy := &models.Movie{ID: "MOVIE-123", Actresses: []models.Actress{yui, aoi}}
	credits := &models.Movie{
		ID: "MOVIE-123", Actresses: []models.Actress{{FirstName: "Stale projection"}},
		Credits: []models.MovieCredit{
			{Actress: &yui, CreditedName: "Screen Alias"},
			{Actress: &aoi, CreditedName: "Ignored Alias", DisplayForceCanonical: true},
			{Actress: &yui, UserOverride: true, OverrideName: "Editor Override"},
			{Actress: &yui, CreditedName: "Suppressed", Suppressed: true},
			{Actress: &models.Actress{FirstName: "Quarantined", AmbiguityQuarantined: true}},
		},
	}
	for _, tc := range []struct {
		name       string
		movie      *models.Movie
		first, ja  bool
		credited   bool
		minimum    int
		mode       models.UnknownActressMode
		wantNames  []string
		wantRender string
	}{
		{"outer Roman order and delimiter", legacy, true, false, false, 3, models.UnknownActressModeFallback, []string{"Yui Hatano", "Aoi Tsukasa"}, "Yui Hatano + Aoi Tsukasa"},
		{"outer Japanese preference", legacy, false, true, false, 3, models.UnknownActressModeFallback, []string{"Hatano Yui", "Tsukasa Aoi"}, "波多野結衣 + 葵つかさ"},
		{"credited override and forced canonical", credits, true, true, true, 4, models.UnknownActressModeFallback, []string{"Screen Alias", "Aoi Tsukasa", "Editor Override"}, "Screen Alias + 葵つかさ + Editor Override"},
		{"group threshold counts visible credits", credits, true, true, true, 3, models.UnknownActressModeFallback, []string{"Screen Alias", "Aoi Tsukasa", "Editor Override"}, "Ensemble"},
		{"unknown custom fallback", &models.Movie{Actresses: []models.Actress{{FirstName: "Unknown"}}}, true, true, false, 3, models.UnknownActressModeFallback, []string{"Unknown"}, "Uncredited"},
		{"empty skipped actors retain filename fallback", &models.Movie{}, true, true, false, 3, models.UnknownActressModeSkip, nil, ""},
		{"suppressed credits do not revive legacy projection", &models.Movie{Actresses: legacy.Actresses, Credits: []models.MovieCredit{{Actress: &yui, Suppressed: true}}}, true, true, true, 3, models.UnknownActressModeFallback, nil, "Uncredited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				RenameFile: true, FileFormat: "<ACTORS>", FolderFormat: "<ACTORS>",
				FirstNameOrder: tc.first, ActressLanguageJA: tc.ja, ActressDelimiter: " + ", UseCreditedName: tc.credited,
				MediaFormatConfig: MediaFormatConfig{
					FirstNameOrder: !tc.first, ActressLanguageJA: !tc.ja, ActressDelimiter: "wrong delimiter",
					GroupActress: true, GroupActressMin: tc.minimum, GroupActressName: "Ensemble",
					GroupUnknownActressName: "Uncredited", UnknownActressMode: tc.mode,
				},
			}
			match := models.FileMatchInfo{MovieID: "MATCH-123", Name: "original.mkv", Extension: ".mkv"}
			before := tc.movie.Clone()
			engine := &renderPolicyRecordingEngine{EngineInterface: template.NewEngine()}
			pc := buildPlanContext(cfg, engine, tc.movie, match)
			require.NoError(t, pc.Err)
			base := resolveBaseFileName(cfg, engine, tc.movie, match)
			require.Len(t, engine.contexts, 3)
			for _, ctx := range []*template.Context{pc.Ctx, engine.contexts[2]} {
				require.Equal(t, tc.wantNames, ctx.Actresses)
				require.Equal(t, tc.first, ctx.FirstNameOrder)
				require.Equal(t, tc.ja, ctx.ActressLanguageJa)
				require.Equal(t, " + ", ctx.ActressDelimiter)
				require.True(t, ctx.GroupActress)
				require.Equal(t, tc.minimum, ctx.GroupActressMin)
				require.Equal(t, "Ensemble", ctx.GroupActressName)
				require.Equal(t, "Uncredited", ctx.GroupUnknownActressName)
				require.Equal(t, tc.mode, ctx.UnknownActressMode)
				got, err := template.NewEngine().Execute("<ACTORS>", ctx)
				require.NoError(t, err)
				require.Equal(t, tc.wantRender, got)
			}
			want := tc.wantRender
			if want == "" {
				want = match.MovieID
			}
			require.Equal(t, want+".mkv", pc.FileName)
			require.Equal(t, want, pc.FolderName)
			require.Equal(t, want, base)
			require.Equal(t, before, tc.movie, "constructing either context must not mutate movie data")
		})
	}
}

func TestOrganizerTemplateRenderPolicyKeepsTruncationAndMultipartLocal(t *testing.T) {
	movie := &models.Movie{
		ID: "MOVIE-123", Title: "ABCDEFGHIJKLM", OriginalTitle: "NOPQRSTUVWXYZ", OriginalFileName: "source.mkv",
		Translations: []models.MovieTranslation{{Language: "en", Title: "abcdefghijklm", OriginalTitle: "nopqrstuvwxyz"}},
	}
	before := movie.Clone()
	cfg := &Config{RenameFile: true, FileFormat: "<TITLE><PARTSUFFIX>", FolderFormat: "<ID>", MaxTitleLength: 8}
	match := models.FileMatchInfo{MovieID: "MATCH-123", Name: "source-pt2.mkv", Extension: ".mkv", PartNumber: 2, PartSuffix: "-pt2", IsMultiPart: true}
	engine := &renderPolicyRecordingEngine{EngineInterface: template.NewEngine()}
	pc := buildPlanContext(cfg, engine, movie, match)
	require.NoError(t, pc.Err)
	base := resolveBaseFileName(cfg, engine, movie, match)
	require.Len(t, engine.contexts, 3)
	baseCtx := engine.contexts[2]
	for _, ctx := range []*template.Context{pc.Ctx, baseCtx} {
		require.Equal(t, "ABCDE...", ctx.Title)
		require.Equal(t, "NOPQR...", ctx.OriginalTitle)
		require.Equal(t, "abcde...", ctx.Translations["en"].Title)
		require.Equal(t, "nopqr...", ctx.Translations["en"].OriginalTitle)
		require.Equal(t, "source.mkv", ctx.OriginalFilename)
		require.Empty(t, ctx.VideoFilePath)
	}
	require.Equal(t, 2, pc.Ctx.PartNumber)
	require.Equal(t, "-pt2", pc.Ctx.PartSuffix)
	require.True(t, pc.Ctx.IsMultiPart)
	require.Zero(t, baseCtx.PartNumber)
	require.Empty(t, baseCtx.PartSuffix)
	require.False(t, baseCtx.IsMultiPart)
	require.Equal(t, "ABCDE...-pt2.mkv", pc.FileName)
	require.Equal(t, "ABCDE", base, "base name omits multipart suffix and retains filename sanitization")
	require.Equal(t, before, movie)
}
