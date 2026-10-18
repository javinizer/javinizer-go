package batch

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/stretchr/testify/require"
)

// An accepted operation receipt must be immediately reusable, without a GET
// repairing its cast. Exercise the real wire DTO and every multipart projection.
func TestDuplicateCastAcceptedReceipt(t *testing.T) {
	for _, mode := range []string{"identical", "conflicting names", "explicit then implicit thumb", "last explicit thumb", "zero ID resolves existing", "new zero ID"} {
		t.Run(mode, func(t *testing.T) {
			f := newSuppressionFixtureWithConfig(t, 2, newDisplayTitlePreviewConfig("<ACTORS>"))
			rt := testkit.GetTestRuntime(f.deps)
			f.router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
			f.router.POST("/batch/:id/results/:resultId/preview", previewOrganize(rt))
			initial := f.read(t)
			view := initial.Results[f.paths[0]].Movie
			first := view.Actresses[0]
			second := first
			wantName, wantThumb, wantCount := first.FirstName, first.ThumbURL, 1
			switch mode {
			case "conflicting names":
				first.FirstName = "Earlier"
				second.FirstName = "Winner"
				wantName = "Winner"
			case "explicit then implicit thumb":
				first.ThumbURL = "accepted-thumb"
				first.ThumbEdited = true
				second.ThumbURL = "ignored-thumb"
				wantThumb = "accepted-thumb"
			case "last explicit thumb":
				first.ThumbURL = "earlier-thumb"
				first.ThumbEdited = true
				second.ThumbURL = "winner-thumb"
				second.ThumbEdited = true
				wantThumb = "winner-thumb"
			case "zero ID resolves existing":
				second.ID = 0
			case "new zero ID":
				second.ID = 0
				second.FirstName = "New"
				second.LastName = "Person"
				wantCount = 2
			}
			view.Actresses = append(view.Actresses[:0:0], first, second)
			revisions := map[string]uint64{}
			for _, r := range initial.Results {
				revisions[r.ResultID] = r.Revision
			}
			request := contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevisions: revisions}
			path := "/batch/" + f.job.GetID() + "/results/suppression-result-0"
			before := previewTableSnapshot(t, f.deps.Repos.DB)
			preview := doJSON(t, f.router, http.MethodPost, path+"/display-title-preview", request)
			require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
			require.Equal(t, before, previewTableSnapshot(t, f.deps.Repos.DB))
			var title contracts.DisplayTitlePreviewResponse
			require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &title))
			saved := f.patch(t, request, http.StatusOK)
			require.Len(t, saved.Movie.Actresses, wantCount)
			require.Equal(t, title.DisplayTitle, saved.Movie.DisplayTitle)
			stored := f.stored(t)
			require.Equal(t, models.ActressCastVersion(stored.Actresses), saved.Movie.CastVersion)
			require.Equal(t, wantName, stored.Actresses[0].FirstName)
			require.Equal(t, wantThumb, stored.Actresses[0].ThumbURL)
			require.Len(t, stored.Credits, wantCount)
			var original *models.MovieCredit
			for i := range stored.Credits {
				if stored.Credits[i].ActressID == f.actress.ID {
					original = &stored.Credits[i]
				}
			}
			require.NotNil(t, original)
			require.True(t, original.UserOverride)
			require.Equal(t, "Saved override", original.OverrideName)
			require.True(t, original.OrderPinned)
			require.Equal(t, 3, original.OrderIndex)
			row, err := f.deps.Repos.JobRepo.FindByID(t.Context(), f.job.GetID())
			require.NoError(t, err)
			envelope, errs := jobpersist.Decode(row)
			require.Empty(t, errs)
			fresh := f.read(t)
			for _, file := range f.paths {
				live, err := f.job.Results().GetMovieResult(file)
				require.NoError(t, err)
				require.Equal(t, saved.Revisions[live.ResultID], live.Revision)
				require.Equal(t, saved.Revisions[live.ResultID], envelope.Results[file].Revision)
				for _, cast := range [][]models.Actress{live.Movie.Actresses, envelope.Results[file].Movie.Actresses} {
					require.Len(t, cast, wantCount)
					require.Equal(t, saved.Movie.CastVersion, models.ActressCastVersion(cast))
				}
				require.ElementsMatch(t, saved.Movie.Actresses, fresh.Results[file].Movie.Actresses)
				require.Equal(t, saved.Movie.CastVersion, fresh.Results[file].Movie.CastVersion)
			}
			saved.Movie.Actresses[0].FirstName = "Next accepted rename"
			next := f.patch(t, contracts.UpdateMovieRequest{Movie: saved.Movie, ExpectedResultRevisions: saved.Revisions}, http.StatusOK)
			require.Equal(t, "Next accepted rename", next.Movie.Actresses[0].FirstName)
			// The old revision still fails: normalization must not relax admission.
			f.patch(t, contracts.UpdateMovieRequest{Movie: saved.Movie, ExpectedResultRevisions: saved.Revisions}, http.StatusConflict)
		})
	}
}

