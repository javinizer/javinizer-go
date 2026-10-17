package worker

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRescrapeCorrectedIDKeepsMergedCreditPresentation(t *testing.T) {
	for _, correctedID := range []bool{false, true} {
		for _, mergeArrays := range []bool{false, true} {
			t.Run(fmt.Sprintf("corrected=%v/merge=%v", correctedID, mergeArrays), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "image/jpeg")
					_, _ = w.Write([]byte(r.URL.Path))
				}))
				defer server.Close()
				id := "OLD-001"
				if correctedID {
					id = "NEW-001"
				}
				identity := models.Actress{ID: 1, FirstName: "Source", LastName: "Person", JapaneseName: "正名", ThumbURL: server.URL + "/source", Verified: true}
				hidden := models.Actress{ID: 2, FirstName: "Hidden", Verified: true}
				scraped := &models.Movie{ID: id, ContentID: "same-content", Title: "Scraped", RenderGeneration: 7, Actresses: []models.Actress{identity}, Credits: []models.MovieCredit{
					{ID: 1, MovieContentID: "same-content", ActressID: 1, Actress: &identity},
					{ID: 2, MovieContentID: "same-content", ActressID: 2, Actress: &hidden, Suppressed: true},
				}}
				existing := &models.Movie{ID: "OLD-001", ContentID: "same-content", Title: "Existing", Actresses: []models.Actress{
					{FirstName: "Imported", LastName: "Display", JapaneseName: "正名", ThumbURL: server.URL + "/imported"},
					{FirstName: "Extra", LastName: "Person", ThumbURL: server.URL + "/extra"},
					{FirstName: "Hidden"},
				}}
				store := resultstore.New(1, []string{"file.mp4"})
				store.UpdateFileResult("file.mp4", &resultstore.MovieResult{Movie: existing, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "file.mp4", MovieID: existing.ID}})
				wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: scraped, Status: scrape.StatusCompleted}}
				inputs := rescrapePhaseInputs{WF: wf, ResultMap: store, Finder: store, JobID: models.NewJobID()}
				outcome, err := NewRescrapePhase().Rescrape(t.Context(), inputs, RescrapeCmd{MovieID: existing.ID, FilePath: "file.mp4", MergeEnabled: true, Merge: workflow.MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: mergeArrays}})
				require.NoError(t, err)
				require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
				movie := outcome.Movie
				require.NotNil(t, movie)
				require.Equal(t, id, movie.ID)
				require.Equal(t, "same-content", movie.ContentID)
				require.Equal(t, scraped.Credits, movie.Credits)
				wantName := "Imported Display"
				if mergeArrays {
					wantName = "Source Person"
				}
				engine := template.NewEngine()
				ctx := template.NewContextFromMovieWithOptions(movie, template.ContextOptions{RenderCredits: true, FirstNameOrder: true})
				display, err := engine.Execute("<ID> <ACTORS>", ctx)
				require.NoError(t, err)
				assert.Equal(t, id+" "+wantName+", Extra Person", display)
				fs := afero.NewMemMapFs()
				generator := nfo.NewGenerator(fs, &nfo.Config{FirstNameOrder: true, IncludeActressImages: true, TemplateEngine: engine, Tagline: "<ID> <ACTORS>"})
				require.NoError(t, generator.GenerateAtPath(t.Context(), movie, "/output/movie.nfo", "", nil))
				body, err := afero.ReadFile(fs, "/output/movie.nfo")
				require.NoError(t, err)
				var rendered nfo.Movie
				require.NoError(t, xml.Unmarshal(body, &rendered))
				var names []string
				for _, actor := range rendered.Actors {
					names = append(names, actor.Name)
				}
				assert.Equal(t, []string{wantName, "Extra Person"}, names)
				assert.Equal(t, display, rendered.Tagline)
				dl := downloader.NewDownloader(server.Client(), fs, &downloader.Config{DownloadActress: true, ActorFirstNameOrder: true, MediaFormatConfig: organizer.MediaFormatConfig{ActressFolder: ".actors", ActressFormat: "<ACTRESS>_<ACTORS>.jpg"}}, engine)
				images, err := dl.Download(t.Context(), downloader.DownloadCmd{Movie: movie, DestDir: "/output"})
				require.NoError(t, err)
				assert.ElementsMatch(t, []string{filepath.Join("/output/.actors", wantName+"_"+wantName+".jpg"), filepath.Join("/output/.actors", "Extra Person_Extra Person.jpg")}, images.DownloadedPaths)
				for _, result := range images.Results {
					assert.NoError(t, result.Error)
				}
				for imagePath, wantBody := range map[string]string{
					filepath.Join("/output/.actors", wantName+"_"+wantName+".jpg"):    "/source",
					filepath.Join("/output/.actors", "Extra Person_Extra Person.jpg"): "/extra",
				} {
					data, readErr := afero.ReadFile(fs, imagePath)
					assert.NoError(t, readErr)
					assert.Equal(t, wantBody, string(data))
				}
				stored, err := store.GetMovieResult("file.mp4")
				require.NoError(t, err)
				renderCredits, present := stored.Movie.RenderingCredits()
				assert.True(t, present)
				if assert.Len(t, renderCredits, 2) {
					assert.Zero(t, renderCredits[1].ID)
					assert.Zero(t, renderCredits[1].ActressID)
					assert.False(t, renderCredits[1].Actress.Verified)
				}
				assert.Equal(t, scraped.Credits, stored.Movie.Credits)
				assert.Equal(t, "OLD-001", existing.ID)
				for _, mutation := range []string{"id", "content", "generation", "suppression", "identity"} {
					t.Run(mutation+" invalidates", func(t *testing.T) {
						changed := movie.Clone()
						switch mutation {
						case "id":
							changed.ID = "OTHER-001"
						case "content":
							changed.ContentID = "other-content"
						case "generation":
							changed.RenderGeneration++
						case "suppression":
							changed.Credits[0].Suppressed = true
						case "identity":
							changed.Credits[0].Actress.FirstName = "Changed"
						}
						got, _ := changed.RenderingCredits()
						require.Equal(t, changed.Credits, got, "ordinary mutations must still discard the old projection")
					})
				}
			})
		}
	}
}

func TestMergeRescrapeCorrectedIDKeepsExplicitEmptyCreditPresentation(t *testing.T) {
	identity := models.Actress{ID: 1, FirstName: "Hidden", Verified: true}
	scraped := &models.Movie{ID: "NEW-001", ContentID: "same-content", Credits: []models.MovieCredit{{ID: 1, MovieContentID: "same-content", ActressID: 1, Actress: &identity, Suppressed: true}}}
	existing := &models.Movie{ID: "OLD-001", Actresses: []models.Actress{identity}}
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true}, "file.mp4")
	credits, present := merged.RenderingCredits()
	require.True(t, present)
	require.Empty(t, credits, "an explicit empty projection must survive the deliberate display-ID correction")
	require.Equal(t, scraped.Credits, merged.Credits)
}
