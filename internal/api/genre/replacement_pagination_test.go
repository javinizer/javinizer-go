package genre

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplacementPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	maxInt := strconv.Itoa(math.MaxInt)
	endpoints := []struct {
		name         string
		path         string
		defaultLimit int
		maxLimit     int
		newDeps      func(*testing.T, []string) GenreDeps
	}{
		{
			name: "genre", path: "/genres/replacements", defaultLimit: 500, maxLimit: 1000,
			newDeps: func(t *testing.T, originals []string) GenreDeps {
				var replacements []models.GenreReplacement
				if originals != nil {
					replacements = make([]models.GenreReplacement, len(originals))
				}
				for i, original := range originals {
					replacements[i] = models.GenreReplacement{Original: original}
				}
				repo := mocks.NewMockGenreReplacementRepositoryInterface(t)
				repo.EXPECT().List(context.Background()).Return(replacements, nil).Once()
				return NewGenreDeps(database.ReplacementRepos{GenreReplacementRepo: repo}, database.TranslationRepos{})
			},
		},
		{
			name: "word", path: "/words/replacements", defaultLimit: 200, maxLimit: 500,
			newDeps: func(t *testing.T, originals []string) GenreDeps {
				var replacements []models.WordReplacement
				if originals != nil {
					replacements = make([]models.WordReplacement, len(originals))
				}
				for i, original := range originals {
					replacements[i] = models.WordReplacement{Original: original}
				}
				repo := mocks.NewMockWordReplacementRepositoryInterface(t)
				repo.EXPECT().List(context.Background()).Return(replacements, nil).Once()
				return NewGenreDeps(database.ReplacementRepos{WordReplacementRepo: repo}, database.TranslationRepos{})
			},
		},
	}
	datasets := []struct {
		name      string
		originals []string
	}{
		{name: "populated", originals: []string{"first", "second", "third"}},
		{name: "empty", originals: []string{}},
		{name: "nil"},
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			tests := []struct {
				name       string
				query      string
				wantLimit  int
				wantOffset int
				wantEnd    int
			}{
				{name: "default limit", wantLimit: endpoint.defaultLimit, wantEnd: 3},
				{name: "first page", query: "?limit=2&offset=0", wantLimit: 2, wantEnd: 2},
				{name: "middle page", query: "?limit=1&offset=1", wantLimit: 1, wantOffset: 1, wantEnd: 2},
				{name: "exact last page", query: "?limit=2&offset=1", wantLimit: 2, wantOffset: 1, wantEnd: 3},
				{name: "partial last page", query: "?limit=2&offset=2", wantLimit: 2, wantOffset: 2, wantEnd: 3},
				{name: "offset at length", query: "?limit=2&offset=3", wantLimit: 2, wantOffset: 3, wantEnd: 3},
				{name: "offset beyond length", query: "?limit=2&offset=4", wantLimit: 2, wantOffset: 3, wantEnd: 3},
				{name: "MaxInt offset", query: "?offset=" + maxInt, wantLimit: endpoint.defaultLimit, wantOffset: 3, wantEnd: 3},
				{name: "near MaxInt offset", query: "?limit=2&offset=" + strconv.Itoa(math.MaxInt-1), wantLimit: 2, wantOffset: 3, wantEnd: 3},
				{name: "MaxInt limit", query: "?limit=" + maxInt + "&offset=1", wantLimit: endpoint.maxLimit, wantOffset: 1, wantEnd: 3},
				{name: "MaxInt limit and offset", query: "?limit=" + maxInt + "&offset=" + maxInt, wantLimit: endpoint.maxLimit, wantOffset: 3, wantEnd: 3},
				{name: "zero limit", query: "?limit=0", wantLimit: endpoint.defaultLimit, wantEnd: 3},
				{name: "negative parameters", query: "?limit=-1&offset=-1", wantLimit: endpoint.defaultLimit, wantEnd: 3},
				{name: "invalid parameters", query: "?limit=invalid&offset=invalid", wantLimit: endpoint.defaultLimit, wantEnd: 3},
				{name: "out of range parameters", query: "?limit=" + maxInt + "0&offset=" + maxInt + "0", wantLimit: endpoint.defaultLimit, wantEnd: 3},
			}
			for _, dataset := range datasets {
				t.Run(dataset.name, func(t *testing.T) {
					for _, tt := range tests {
						t.Run(tt.name, func(t *testing.T) {
							router := setupGenreRouter(endpoint.newDeps(t, dataset.originals))
							w := httptest.NewRecorder()
							req := httptest.NewRequest(http.MethodGet, endpoint.path+tt.query, nil)
							// No recovery middleware: an overflowing slice bound must fail this test.
							router.ServeHTTP(w, req)
							require.Equal(t, http.StatusOK, w.Code, w.Body.String())

							var response struct {
								Replacements []struct {
									Original string `json:"original"`
								} `json:"replacements"`
								Count  int `json:"count"`
								Total  int `json:"total"`
								Limit  int `json:"limit"`
								Offset int `json:"offset"`
							}
							require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
							wantOffset, wantEnd := tt.wantOffset, tt.wantEnd
							if len(dataset.originals) == 0 {
								wantOffset, wantEnd = 0, 0
							}
							assert.Equal(t, tt.wantLimit, response.Limit)
							assert.Equal(t, wantOffset, response.Offset)
							assert.Equal(t, len(dataset.originals), response.Total)
							assert.Equal(t, wantEnd-wantOffset, response.Count)
							require.Len(t, response.Replacements, wantEnd-wantOffset)
							for i, replacement := range response.Replacements {
								assert.Equal(t, dataset.originals[wantOffset+i], replacement.Original)
							}
							// Preserve the repository's nil versus empty response representation.
							assert.Equal(t, dataset.originals == nil, response.Replacements == nil)
						})
					}
				})
			}
		})
	}
}
