package template

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestRenderOptionsNewContextPreservesConstructionPolicy(t *testing.T) {
	identity := models.Actress{FirstName: "Given", LastName: "Family", JapaneseName: "日本名", Verified: true}
	movie := &models.Movie{
		ID: "MOV-001", Title: "Title", Actresses: []models.Actress{identity},
		Credits: []models.MovieCredit{{Actress: &identity, CreditedName: "Reported Name"}},
	}
	for _, tc := range []struct {
		name string
		opts ContextOptions
		want string
	}{
		{"zero options", ContextOptions{}, "Family Given"},
		{"first name", ContextOptions{FirstNameOrder: true}, "Given Family"},
		{"canonical credit", ContextOptions{RenderCredits: true}, "Family Given"},
		{"selected credit", ContextOptions{RenderCredits: true, UseCreditedName: true}, "Reported Name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := RenderOptions{
				ContextOptions: tc.opts,
				GroupActress:   true, GroupActressMin: 3,
				GroupActressName: "Ensemble", GroupUnknownActressName: "Nobody",
				UnknownActressMode: models.UnknownActressModeFallback,
				ActressLanguageJa:  true, ActressDelimiter: " + ",
			}
			got := opts.NewContext(movie)
			require.Equal(t, []string{tc.want}, got.Actresses)
			want := NewContextFromMovieWithOptions(movie, tc.opts)
			want.GroupActress = true
			want.GroupActressMin = 3
			want.GroupActressName = "Ensemble"
			want.GroupUnknownActressName = "Nobody"
			want.UnknownActressMode = models.UnknownActressModeFallback
			want.ActressLanguageJa = true
			want.ActressDelimiter = " + "
			require.Equal(t, want, got)
		})
	}
	require.Equal(t, NewContextFromMovie(movie), (RenderOptions{}).NewContext(movie))
}

func TestRenderOptionsApplyToChangesOnlyRenderingPolicy(t *testing.T) {
	identity := models.Actress{FirstName: "Given", LastName: "Family", JapaneseName: "日本名", Verified: true}
	movie := &models.Movie{Title: "Title", Actresses: []models.Actress{identity}}
	ctx := NewContextFromMovie(movie)
	ctx.Title = "Output title"
	ctx.VideoFilePath = "/video.mkv"
	ctx.PartNumber, ctx.PartSuffix, ctx.IsMultiPart = 2, "-B", true
	ctx.Index = 7
	ctx.ActressName = "Portrait name"
	execCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx.SetExecCtx(execCtx)
	original := ctx.Clone()

	opts := RenderOptions{
		ContextOptions: ContextOptions{FirstNameOrder: true, RenderCredits: true, UseCreditedName: true},
		GroupActress:   true, GroupActressMin: 4, GroupActressName: "Ensemble",
		GroupUnknownActressName: "Nobody", UnknownActressMode: models.UnknownActressModeFallback,
		ActressLanguageJa: true, ActressDelimiter: " + ",
	}
	opts.ApplyTo(ctx)
	require.Equal(t, []string{"Family Given"}, ctx.Actresses, "applying policy must not reconstruct names")
	require.Equal(t, original.ActressDetails, ctx.ActressDetails)
	require.Equal(t, execCtx, ctx.execCtx)
	actual, err := NewEngine().Execute("<ACTORS>|<TITLE>|<PARTSUFFIX>", ctx)
	require.NoError(t, err)
	require.Equal(t, "日本名|Output title|-B", actual)

	// Applying zero options clears prior policy without synthesizing defaults.
	(RenderOptions{}).ApplyTo(ctx)
	require.Equal(t, original, ctx.Clone())
	require.Equal(t, execCtx, ctx.execCtx)
}
