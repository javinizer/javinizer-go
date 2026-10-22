package actress

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func catalogFilterRequest(t *testing.T, repo database.ActressRepositoryInterface, ctx context.Context, params url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/actresses", listActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/actresses?"+params.Encode(), nil).WithContext(ctx))
	return w
}

func TestCatalogFiltersHTTP(t *testing.T) {
	db, repo, _ := setupActressTestDB(t)
	rows := []models.Actress{
		{ID: 1, DMMID: 123, FirstName: "Complete", JapaneseName: "検索一", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/one.jpg", Verified: true},
		{ID: 2, FirstName: "NoDMM", JapaneseName: "検索二", ThumbURL: "https://example.com/custom", Verified: true},
		{ID: 3, DMMID: 234, FirstName: "NoThumb", JapaneseName: "検索三", ThumbURL: "  ", Verified: true},
		{ID: 4, DMMID: 345, FirstName: "NoJapanese", JapaneseName: "  ", ThumbURL: "https://example.com/custom", Verified: true},
		{ID: 5, DMMID: 456, JapaneseName: "検索五", FirstName: "  ", LastName: " ", ThumbURL: "https://example.com/custom", Verified: true},
		{ID: 6, DMMID: 567, FirstName: "Placeholder", JapaneseName: "検索六", ThumbURL: "https://pics.dmm.co.jp/mono/noimage/now_printing.jpg", Verified: true},
		{ID: 7, DMMID: 678, FirstName: "Extensionless", JapaneseName: "検索七", ThumbURL: "https://awsimgsrc.dmm.com/mono/actjpgs/name", Verified: true},
		{ID: 8, DMMID: 923, FirstName: "Candidate", JapaneseName: "検索候補", Origin: "scrape", Verified: false},
	}
	require.NoError(t, db.Create(&rows).Error)
	cases := []struct {
		filter        string
		ids, searched []uint
	}{
		{"", []uint{1, 2, 3, 4, 5, 6, 7}, []uint{1, 2, 3, 5, 6, 7}},
		{"missing_dmm", []uint{2}, []uint{2}},
		{"has_dmm", []uint{1, 3, 4, 5, 6, 7}, []uint{1, 3, 5, 6, 7}},
		{"missing_thumbnail", []uint{3, 6, 7}, []uint{3, 6, 7}},
		{"missing_japanese_name", []uint{4}, []uint{}},
		{"japanese_name_only", []uint{5}, []uint{5}},
		{"missing_metadata", []uint{3, 4, 5, 6, 7}, []uint{3, 5, 6, 7}},
	}
	for _, tc := range cases {
		for _, search := range []bool{false, true} {
			name := tc.filter
			query := ""
			want := tc.ids
			if search {
				name += "/search"
				query = " 検索 "
				want = tc.searched
			}
			t.Run(name, func(t *testing.T) {
				for _, order := range []string{"asc", "desc"} {
					ordered := append([]uint{}, want...)
					if order == "desc" {
						for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
							ordered[i], ordered[j] = ordered[j], ordered[i]
						}
					}
					// Every page, including the empty page after the end, must share the same total.
					for offset := 0; offset <= len(ordered); offset++ {
						params := url.Values{"filter": {" " + tc.filter + " "}, "q": {query}, "sort_by": {"id"}, "sort_order": {order}, "limit": {"1"}, "offset": {strconv.Itoa(offset)}}
						w := catalogFilterRequest(t, repo, t.Context(), params)
						require.Equal(t, 200, w.Code, w.Body.String())
						var got actressesResponse
						require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
						require.EqualValues(t, len(want), got.Total)
						require.Equal(t, offset, got.Offset)
						require.Equal(t, 1, got.Limit)
						if offset == len(ordered) {
							require.Zero(t, got.Count)
							require.Empty(t, got.Actresses)
						} else {
							require.Equal(t, 1, got.Count)
							require.Len(t, got.Actresses, 1)
							require.Equal(t, ordered[offset], got.Actresses[0].ID)
						}
					}
				}
			})
		}
	}
	for _, filter := range []string{"", "has_dmm", "missing_thumbnail", "missing_dmm"} {
		w := catalogFilterRequest(t, repo, t.Context(), url.Values{"q": {"23"}, "filter": {filter}, "sort_by": {"id"}})
		require.Equal(t, 200, w.Code, w.Body.String())
		var got actressesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		want := []uint{1, 3}
		if filter == "missing_thumbnail" {
			want = []uint{3}
		}
		if filter == "missing_dmm" {
			want = []uint{}
		}
		ids := []uint{}
		for _, row := range got.Actresses {
			ids = append(ids, row.ID)
		}
		require.Equal(t, want, ids)
		require.EqualValues(t, len(want), got.Total)
	}
}

func TestCatalogFiltersHTTPInvalidArguments(t *testing.T) {
	// Nil repositories ensure validation fails before any database call.
	for _, params := range []url.Values{
		{"filter": {"unknown"}}, {"filter": {"has_dmm OR 1=1"}}, {"filter": {"has_dmm,missing_thumbnail"}},
		{"filter": {"has_dmm"}, "sort_by": {"unknown"}}, {"filter": {"has_dmm"}, "sort_order": {"sideways"}},
	} {
		w := catalogFilterRequest(t, nil, t.Context(), params)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
}

func TestCatalogFiltersHTTPRepositoryFailuresAndContext(t *testing.T) {
	for _, search := range []bool{false, true} {
		for _, failCount := range []bool{false, true} {
			name := "list"
			if search {
				name = "search"
			}
			if failCount {
				name += "/count"
			} else {
				name += "/page"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				repo := mocks.NewMockActressRepositoryInterface(t)
				sameContext := mock.MatchedBy(func(got context.Context) bool { return got == ctx })
				err := errors.New("catalog unavailable")
				if search {
					if failCount {
						repo.EXPECT().CountSearchFiltered(sameContext, "23", "has_dmm").Return(0, err).Once()
					} else {
						repo.EXPECT().CountSearchFiltered(sameContext, "23", "has_dmm").Return(2, nil).Once()
						repo.EXPECT().SearchFiltered(sameContext, "23", "has_dmm", 1, 1, "id", "desc").Return(nil, err).Once()
					}
				} else {
					if failCount {
						repo.EXPECT().CountFiltered(sameContext, "has_dmm").Return(0, err).Once()
					} else {
						repo.EXPECT().CountFiltered(sameContext, "has_dmm").Return(2, nil).Once()
						repo.EXPECT().ListFiltered(sameContext, "has_dmm", 1, 1, "id", "desc").Return(nil, err).Once()
					}
				}
				params := url.Values{"filter": {" has_dmm "}, "limit": {"1"}, "offset": {"1"}, "sort_by": {"id"}, "sort_order": {"desc"}}
				if search {
					params.Set("q", " 23 ")
				}
				w := catalogFilterRequest(t, repo, ctx, params)
				require.Equal(t, 500, w.Code)
				require.Contains(t, w.Body.String(), "catalog unavailable")
			})
		}
	}
}

func TestCatalogFiltersHTTPCanceledContext(t *testing.T) {
	_, repo, _ := setupActressTestDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, query := range []string{"", "23"} {
		w := catalogFilterRequest(t, repo, ctx, url.Values{"filter": {"has_dmm"}, "q": {query}})
		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Contains(t, w.Body.String(), "context canceled")
	}
}
