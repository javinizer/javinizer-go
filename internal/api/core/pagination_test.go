package core

import (
	"math"
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

func TestPaginationBounds(t *testing.T) {
	tests := []struct {
		name      string
		length    int
		limit     int
		offset    int
		wantStart int
		wantEnd   int
	}{
		{name: "first page", length: 5, limit: 2, wantEnd: 2},
		{name: "middle page", length: 5, limit: 2, offset: 2, wantStart: 2, wantEnd: 4},
		{name: "partial last page", length: 5, limit: 2, offset: 4, wantStart: 4, wantEnd: 5},
		{name: "exact last page", length: 5, limit: 2, offset: 3, wantStart: 3, wantEnd: 5},
		{name: "offset at length", length: 5, limit: 2, offset: 5, wantStart: 5, wantEnd: 5},
		{name: "offset beyond length", length: 5, limit: 2, offset: 6, wantStart: 5, wantEnd: 5},
		{name: "empty data", limit: 2},
		{name: "empty data beyond length", limit: math.MaxInt, offset: math.MaxInt},
		{name: "zero limit", length: 5, offset: 2, wantStart: 2, wantEnd: 2},
		{name: "MaxInt offset", length: 5, limit: 2, offset: math.MaxInt, wantStart: 5, wantEnd: 5},
		{name: "MaxInt limit", length: 5, limit: math.MaxInt, offset: 2, wantStart: 2, wantEnd: 5},
		{name: "MaxInt limit and offset", length: 5, limit: math.MaxInt, offset: math.MaxInt, wantStart: 5, wantEnd: 5},
		{name: "MaxInt length", length: math.MaxInt, limit: math.MaxInt, wantEnd: math.MaxInt},
		{name: "MaxInt length partial page", length: math.MaxInt, limit: math.MaxInt, offset: math.MaxInt - 1, wantStart: math.MaxInt - 1, wantEnd: math.MaxInt},
		{name: "MaxInt length and offset", length: math.MaxInt, limit: math.MaxInt, offset: math.MaxInt, wantStart: math.MaxInt, wantEnd: math.MaxInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := PaginationBounds(tt.length, tt.limit, tt.offset)
			assert.Equal(t, tt.wantStart, start)
			assert.Equal(t, tt.wantEnd, end)
		})
	}
}
