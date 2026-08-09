package batch

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/worker"
)

// F-R14-2: admission conflicts surface as 409; every other failure is 500.
func TestWriteRescrapeOpErrorMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("direct conflict is 409", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		writeRescrapeOpError(c, &worker.EditAdmissionConflictError{Message: "witness fence"})
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "witness fence")
	})

	t.Run("wrapped conflict is 409", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		writeRescrapeOpError(c, fmt.Errorf("outer: %w", &worker.EditAdmissionConflictError{Message: "wrapped fence"}))
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("generic error is 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		writeRescrapeOpError(c, errors.New("boom"))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Rescrape failed")
	})
}

// The batch-side anchored marker tail check mirrors the worker one: every
// malformed shape is rejected, every guard arm is load-bearing.
func TestHexLowerHexTailBatchGuards(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"no dot", "abc", false},
		{"dot too early", "a.b2", false},
		{"trailing dot", "x.a1.", false},
		{"missing second dot", "ab.cd", false},
		{"empty first hex part", "x..b2", false},
		{"empty second hex part", "x.a1.", false},
		{"non-hex first part", "x.g1.b2", false},
		{"non-hex second part", "x.a1.zz", false},
		{"uppercase hex rejected", "x.A1.B2", false},
		{"valid", "x.a1.b2", true},
		{"valid long", ".inflight-IPX-1.1a2b3c.d4e5f6", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, hexLowerHexTail(tc.in), "hexLowerHexTail(%q)", tc.in)
		})
	}
}
