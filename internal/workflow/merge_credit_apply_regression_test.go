package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestExistingNFOMergePublishesSelectedCreditAlias(t *testing.T) {
	for _, tc := range []struct {
		name       string
		force      bool
		suppressed bool
		wantErr    error
	}{
		{name: "merge"},
		{name: "suppressed cast", suppressed: true},
		{name: "force overwrite control", force: true},
		{name: "stale generation", wantErr: database.ErrApplyPublicationStale},
		{name: "open collision", wantErr: database.ErrApplyArtifactPublicationBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			identity := models.Actress{FirstName: "Canonical", LastName: "Person", Verified: true, Origin: "user"}
			require.NoError(t, db.Create(&identity).Error)
			movie := models.Movie{ID: "AUDIT-001", ContentID: "audit001", Title: "Synthetic", RenderDirty: true}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: identity.ID, CreditedName: "Reported Alias", Origin: "user", OverrideName: "User Selected Alias", UserOverride: true, Suppressed: tc.suppressed}).Error)
			if !tc.suppressed {
				require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{identity}))
			}
			snapshot, err := db.Repositories().MovieRepo.FindByID(t.Context(), movie.ID)
			require.NoError(t, err)
			if tc.suppressed {
				require.Empty(t, snapshot.Actresses, "durable suppressed cast must project to no actors")
			}
			if tc.wantErr == database.ErrApplyPublicationStale {
				require.NoError(t, db.Model(&movie).Update("render_generation", snapshot.RenderGeneration+1).Error)
			}
			if tc.wantErr == database.ErrApplyArtifactPublicationBlocked {
				require.NoError(t, db.Create(&models.CreditCollision{CreditID: snapshot.Credits[0].ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, Status: models.CollisionStatusOpen}).Error)
			}
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll("/incoming", 0755))
			require.NoError(t, afero.WriteFile(fs, "/incoming/AUDIT-001.mp4", []byte("synthetic video fixture"), 0644))
			existingNFO := []byte(`<movie><title>Existing</title><id>AUDIT-001</id></movie>`)
			if tc.suppressed {
				existingNFO = []byte(`<movie><title>Existing</title><id>AUDIT-001</id><actor><name>Canonical Person</name></actor></movie>`)
			}
			require.NoError(t, afero.WriteFile(fs, "/incoming/AUDIT-001.nfo", existingNFO, 0644))
			cfg := &nfo.Config{FilenameTemplate: "<ID>.nfo", FirstNameOrder: true}
			engine := template.NewEngine()
			log := NewDBRevertLog(database.NewBatchFileOperationRepository(db), NewRevertLogConfig(true, nil), "audit-merge", fs, engine, nil, nil)
			orch := newApplyOrchestrator(fs, nil, nil, nfo.NewGenerator(fs, cfg), nfo.NewNFOImplementor(fs, cfg, engine), ApplyConfig{DisplayTitle: "<ID> <ACTORS>", NFONameCfg: cfg.ToNFONameConfig(false, "", 0)}, engine, log, nil, nil)
			fencer := pr260FencedCounter(t, db)
			result, err := orch.Execute(t.Context(), ApplyCmd{Movie: snapshot, PublicationFence: fencer, Match: models.FileMatchInfo{Path: "/incoming/AUDIT-001.mp4", Name: "AUDIT-001.mp4", Extension: ".mp4", MovieID: movie.ID}, DestPath: "/incoming", Organize: OrganizeOptions{Skip: true}, Merge: MergeOptions{ForceOverwrite: tc.force, ScalarStrategy: nfo.PreferScraper}, GenerateNFO: true, OperationMode: operationmode.OperationModeInPlace})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Greater(t, fencer.calls, int32(0))
				require.Zero(t, fencer.callbacks)
				body, readErr := afero.ReadFile(fs, "/incoming/AUDIT-001.nfo")
				require.NoError(t, readErr)
				require.Equal(t, existingNFO, body, "rejected publication must leave the existing NFO intact")
				return
			}
			require.NoError(t, err)
			require.Greater(t, fencer.callbacks, int32(0))
			require.NotEmpty(t, result.NFOPath)
			body, err := afero.ReadFile(fs, result.NFOPath)
			require.NoError(t, err)
			stored, err := db.Repositories().MovieRepo.FindByID(t.Context(), movie.ID)
			require.NoError(t, err)
			require.Equal(t, "User Selected Alias", stored.Credits[0].OverrideName)
			t.Logf("published=%s credits_after_merge=%d db_override=%q nfo=%s", result.NFOPath, len(result.Movie.Credits), stored.Credits[0].OverrideName, string(body))
			if tc.suppressed {
				require.True(t, stored.Credits[0].Suppressed)
				require.NotContains(t, string(body), "<actor>")
				require.NotContains(t, string(body), "Canonical Person")
				require.NotContains(t, string(body), "User Selected Alias")
				return
			}
			require.Contains(t, string(body), "<name>User Selected Alias</name>")
			require.Contains(t, string(body), "<title>AUDIT-001 User Selected Alias</title>")
			require.NotContains(t, string(body), "Canonical Person")
		})
	}
}
