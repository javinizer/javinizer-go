package nfo

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/template"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPersistedEmptyActorText(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	unnamed := models.Actress{DMMID: 90001, Verified: true, Origin: database.ActressOriginUser}
	named := models.Actress{DMMID: 90002, FirstName: "Given", LastName: "Family", JapaneseName: "日本名", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Repositories().ActressRepo.Create(t.Context(), &unnamed))
	require.NoError(t, db.Repositories().ActressRepo.Create(t.Context(), &named))
	saved, err := db.Repositories().MovieRepo.Upsert(t.Context(), &models.Movie{ID: "SYNTHETIC", ContentID: "synthetic", Title: "Synthetic", Actresses: []models.Actress{unnamed, named}})
	require.NoError(t, err)
	require.Len(t, saved.Credits, 2)
	movie, err := db.Repositories().MovieRepo.FindByContentID(t.Context(), "synthetic")
	require.NoError(t, err)
	require.Len(t, movie.Credits, 2)
	t.Logf("durable credits: count=%d unnamed=%q named=%q", len(movie.Credits), movie.Credits[0].CreditedName, movie.Credits[1].CreditedName)
	before := movie.Clone()
	for _, tc := range []struct {
		name                string
		group, ja, credited bool
		delim               string
	}{
		{"plain", false, false, false, ""}, {"explicit delimiter", false, false, false, " / "}, {"Japanese", false, true, false, " / "}, {"credited", false, false, true, " / "}, {"group", true, false, false, " / "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Tagline: "<ACTORS>", FirstNameOrder: true, UnknownActressMode: models.UnknownActressModeSkip, GroupActress: tc.group, ActressLanguageJA: tc.ja, UseCreditedName: tc.credited, ActressDelimiter: tc.delim}
			out, err := NewGenerator(nil, cfg).movieToNFO(t.Context(), movie, "", "", 0, false, nil)
			require.NoError(t, err)
			require.Len(t, out.Actors, 1)
			t.Logf("actor=%q tagline=%q", out.Actors[0].Name, out.Tagline)
			require.Equal(t, before, movie)
			if tc.group {
				require.Equal(t, "@Group", out.Tagline)
			} else {
				require.Equal(t, out.Actors[0].Name, out.Tagline, "empty identity must not produce an empty list item")
			}
		})
	}
	for _, control := range []struct {
		name                 string
		mode                 models.UnknownActressMode
		override, suppressed bool
		want                 string
		actors               int
	}{
		{"skip", models.UnknownActressModeSkip, false, false, "Given Family", 1},
		{"fallback", models.UnknownActressModeFallback, false, false, "Given Family", 2},
		{"suppressed", models.UnknownActressModeSkip, false, true, "Given Family", 1},
		{"override", models.UnknownActressModeSkip, true, false, "Chosen / Given Family", 2},
	} {
		t.Run("control_"+control.name, func(t *testing.T) {
			m := movie.Clone()
			m.Credits[0].Suppressed = control.suppressed
			if control.override {
				m.Credits[0].UserOverride = true
				m.Credits[0].OverrideName = "Chosen"
			}
			original := m.Clone()
			cfg := &Config{Tagline: "<ACTORS>", FirstNameOrder: true, UnknownActressMode: control.mode, ActressDelimiter: " / "}
			g := NewGenerator(nil, cfg)
			out, err := g.movieToNFO(t.Context(), m, "", "", 0, false, nil)
			require.NoError(t, err)
			require.Equal(t, control.want, out.Tagline)
			require.Len(t, out.Actors, control.actors)
			if control.mode == models.UnknownActressModeFallback {
				require.Equal(t, "Unknown", out.Actors[0].Name)
			}
			ctx := g.movieTemplateContext(t.Context(), m, "", "", 0, false)
			title, err := template.NewEngine().Execute("<TITLE> - <ACTORS>", ctx)
			require.NoError(t, err)
			require.Equal(t, "Synthetic - "+control.want, title)
			t.Logf("tagline=%q title-template=%q actors=%v", out.Tagline, title, out.Actors)
			require.Equal(t, original, m)
		})
	}

}
