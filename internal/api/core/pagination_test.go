package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestParsePagination(t *testing.T) {
	testCases := []struct {
		name         string
		queryParams  string
		defaultLimit int
		maxLimit     int
		wantLimit    int
		wantOffset   int
	}{
		{
			name:         "default values when no query params",
			queryParams:  "",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    50,
			wantOffset:   0,
		},
		{
			name:         "valid limit and offset",
			queryParams:  "?limit=100&offset=25",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    100,
			wantOffset:   25,
		},
		{
			name:         "limit exceeds maxLimit clamped",
			queryParams:  "?limit=1000",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    500,
			wantOffset:   0,
		},
		{
			name:         "negative limit falls back to default",
			queryParams:  "?limit=-10",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    50,
			wantOffset:   0,
		},
		{
			name:         "zero limit falls back to default",
			queryParams:  "?limit=0",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    50,
			wantOffset:   0,
		},
		{
			name:         "negative offset falls back to 0",
			queryParams:  "?offset=-5",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    50,
			wantOffset:   0,
		},
		{
			name:         "non-numeric values fall back to defaults",
			queryParams:  "?limit=abc&offset=xyz",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    50,
			wantOffset:   0,
		},
		{
			name:         "events-style max 200",
			queryParams:  "?limit=300",
			defaultLimit: 50,
			maxLimit:     200,
			wantLimit:    200,
			wantOffset:   0,
		},
		{
			name:         "limit exactly at maxLimit",
			queryParams:  "?limit=500",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    500,
			wantOffset:   0,
		},
		{
			name:         "offset zero is valid",
			queryParams:  "?offset=0",
			defaultLimit: 50,
			maxLimit:     500,
			wantLimit:    50,
			wantOffset:   0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodGet, "/test"+tc.queryParams, nil)
			c.Request = req

			limit, offset := ParsePagination(c, tc.defaultLimit, tc.maxLimit)
			assert.Equal(t, tc.wantLimit, limit)
			assert.Equal(t, tc.wantOffset, offset)
		})
	}
}
