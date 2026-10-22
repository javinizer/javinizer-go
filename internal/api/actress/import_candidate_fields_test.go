package actress

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestImportOverwriteSparseCandidatePreservesFields(t *testing.T) {
	for _, reference := range []string{"id", "dmm", "first", "last", "japanese"} {
		t.Run(reference, func(t *testing.T) {
			repo := newMockActressRepo(t)
			a := models.Actress{DMMID: 821, FirstName: "Given", LastName: "Family", JapaneseName: "日本名", ThumbURL: "candidate.jpg", Origin: database.ActressOriginScrape}
			require.NoError(t, repo.Create(t.Context(), &a))
			require.False(t, a.Verified)
			item := actressImportItem{}
			switch reference {
			case "id":
				item.ID = a.ID
			case "dmm":
				item.DMMID = a.DMMID
			case "first":
				item.ID = a.ID
				item.FirstName = "New"
			case "last":
				item.ID = a.ID
				item.LastName = "New"
			case "japanese":
				item.ID = a.ID
				item.JapaneseName = "新名"
			}
			data, err := json.Marshal(actressesImportRequest{OverwriteNames: true, Actresses: []actressImportItem{item}})
			require.NoError(t, err)
			router := gin.New()
			router.POST("/import", importActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
			request := httptest.NewRequest(http.MethodPost, "/import", bytes.NewReader(data))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			var summary importSummaryResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
			require.Equal(t, 1, summary.Updated)
			require.Zero(t, summary.Errors)
			got, err := repo.FindByID(t.Context(), a.ID)
			require.NoError(t, err)
			wantFirst := a.FirstName
			if reference == "first" {
				wantFirst = "New"
			}
			require.Equal(t, wantFirst, got.FirstName)
			wantLast := a.LastName
			if reference == "last" {
				wantLast = "New"
			}
			require.Equal(t, wantLast, got.LastName)
			wantJapanese := a.JapaneseName
			if reference == "japanese" {
				wantJapanese = "新名"
			}
			require.Equal(t, wantJapanese, got.JapaneseName)
			require.Equal(t, a.ThumbURL, got.ThumbURL)
			require.Equal(t, a.DMMID, got.DMMID)
			require.True(t, got.Verified)
			require.Equal(t, database.ActressOriginImport, got.Origin)
		})
	}
}
