package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPR260ActressImageFilenameMatchesRenderedNFOCredit(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("actress image payload"))
	}))
	defer server.Close()

	canonical := models.Actress{ID: 7, FirstName: "Momo", LastName: "Sakura", JapaneseName: "さくらもも", ThumbURL: server.URL + "/canonical.jpg", Verified: true, Origin: "user"}
	tests := []struct {
		name           string
		credited       bool
		firstNameOrder bool
		japanese       bool
		credit         models.MovieCredit
		wantName       string
	}{
		{name: "credited name", credited: true, firstNameOrder: true, credit: models.MovieCredit{CreditedName: "Stage Alias"}, wantName: "Stage Alias"},
		{name: "canonical first name order", firstNameOrder: true, credit: models.MovieCredit{CreditedName: "Stage Alias"}, wantName: "Momo Sakura"},
		{name: "canonical last name order", credit: models.MovieCredit{CreditedName: "Stage Alias"}, wantName: "Sakura Momo"},
		{name: "override wins", credited: false, firstNameOrder: true, credit: models.MovieCredit{CreditedName: "Stage Alias", UserOverride: true, OverrideName: "Director Pick"}, wantName: "Director Pick"},
		{name: "force canonical", credited: true, firstNameOrder: true, credit: models.MovieCredit{CreditedName: "Stage Alias", DisplayForceCanonical: true}, wantName: "Momo Sakura"},
		{name: "Japanese canonical", credited: false, firstNameOrder: true, japanese: true, credit: models.MovieCredit{CreditedName: "Stage Alias"}, wantName: "さくらもも"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			requests = nil
			mu.Unlock()
			credit := tt.credit
			credit.ActressID = canonical.ID
			credit.Actress = &canonical
			credit.ReportedThumbURL = server.URL + "/reported-must-not-be-used.jpg"
			movie := &models.Movie{ContentID: "image-parity", ID: "IMAGE-PARITY", Title: "Parity", Actresses: []models.Actress{canonical}, Credits: []models.MovieCredit{credit}}
			dir := t.TempDir()
			cfg := &Config{
				ActorJapaneseNames: tt.japanese, ActorFirstNameOrder: tt.firstNameOrder, UseCreditedName: tt.credited,
				UserAgent: "test", DownloadActress: true,
				MediaFormatConfig: organizer.MediaFormatConfig{ActressFolder: ".actors", ActressFormat: "<ACTORNAME>.jpg"},
			}
			d := NewDownloader(http.DefaultClient, afero.NewOsFs(), cfg, nil)
			results, err := d.downloadActressImages(context.Background(), movie, dir)
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.True(t, results[0].Downloaded)
			require.Equal(t, tt.wantName+".jpg", filepath.Base(results[0].LocalPath))
			require.FileExists(t, results[0].LocalPath)
			mu.Lock()
			require.Equal(t, []string{"/canonical.jpg"}, append([]string(nil), requests...))
			mu.Unlock()

			gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{FilenameTemplate: "movie.nfo", FirstNameOrder: tt.firstNameOrder, ActressLanguageJA: tt.japanese, UseCreditedName: tt.credited})
			require.NoError(t, gen.Generate(context.Background(), movie, dir, "", "", nil))
			xml, err := os.ReadFile(filepath.Join(dir, "movie.nfo"))
			require.NoError(t, err)
			require.Contains(t, string(xml), fmt.Sprintf("<name>%s</name>", tt.wantName))
		})
	}
}

func TestPR260ActressImageCreditEligibilityAndLegacyFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("image")) }))
	defer server.Close()
	verified := models.Actress{ID: 1, FirstName: "Visible", LastName: "Person", ThumbURL: server.URL + "/visible.jpg", Verified: true}
	candidate := models.Actress{ID: 2, FirstName: "Hidden", LastName: "Candidate", ThumbURL: server.URL + "/candidate.jpg", Verified: false}

	tests := []struct {
		name      string
		movie     *models.Movie
		wantFiles []string
		wantNFO   []string
		denyNFO   []string
	}{
		{name: "suppressed and unverified excluded", movie: &models.Movie{ID: "EXCLUDED", Actresses: []models.Actress{verified, candidate}, Credits: []models.MovieCredit{{ActressID: verified.ID, Actress: &verified, Suppressed: true}, {ActressID: candidate.ID, Actress: &candidate}}}, denyNFO: []string{"Visible Person", "Hidden Candidate"}},
		{name: "present unresolved credits do not fall back", movie: &models.Movie{ID: "UNRESOLVED", Actresses: []models.Actress{verified}, Credits: []models.MovieCredit{{ActressID: candidate.ID, Actress: &candidate, CreditedName: "Reported Candidate"}}}, denyNFO: []string{"Visible Person", "Reported Candidate"}},
		{name: "blank verified credit is skipped", movie: &models.Movie{ID: "BLANK-CREDIT", Credits: []models.MovieCredit{{Actress: &models.Actress{ID: 3, ThumbURL: server.URL + "/blank.jpg", Verified: true}}}}},
		{name: "blank legacy actress is skipped", movie: &models.Movie{ID: "BLANK-LEGACY", Actresses: []models.Actress{{ID: 4, ThumbURL: server.URL + "/blank-legacy.jpg", Verified: true}}, Credits: nil}},
		{name: "legacy actresses fall back", movie: &models.Movie{ID: "LEGACY", Actresses: []models.Actress{verified}, Credits: nil}, wantFiles: []string{"Visible Person.jpg"}, wantNFO: []string{"Visible Person"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			d := NewDownloader(http.DefaultClient, afero.NewOsFs(), &Config{ActorFirstNameOrder: true, UnknownActressMode: models.UnknownActressModeSkip, DownloadActress: true, MediaFormatConfig: organizer.MediaFormatConfig{ActressFolder: ".actors", ActressFormat: "<ACTORNAME>.jpg"}}, nil)
			results, err := d.downloadActressImages(context.Background(), tt.movie, dir)
			require.NoError(t, err)
			got := make([]string, 0, len(results))
			for _, result := range results {
				got = append(got, filepath.Base(result.LocalPath))
			}
			require.ElementsMatch(t, tt.wantFiles, got)

			gen := nfo.NewGenerator(afero.NewOsFs(), &nfo.Config{FilenameTemplate: "movie.nfo", FirstNameOrder: true})
			require.NoError(t, gen.Generate(context.Background(), tt.movie, dir, "", "", nil))
			xml, err := os.ReadFile(filepath.Join(dir, "movie.nfo"))
			require.NoError(t, err)
			for _, name := range tt.wantNFO {
				require.Contains(t, string(xml), "<name>"+name+"</name>")
			}
			for _, name := range tt.denyNFO {
				require.False(t, strings.Contains(string(xml), "<name>"+name+"</name>"))
			}
		})
	}
}
