package temp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type writeBodyErrorWriter struct {
	header http.Header
	status int
}

func (w *writeBodyErrorWriter) Header() http.Header       { return w.header }
func (w *writeBodyErrorWriter) WriteHeader(status int)    { w.status = status }
func (w *writeBodyErrorWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }

func TestWriteImageBodyHeadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c := newGinTestContext(recorder)
	c.Request.Method = http.MethodHead

	writeImageBody(c, []byte("body"))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, recorder.Body.String())
}

func TestWriteImageBodyWriterFailureAborts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := &writeBodyErrorWriter{header: make(http.Header)}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodGet, "/temp/image", nil)

	writeImageBody(c, []byte("body"))

	assert.True(t, c.IsAborted())
	// Gin has already committed the 200 header before the underlying writer
	// reports its error; the important contract is that the context is aborted.
	assert.Equal(t, http.StatusOK, writer.status)
}
