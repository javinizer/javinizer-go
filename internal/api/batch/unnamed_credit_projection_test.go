package batch

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestUnnamedCreditProjectionThroughRealCollision(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprint(force), func(t *testing.T) {
			cfg := newDisplayTitlePreviewConfig("<ACTORS>")
			cfg.Metadata.NFO.Feature.UseCreditedName = true
			f := newSuppressionFixtureWithConfig(t, 1, cfg)
			require.NoError(t, f.deps.Repos.DB.Model(&models.Actress{}).Where("id = ?", f.actress.ID).Updates(map[string]any{"first_name": "", "last_name": "", "japanese_name": "", "dmm_id": 741852}).Error)
			require.NoError(t, f.deps.Repos.DB.Model(&models.MovieCredit{}).Where("id = ?", f.credit.ID).Updates(map[string]any{"user_override": false, "override_name": "", "credited_name": "Reported Alias"}).Error)
			incoming := f.stored(t)
			incoming.Credits = []models.MovieCredit{{ActressID: f.actress.ID, CreditedName: "Reported Alias", Origin: "scrape", Source: "synthetic", Scraped: models.Actress{DMMID: 741852, FirstName: "Reported", LastName: "Alias"}}}
			_, err := f.deps.Repos.MovieRepo.Upsert(t.Context(), incoming)
			require.NoError(t, err)
			var collisions []models.CreditCollision
			require.NoError(t, f.deps.Repos.DB.Where("credit_id = ? AND status = ?", f.credit.ID, models.CollisionStatusOpen).Find(&collisions).Error)
			require.Len(t, collisions, 1)
			require.Empty(t, collisions[0].CanonicalValue)
			require.Equal(t, "Reported Alias", collisions[0].ReportedValue)
			if force {
				res := doJSON(t, f.router, http.MethodPost, fmt.Sprintf("/actresses/collisions/%d/resolve", collisions[0].ID), map[string]any{"resolution": "keep_identity"})
				require.Equal(t, 200, res.Code, res.Body.String())
			}

			stored := f.stored(t)
			require.Equal(t, force, stored.Credits[0].DisplayForceCanonical)
			require.Empty(t, stored.Credits[0].Actress.FirstName)
			rt := testkit.GetTestRuntime(f.deps)
			f.router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
			fresh := f.read(t)
			view := fresh.Results[f.paths[0]].Movie
			before := previewTableSnapshot(t, f.deps.Repos.DB)
			res := doJSON(t, f.router, http.MethodPost, "/batch/"+f.job.GetID()+"/results/suppression-result-0/display-title-preview", map[string]any{"movie": view})
			require.Equal(t, 200, res.Code, res.Body.String())
			require.Equal(t, before, previewTableSnapshot(t, f.deps.Repos.DB))
			var preview contracts.DisplayTitlePreviewResponse
			require.NoError(t, json.Unmarshal(res.Body.Bytes(), &preview))
			fs := afero.NewMemMapFs()
			g := nfo.NewGenerator(fs, &nfo.Config{Tagline: "<ACTORS>", UseCreditedName: true, UnknownActressMode: models.UnknownActressModeSkip})
			require.NoError(t, g.GenerateAtPath(t.Context(), stored, "/fixture.nfo", "", nil))
			data, err := afero.ReadFile(fs, "/fixture.nfo")
			require.NoError(t, err)
			var output struct {
				Actors []struct {
					Name string `xml:"name"`
				} `xml:"actor"`
				Tagline string `xml:"tagline"`
			}
			require.NoError(t, xml.Unmarshal(data, &output))
			require.Len(t, output.Actors, 1)
			t.Logf("force=%v actor=%q tagline=%q preview=%q", force, output.Actors[0].Name, output.Tagline, preview.DisplayTitle)
			require.Equal(t, "Reported Alias", output.Actors[0].Name)
			require.Equal(t, output.Actors[0].Name, preview.DisplayTitle)
			require.Equal(t, output.Actors[0].Name, output.Tagline)
		})
	}
}
