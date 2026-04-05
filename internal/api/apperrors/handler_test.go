package apperrors

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestWriteAPIError_PathError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrPathOutsideAllowed)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "access denied")
	assert.Contains(t, w.Body.String(), "PATH_OUTSIDE_ALLOWED_DIRS")
}

func TestWriteAPIError_GenericError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, errors.New("some generic error"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "some generic error")
	assert.NotContains(t, w.Body.String(), "code")
}

func TestWriteAPIError_NewPathError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	err := NewPathError(ErrPathNotExist, "/missing/path")
	WriteAPIError(c, err)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "does not exist")
	assert.Contains(t, w.Body.String(), "PATH_NOT_EXIST")
}

func TestWriteAPIError_OperatorMessageNotInResponse(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrAllowedDirsEmpty)

	assert.NotContains(t, w.Body.String(), ErrAllowedDirsEmpty.OperatorMessage)
	assert.NotContains(t, w.Body.String(), "configuration file")
}

func TestWriteAPIError_DocsFieldIncluded(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrAllowedDirsEmpty)

	assert.Contains(t, w.Body.String(), "/docs/configuration")
}

func TestWriteAPIError_DocsFieldOmitted(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrPathNotExist)

	body := w.Body.String()
	assert.NotContains(t, body, `"docs"`)
}

func TestWriteAPIError_AllowlistEmpty(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrAllowedDirsEmpty)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "no allowed directories configured")
	assert.Contains(t, w.Body.String(), "ALLOWED_DIRS_EMPTY")
}

func TestWriteAPIError_Denylist(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrPathInDenylist)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "system directory")
	assert.Contains(t, w.Body.String(), "PATH_IN_DENYLIST")
}

func TestWriteAPIError_PathNotDir(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrPathNotDir)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "not a directory")
	assert.Contains(t, w.Body.String(), "PATH_NOT_DIR")
}

func TestWriteAPIError_InvalidPath(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrPathInvalid)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cannot access path")
	assert.Contains(t, w.Body.String(), "PATH_INVALID")
}

func TestWriteAPIError_UnresolvablePath(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, ErrPathUnresolvable)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cannot resolve")
	assert.Contains(t, w.Body.String(), "PATH_UNRESOLVABLE")
}

func TestAPIErrorResponse_JSONFormat(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, NewPathError(ErrPathOutsideAllowed, "/test/path"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "error")
	assert.Contains(t, response, "code")
	assert.Equal(t, "PATH_OUTSIDE_ALLOWED_DIRS", response["code"])
}

func TestAPIErrorResponse_GenericErrorJSON(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	WriteAPIError(c, errors.New("generic error"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "error")
	assert.Equal(t, "generic error", response["error"])
	_, hasCode := response["code"]
	assert.False(t, hasCode, "Generic errors should not have code field")
}
