package actress

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

func TestHTTPManualNameEditProtectsAgainstImportOverwrite(t *testing.T) {
	repo := newMockActressRepo(t)
	router := gin.New()
	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router.POST("/import", importActresses(deps))
	router.PUT("/actresses/:id", updateActress(deps))
	call := func(method, path, body string) string {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, 200, w.Code, w.Body.String())
		return w.Body.String()
	}
	call("POST", "/import", `{"actresses":[{"dmm_id":81235,"first_name":"Imported"}]}`)
	a, err := repo.FindByDMMID(t.Context(), 81235)
	require.NoError(t, err)
	call("PUT", fmt.Sprintf("/actresses/%d", a.ID), `{"dmm_id":81235,"first_name":"Manual"}`)
	manual, err := repo.FindByID(t.Context(), a.ID)
	require.NoError(t, err)
	t.Logf("after manual PUT: name=%q origin=%q", manual.FirstName, manual.Origin)
	out := call("POST", "/import", `{"overwrite_names":true,"actresses":[{"dmm_id":81235,"first_name":"Reimported"}]}`)
	after, err := repo.FindByID(t.Context(), a.ID)
	require.NoError(t, err)
	t.Logf("overwrite response=%s after=%q origin=%q", out, after.FirstName, after.Origin)
	require.Equal(t, "Manual", after.FirstName)
}