func TestDuplicateCastEnvelopeFailureRollsBack(t *testing.T) {
	f := newSuppressionFixture(t, 2)
	initial := f.read(t)
	view := initial.Results[f.paths[0]].Movie
	view.Actresses = append(view.Actresses, view.Actresses[0])
	view.Actresses[1].FirstName = "Must roll back"
	before := previewTableSnapshot(t, f.deps.Repos.DB)
	// Fail the envelope after the upsert has normalized the detached candidate.
	require.NoError(t, f.deps.Repos.DB.Exec("CREATE TRIGGER reject_duplicate_receipt BEFORE UPDATE ON jobs BEGIN SELECT RAISE(ABORT, 'injected envelope failure'); END").Error)
	f.patch(t, contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevisions: map[string]uint64{"suppression-result-0": 1, "suppression-result-1": 1}}, http.StatusInternalServerError)
	require.Equal(t, before, previewTableSnapshot(t, f.deps.Repos.DB))
	for _, file := range f.paths {
		live, err := f.job.Results().GetMovieResult(file)
		require.NoError(t, err)
		require.Equal(t, uint64(1), live.Revision)
		require.Len(t, live.Movie.Actresses, 1)
		require.Equal(t, f.actress.FirstName, live.Movie.Actresses[0].FirstName)
	}
}

func TestDuplicateCastInvalidRenameAtomic(t *testing.T) {
	f := newSuppressionFixture(t, 2)
	rt := testkit.GetTestRuntime(f.deps)
	f.router.POST("/batch/:id/results/:resultId/display-title-preview", previewDisplayTitle(rt))
	f.router.POST("/batch/:id/results/:resultId/preview", previewOrganize(rt))
	initial := f.read(t)
	view := initial.Results[f.paths[0]].Movie
	view.Actresses = append(view.Actresses, view.Actresses[0])
	view.Actresses[0].FirstName = "Temporary accepted name"
	view.Actresses[1].FirstName = ""
	view.Actresses[1].LastName = ""
	view.Actresses[1].JapaneseName = ""
	request := map[string]any{"movie": view, "expected_result_revisions": map[string]uint64{"suppression-result-0": 1, "suppression-result-1": 1}, "operation_mode": "in-place"}
	before := previewTableSnapshot(t, f.deps.Repos.DB)
	path := "/batch/" + f.job.GetID() + "/results/suppression-result-0"
	for _, suffix := range []string{"/display-title-preview", "/preview", ""} {
		method := http.MethodPost
		if suffix == "" {
			method = http.MethodPatch
		}
		response := doJSON(t, f.router, method, path+suffix, request)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Equal(t, before, previewTableSnapshot(t, f.deps.Repos.DB))
		for _, file := range f.paths {
			live, err := f.job.Results().GetMovieResult(file)
			require.NoError(t, err)
			require.Equal(t, uint64(1), live.Revision)
			require.Len(t, live.Movie.Actresses, 1)
			require.Equal(t, f.actress.FirstName, live.Movie.Actresses[0].FirstName)
		}
	}
}
