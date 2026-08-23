package temp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/stretchr/testify/assert"
)

func TestServeTempImageUncachedEmptyTruncatedBodyAborts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(lookupPublicIP)
	t.Cleanup(cleanup)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	recorder := httptest.NewRecorder()
	c := newGinTestContext(recorder)
	serveTempImageUncached(c, &core.TempNarrowConfig{}, srv.URL+"/truncated.jpg", srv.URL+"/truncated.jpg")

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
}
