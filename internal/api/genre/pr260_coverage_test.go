package genre

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUpdateWordReplacementRejectsBlankOriginalPR260(t *testing.T) {
	deps, _ := newTestWordDeps(t)
	router := gin.New()
	router.PUT("/words/replacements", updateWordReplacement(deps, func() {}))
	request := httptest.NewRequest(http.MethodPut, "/words/replacements", bytes.NewBufferString(`{"original":"  ","replacement":"value"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}
