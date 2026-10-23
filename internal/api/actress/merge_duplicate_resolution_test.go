package actress

import (
	"bytes"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditHTTPRejectsDuplicateDecisionsWithoutMutation(t *testing.T) {
	repo := newMockActressRepo(t)
	target := models.Actress{FirstName: "Target"}
	source := models.Actress{FirstName: "Source"}
	require.NoError(t, repo.Create(t.Context(), &target))
	require.NoError(t, repo.Create(t.Context(), &source))
	router := gin.New()
	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router.POST("/preview", previewActressMerge(deps))
	router.POST("/merge", mergeActresses(deps))
	for i := 0; i < 40; i++ {
		for _, path := range []string{"/preview", "/merge"} {
			body := fmt.Sprintf(`{"target_id":%d,"source_id":%d,"resolutions":{"FIRST_NAME":"source","first_name":"target"}}`, target.ID, source.ID)
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, 400, w.Code, w.Body.String())
			a, err := repo.FindByID(t.Context(), target.ID)
			require.NoError(t, err)
			require.Equal(t, "Target", a.FirstName)
			b, err := repo.FindByID(t.Context(), source.ID)
			require.NoError(t, err)
			require.Equal(t, "Source", b.FirstName)
		}
	}
}
