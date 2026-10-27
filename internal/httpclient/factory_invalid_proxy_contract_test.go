package httpclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestRestyWithFlareSolverrRejectsMalformedProxyBeforeIO(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			result, err := NewRestyClientWithFlareSolverr(
				&models.ProxyProfile{URL: "http://[::1"},
				models.FlareSolverrConfig{Enabled: enabled, URL: server.URL},
				time.Second, 0,
			)
			var parseErr *url.Error
			if result != nil || !errors.As(err, &parseErr) || !strings.Contains(err.Error(), "invalid proxy URL") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if requests.Load() != 0 {
				t.Fatalf("invalid proxy construction made %d FlareSolverr requests", requests.Load())
			}
		})
	}
}
