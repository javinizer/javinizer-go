package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateJobIDRejectsUnsafeIDsBeforeHandler(t *testing.T) {
	for _, path := range []string{"/jobs/.", "/jobs/..", "/jobs/a%5Cb", "/missing-id"} {
		t.Run(path, func(t *testing.T) {
			called := false
			router := gin.New()
			handler := func(c *gin.Context) { called = true; c.Status(http.StatusNoContent) }
			router.GET("/jobs/:id", ValidateJobID(), handler)
			router.GET("/missing-id", ValidateJobID(), handler)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), `"error":`)
			require.False(t, called, "unsafe job identifiers must never reach filesystem handlers")
		})
	}
}

func TestValidateJobIDPassesSafeIDUnchanged(t *testing.T) {
	router := gin.New()
	observed := ""
	router.GET("/jobs/:id", ValidateJobID(), func(c *gin.Context) { observed = c.Param("id"); c.Status(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jobs/job-123_abc", nil))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "job-123_abc", observed)
}
