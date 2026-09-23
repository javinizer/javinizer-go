package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPR260ConfiguredCreditNameRealOutputs(t *testing.T) {
	cases := []struct {
		label      string
		credited   bool
		override   string
		forced     bool
		suppressed bool
		rename     bool
		expected   string
	}{
		{label: "canonical default", expected: "Canonical Identity"},
		{label: "configured credited", credited: true, expected: "Reported Identity"},
		{label: "explicit override", credited: true, override: "Pinned Identity", expected: "Pinned Identity"},
		{label: "forced canonical", credited: true, forced: true, expected: "Canonical Identity"},
		{label: "forced canonical explicit override", credited: true, forced: true, override: "Pinned Identity", expected: "Pinned Identity"},
		{label: "canonical rename after reload", rename: true, expected: "Renamed Identity"},
		{label: "suppressed and candidate excluded", credited: true, suppressed: true, expected: "Reported Identity"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			db, dsn := pr260ArtifactDB(t)
			actress := models.Actress{FirstName: "Canonical", LastName: "Identity", Verified: true, Origin: "user"}
			require.NoError(t, db.Create(&actress).Error)
			movie := models.Movie{ContentID: "pr260-parity", ID: "PR260-PARITY", Title: "Parity movie"}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{actress}))
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Reported Identity", Origin: "scrape", OverrideName: tc.override, UserOverride: tc.override != "", DisplayForceCanonical: tc.forced}
			require.NoError(t, db.Create(&credit).Error)
			if tc.suppressed {
				excluded := models.Actress{FirstName: "Candidate", LastName: "Identity", Verified: false, Origin: "scrape"}
				require.NoError(t, db.Create(&excluded).Error)
				require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: excluded.ID, CreditedName: "Candidate Identity", Origin: "scrape"}).Error)
				suppressed := models.Actress{FirstName: "Suppressed", LastName: "Identity", Verified: true, Origin: "user"}
				require.NoError(t, db.Create(&suppressed).Error)
				require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: suppressed.ID, CreditedName: "Suppressed Identity", Suppressed: true, Origin: "user"}).Error)
				require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{actress, excluded, suppressed}))
			}
			if tc.rename {
				require.NoError(t, db.Model(&actress).Update("first_name", "Renamed").Error)
			}
			reopened, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = reopened.Close() })
			snapshot, err := reopened.Repositories().MovieRepo.FindByID(context.Background(), movie.ID)
			require.NoError(t, err)
			require.NotEmpty(t, snapshot.Credits)
			fs := afero.NewMemMapFs()
			source := "/incoming/PR260-PARITY.mp4"
			require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
			require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
			poster := pr260PosterBytes(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(poster)
			}))
			defer server.Close()
			snapshot.Poster.PosterURL = server.URL + "/poster.jpg"
			app := config.DefaultConfig(nil, nil)
			app.Metadata.NFO.Feature.UseCreditedName = tc.credited
			app.Metadata.NFO.Format.FirstNameOrder = true
			app.Metadata.NFO.Format.FilenameTemplate = "<ID>.nfo"
			app.Output.Template.FolderFormat = "<ACTRESS>"
			app.Output.Template.SubfolderFormat = nil
			app.Output.Template.FileFormat = "<ID>"
			app.Output.Operation.RenameFile = true
			app.Output.MediaFormat.PosterFormat = "<ACTRESS>-poster.jpg"
			app.Output.Download.DownloadPoster = true
			nameCfg := nfo.NFONameConfigFromAppConfig(app)
			orgCfg := organizer.ConfigFromAppConfig(app, nameCfg)
			orgCfg.OperationMode = operationmode.OperationModeOrganize
			dlCfg := downloader.ConfigFromAppConfig(app, nameCfg)
			genCfg := nfo.ConfigFromAppConfig(app, nameCfg)
			engine := template.NewEngine()
			orch := newApplyOrchestrator(fs, organizer.NewOrganizer(fs, orgCfg, engine, nil), downloader.NewDownloader(server.Client(), fs, dlCfg, engine), nfo.NewGenerator(fs, genCfg), nil, ApplyConfig{NFONameCfg: nameCfg}, engine, noOpRevertLog{}, nil, nil)
			match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}
			cmd := ApplyCmd{Movie: snapshot, PublicationFence: reopened.Repositories().MovieRepo.(database.ApplyPublicationFencer), Match: match, DestPath: "/library", Organize: OrganizeOptions{MoveFiles: true}, Download: true, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
			result, err := orch.Execute(context.Background(), cmd)
			require.NoError(t, err)
			require.NotNil(t, result)
			dir := filepath.Join("/library", tc.expected)
			video, err := afero.ReadFile(fs, filepath.Join(dir, "PR260-PARITY.mp4"))
			require.NoError(t, err)
			require.Equal(t, "video", string(video))
			nfoBytes, err := afero.ReadFile(fs, filepath.Join(dir, "PR260-PARITY.nfo"))
			require.NoError(t, err)
			require.Contains(t, string(nfoBytes), "<name>"+tc.expected+"</name>")
			require.NotContains(t, string(nfoBytes), "<name>Candidate Identity</name>")
			require.NotContains(t, string(nfoBytes), "<name>Suppressed Identity</name>")
			_, err = afero.ReadFile(fs, filepath.Join(dir, tc.expected+"-poster.jpg"))
			require.NoError(t, err)
			stored, err := reopened.Repositories().MovieRepo.FindByID(context.Background(), movie.ID)
			require.NoError(t, err)
			canonical := "Canonical Identity"
			if tc.rename {
				canonical = "Renamed Identity"
			}
			require.Equal(t, canonical, models.FormatActressName(stored.Actresses[0], models.FormatActressNameOptions{FirstNameOrder: true}))
			other := &models.Movie{ID: "OTHER", Actresses: []models.Actress{stored.Actresses[0]}}
			otherContext := template.NewContextFromMovieWithOptions(other, template.ContextOptions{FirstNameOrder: true, RenderCredits: true, UseCreditedName: tc.credited})
			otherName, err := engine.Execute("<ACTRESS>", otherContext)
			require.NoError(t, err)
			require.Equal(t, canonical, otherName)
			if tc.suppressed {
				paths, err := afero.ReadDir(fs, dir)
				require.NoError(t, err)
				for _, entry := range paths {
					require.False(t, strings.Contains(entry.Name(), "Candidate") || strings.Contains(entry.Name(), "Suppressed"), entry.Name())
				}
			}
		})
	}
}
