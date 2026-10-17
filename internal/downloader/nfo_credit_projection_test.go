package downloader

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestNFORenderOnlyActorBeyondDurableCreditCountUsesOwnFilenameContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("synthetic")) }))
	defer server.Close()
	a := models.Actress{ID: 1, FirstName: "Source", LastName: "Person", ThumbURL: server.URL + "/source", Verified: true}
	source := &models.Movie{ID: "CAST-001", ContentID: "cast001", Title: "Title", Actresses: []models.Actress{a}, Credits: []models.MovieCredit{{ID: 1, ActressID: 1, MovieContentID: "cast001", Actress: &a}}}
	existing := &models.Movie{Actresses: []models.Actress{{FirstName: "Extra", LastName: "Person", ThumbURL: server.URL + "/extra"}}}
	merged, err := nfo.MergeMovieMetadataWithOptions(source, existing, nfo.PreferNFO, true)
	require.NoError(t, err)
	fs := afero.NewMemMapFs()
	cfg := &Config{DownloadActress: true, ActorFirstNameOrder: true, MediaFormatConfig: organizer.MediaFormatConfig{ActressFolder: ".actors", ActressFormat: "<ACTRESS>_<ACTORS>.jpg"}}
	downloader := NewDownloader(server.Client(), fs, cfg, template.NewEngine())
	images, err := downloader.downloadActressImages(t.Context(), merged.Merged, "/output")
	require.NoError(t, err)
	require.Len(t, images, 2)
	require.Equal(t, "Source Person_Source Person.jpg", filepath.Base(images[0].LocalPath))
	require.Equal(t, "Extra Person_Extra Person.jpg", filepath.Base(images[1].LocalPath))
	require.Len(t, merged.Merged.Credits, 1)
	require.Equal(t, source.Credits, merged.Merged.Credits)
}
