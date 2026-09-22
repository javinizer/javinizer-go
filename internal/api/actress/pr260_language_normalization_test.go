package actress

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActressAPIIncludeTranslationsNormalizesLanguage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, actressRepo, translationRepo := setupActressTestDB(t)
	actress := models.Actress{FirstName: "API"}
	require.NoError(t, actressRepo.Create(t.Context(), &actress))
	require.NoError(t, translationRepo.Upsert(t.Context(), &models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "API EN"}))
	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: actressRepo}, TranslationRepos: database.TranslationRepos{ActressTranslationRepo: translationRepo}}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))
	router.GET("/actresses/:id", getActress(deps))

	for _, path := range []string{"/actresses?include_translations=%20EN%20", "/actresses/1?include_translations=%20eN%20"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		if path == "/actresses?include_translations=%20EN%20" {
			var body actressesResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Len(t, body.Actresses, 1)
			require.Len(t, body.Actresses[0].Translations, 1)
		} else {
			var body models.Actress
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Len(t, body.Translations, 1)
		}
	}
}
