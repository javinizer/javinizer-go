package workflow

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestNFOMergedCreditProjectionPublishesConsistentActorsAndImages(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		for _, selection := range []string{"override", "credited", "unedited"} {
			useCredited := selection == "credited"
			edited := selection != "unedited"
			t.Run(fmt.Sprintf("overwrite=%v/selection=%s", overwrite, selection), func(t *testing.T) {
				var mu sync.Mutex
				var requests []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					requests = append(requests, r.URL.Path)
					mu.Unlock()
					w.Header().Set("Content-Type", "image/jpeg")
					_, _ = w.Write([]byte("synthetic image payload"))
				}))
				defer server.Close()
				db, _ := pr260ArtifactDB(t)
				identity := models.Actress{FirstName: "Canonical", LastName: "Person", JapaneseName: "正名", ThumbURL: server.URL + "/fresh.jpg", Verified: true, Origin: "user"}
				hidden := models.Actress{FirstName: "Hidden", LastName: "Person", Verified: true, Origin: "user"}
				require.NoError(t, db.Create(&identity).Error)
				if edited {
					require.NoError(t, db.Create(&hidden).Error)
				}
				extra := models.Actress{FirstName: "New", LastName: "Face", Verified: true, Origin: "user"}
				if edited {
					require.NoError(t, db.Create(&extra).Error)
				}
				movie := models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Synthetic", RenderGeneration: 7, RenderDirty: true}
				require.NoError(t, db.Create(&movie).Error)
				wantName := "Selected Alias"
				credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: identity.ID, CreditedName: "Reported Alias", OverrideName: wantName, UserOverride: true, Origin: "user", OrderPinned: true}
				if useCredited {
					credit.UserOverride, credit.OverrideName = false, ""
					wantName = "Reported Alias"
				}
				if !edited {
					credit.UserOverride = false
					credit.OverrideName = ""
					credit.Origin = ""
					credit.OrderPinned = false
					wantName = "Canonical Person"
				}
				require.NoError(t, db.Create(&credit).Error)
				cast := []models.Actress{identity}
				if edited {
					require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: hidden.ID, CreditedName: "Hidden Person", Suppressed: true, Origin: "user", OrderIndex: 1}).Error)
					require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: extra.ID, CreditedName: "New Face", OrderIndex: 2}).Error)
					cast = append(cast, extra)
				}
				require.NoError(t, db.Model(&movie).Association("Actresses").Replace(cast))
				snapshot, err := db.Repositories().MovieRepo.FindByID(t.Context(), movie.ID)
				require.NoError(t, err)
				fs := afero.NewMemMapFs()
				require.NoError(t, fs.MkdirAll("/incoming", 0755))
				require.NoError(t, afero.WriteFile(fs, "/incoming/CAST-001.mp4", []byte("synthetic video fixture"), 0644))
				existingNFO := fmt.Sprintf(`<movie><title>Existing</title><id>CAST-001</id><actor><name>Canonical Person</name><thumb>%s/stale.jpg</thumb></actor><actor><name>Hidden Person</name></actor><actor><name>New Face</name><thumb>%s/new.jpg</thumb></actor></movie>`, server.URL, server.URL)
				if !edited {
					existingNFO = strings.ReplaceAll(existingNFO, `<actor><name>Hidden Person</name></actor>`, "")
				}
				require.NoError(t, afero.WriteFile(fs, "/incoming/CAST-001.nfo", []byte(existingNFO), 0644))
				app := config.DefaultConfig(nil, nil)
				app.Metadata.NFO.Feature.UseCreditedName = useCredited
				app.Metadata.NFO.Format.FirstNameOrder = true
				app.Metadata.NFO.Format.FilenameTemplate = "<ID>.nfo"
				app.Metadata.NFO.Feature.IncludeActressImages = true
				app.Output.Download.DownloadActress = true
				app.Output.Download.DownloadPoster = false
				app.Output.Download.DownloadCover = false
				app.Output.MediaFormat.ActressFolder = ".actors"
				app.Output.MediaFormat.ActressFormat = "<ACTRESS>.jpg"
				nameCfg := nfo.NFONameConfigFromAppConfig(app)
				genCfg := nfo.ConfigFromAppConfig(app, nameCfg)
				engine := template.NewEngine()
				log := NewDBRevertLog(database.NewBatchFileOperationRepository(db), NewRevertLogConfig(true, nil), "credit-cast", fs, engine, nil, nil)
				orch := newApplyOrchestrator(fs, nil, downloader.NewDownloader(server.Client(), fs, downloader.ConfigFromAppConfig(app, nameCfg), engine), nfo.NewGenerator(fs, genCfg), nfo.NewNFOImplementor(fs, genCfg, engine), ApplyConfig{DisplayTitle: "<ID> <ACTORS>", NFONameCfg: nameCfg}, engine, log, nil, nil)
				result, err := orch.Execute(t.Context(), ApplyCmd{Movie: snapshot, PersistedMovie: true, PublicationFence: db.Repositories().MovieRepo.(database.ApplyPublicationFencer), Match: models.FileMatchInfo{Path: "/incoming/CAST-001.mp4", Name: "CAST-001.mp4", Extension: ".mp4", MovieID: movie.ID}, DestPath: "/incoming", Organize: OrganizeOptions{Skip: true}, Merge: MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true}, GenerateNFO: true, Download: true, OverwriteExistingMedia: overwrite, OperationMode: operationmode.OperationModeInPlace})
				require.NoError(t, err)
				body, err := afero.ReadFile(fs, result.NFOPath)
				require.NoError(t, err)
				var rendered nfo.Movie
				require.NoError(t, xml.Unmarshal(body, &rendered))
				require.Len(t, rendered.Actors, 2)
				require.Equal(t, wantName, rendered.Actors[0].Name)
				require.Equal(t, "New Face", rendered.Actors[1].Name)
				require.Contains(t, rendered.Title, wantName)
				require.Contains(t, rendered.Title, "New Face")
				require.NotContains(t, string(body), "Hidden Person")
				image, err := afero.ReadFile(fs, filepath.Join("/incoming/.actors", wantName+".jpg"))
				require.NoError(t, err)
				require.Equal(t, "synthetic image payload", string(image))
				newExists, err := afero.Exists(fs, "/incoming/.actors/New Face.jpg")
				require.NoError(t, err)
				require.Equal(t, !overwrite, newExists)
				mu.Lock()
				gotRequests := append([]string(nil), requests...)
				mu.Unlock()
				if overwrite {
					require.ElementsMatch(t, []string{"/fresh.jpg"}, gotRequests)
				} else {
					require.ElementsMatch(t, []string{"/fresh.jpg", "/new.jpg"}, gotRequests)
				}
				stored, err := db.Repositories().MovieRepo.FindByID(t.Context(), movie.ID)
				require.NoError(t, err)
				wantCount := 1
				if edited {
					wantCount = 3
				}
				require.Len(t, stored.Credits, wantCount)
				require.Equal(t, snapshot.Credits, stored.Credits)
				var count int64
				require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
				require.EqualValues(t, wantCount, count)
			})
		}
	}
}
