package actresssync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRoutesDispatchBoundedInputs(t *testing.T) {
	cases := []struct {
		method, path, body, operation string
		status, offset, limit         int
	}{
		{"GET", "/status", "", "status", 200, 0, 0},
		{"GET", "/sources", "", "sources", 200, 0, 50},
		{"GET", "/sources?q=%E5%90%8D%E5%89%8D&offset=2&limit=100", "", "sources", 200, 2, 100},
		{"GET", "/jobs", "", "jobs", 200, 0, 20},
		{"GET", "/jobs?cursor=" + testJobID + "&limit=32", "", "jobs", 200, 0, 32},
		{"GET", "/jobs/" + testJobID, "", "job", 200, 0, 0},
		{"GET", "/jobs/" + testJobID + "/tasks", "", "tasks", 200, 0, 50},
		{"GET", "/jobs/" + testJobID + "/tasks?offset=3&limit=100", "", "tasks", 200, 3, 100},
		{"POST", "/reviews", reviewBody, "review", 201, 0, 0},
		{"POST", "/jobs", confirmBody, "confirm", 202, 0, 0},
		{"POST", "/jobs/" + testJobID + "/cancel", cancelBody, "cancel", 200, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			f := newFixture(t)
			w := f.serve(newRequest(tc.method, tc.path, tc.body))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
			calls := f.service.recorded()
			require.Len(t, calls, 1)
			require.Equal(t, tc.operation, calls[0].method)
			require.Equal(t, tc.offset, calls[0].offset)
			require.Equal(t, tc.limit, calls[0].limit)
			switch tc.operation {
			case "review":
				require.Equal(t, "token:token-alice", calls[0].principal)
				require.Equal(t, []database.ActressSyncChoice{{ActressID: 7, SourceKey: testSourceKey}}, calls[0].choices)
			case "confirm":
				require.Equal(t, "token:token-alice", calls[0].principal)
				require.True(t, calls[0].confirmed)
				require.Equal(t, testReviewID, calls[0].id)
			case "job", "tasks", "cancel":
				require.Equal(t, testJobID, calls[0].id)
			}
		})
	}
}

func TestReviewRequiresExactBoundedSelection(t *testing.T) {
	cases := map[string]string{
		"empty": "", "null": "null", "array": "[]", "no choices": "{}", "empty choices": `{"choices":[]}`, "unicode choices alias": `{"choiceſ":[{"actress_id":7,"source_key":"key"}]}`,
		"unicode actress alias":   `{"choices":[{"actreſſ_id":7,"source_key":"key"}]}`,
		"uppercase choices alias": `{"CHOICES":[{"actress_id":7,"source_key":"key"}]}`,
		"null choices":            `{"choices":null}`,
		"zero actor":              `{"choices":[{"actress_id":0,"source_key":"key"}]}`,
		"negative actor":          `{"choices":[{"actress_id":-1,"source_key":"key"}]}`,
		"fraction actor":          `{"choices":[{"actress_id":1.5,"source_key":"key"}]}`,
		"overflow actor":          `{"choices":[{"actress_id":18446744073709551616,"source_key":"key"}]}`,
		"null actor":              `{"choices":[{"actress_id":null,"source_key":"key"}]}`,
		"missing source":          `{"choices":[{"actress_id":1}]}`,
		"null source":             `{"choices":[{"actress_id":1,"source_key":null}]}`,
		"duplicate actor":         `{"choices":[{"actress_id":1,"source_key":"first"},{"actress_id":1,"source_key":"second"}]}`,
		"duplicate source":        `{"choices":[{"actress_id":1,"source_key":"first"},{"actress_id":2,"source_key":"first"}]}`,
		"long source":             `{"choices":[{"actress_id":1,"source_key":"` + strings.Repeat("a", 513) + `"}]}`,
		"control source":          `{"choices":[{"actress_id":1,"source_key":"a\u0000b"}]}`,
		"duplicate top level":     `{"choices":[],"choices":[{"actress_id":7,"source_key":"key"}]}`,
		"duplicate nested":        `{"choices":[{"actress_id":1,"actress_id":7,"source_key":"key"}]}`,
		"case duplicate":          `{"choices":[{"actress_id":1,"ACTRESS_ID":7,"source_key":"key"}]}`,
		"unknown nested":          `{"choices":[{"actress_id":7,"source_key":"key","after_name":"Injected"}]}`,
		"multiple documents":      reviewBody + ` {}`,
		"invalid utf8":            strings.Replace(reviewBody, "NDI", string([]byte{0xff}), 1),
		"too deep":                `{"choices":` + strings.Repeat("[", 12) + `0` + strings.Repeat("]", 12) + `}`,
	}
	for _, field := range []string{"now", "max_age_seconds", "artifact_digest", "actor_token", "request_key", "principal", "owner", "lease_owner", "verified", "before_name", "after_name", "snapshot", "path"} {
		cases["client authority "+field] = strings.TrimSuffix(reviewBody, "}") + `,"` + field + `":"untrusted"}`
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			w := f.serve(newRequest("POST", "/reviews", body))
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Empty(t, f.service.recorded())
		})
	}
	for _, count := range []int{500, 501} {
		t.Run(fmt.Sprintf("count %d", count), func(t *testing.T) {
			choices := make([]choiceRequest, count)
			for i := range choices {
				choices[i] = choiceRequest{ActressID: uint(i + 1), SourceKey: fmt.Sprintf("source-%d", i)}
			}
			data, err := json.Marshal(reviewRequest{Choices: choices})
			require.NoError(t, err)
			f := newFixture(t)
			w := f.serve(newRequest("POST", "/reviews", string(data)))
			if count == 500 {
				require.Equal(t, 201, w.Code, w.Body.String())
				require.Len(t, f.service.recorded()[0].choices, 500)
			} else {
				require.Equal(t, 400, w.Code)
				require.Empty(t, f.service.recorded())
			}
		})
	}
}

func TestWritesRequireExplicitConfirmation(t *testing.T) {
	for _, path := range []string{"/jobs", "/jobs/" + testJobID + "/cancel"} {
		bodies := []string{`{}`, `null`, `{"confirmed":false}`, `{"confirmed":null}`, `{"confirmed":"true"}`, `{"confirmed":1}`, `{"CONFIRMED":true}`, `{"confirmed":false,"Confirmed":true}`, `{"confirmed":true,"confirmed":false}`, `{"confirmed":true,"now":"2027-01-01"}`}
		if path == "/jobs" {
			bodies = append(bodies, `{"confirmed":true}`, `{"confirmed":true,"review_id":"not-issued"}`, `{"confirmed":true,"review_id":"`+strings.Repeat("A", 64)+`"}`)
		}
		for _, body := range bodies {
			t.Run(path+body, func(t *testing.T) {
				f := newFixture(t)
				w := f.serve(newRequest("POST", path, body))
				require.Equal(t, 400, w.Code, w.Body.String())
				require.Empty(t, f.service.recorded())
			})
		}
	}
}

func TestBodyLimitAlsoAppliesToTrailingWhitespace(t *testing.T) {
	for _, tc := range []struct{ path, body string }{{"/reviews", reviewBody}, {"/jobs", confirmBody}, {"/jobs/" + testJobID + "/cancel", cancelBody}} {
		for _, extra := range []int{0, 1} {
			t.Run(tc.path+fmt.Sprint(extra), func(t *testing.T) {
				f := newFixture(t)
				body := tc.body + strings.Repeat(" ", maxBodyBytes-len(tc.body)+extra)
				w := f.serve(newRequest("POST", tc.path, body))
				if extra == 0 {
					require.Less(t, w.Code, 300, w.Body.String())
					require.Len(t, f.service.recorded(), 1)
				} else {
					require.Equal(t, 413, w.Code)
					require.Empty(t, f.service.recorded())
				}
			})
		}
	}
}

func TestMalformedPaginationCannotReachService(t *testing.T) {
	paths := []string{
		"/status?now=tomorrow", "/status?session=session-alice", "/sources?limit=0", "/sources?limit=101", "/sources?limit=-1", "/sources?offset=-1", "/sources?offset=01", "/sources?offset=%2B1", "/sources?offset=999999999999999999999999999999", "/sources?limit=10&limit=20", "/sources?offset=", "/sources?q=%FF", "/sources?q=%GG", "/sources?q=foo;offset=0", "/sources?q=" + strings.Repeat("a", 129), "/sources?q=" + strings.Repeat("a", maxQueryBytes), "/sources?path=/private/dump",
		"/jobs?limit=33", "/jobs?cursor=arbitrary", "/jobs?offset=1", "/jobs?cursor=" + strings.ToUpper(testJobID), "/jobs/not-a-job", "/jobs/name-../tasks", "/jobs/" + testJobID + "/tasks?limit=101", "/jobs/" + testJobID + "/tasks?offset=-1",
	}
	for _, path := range paths {
		t.Run(path[:min(len(path), 100)], func(t *testing.T) {
			f := newFixture(t)
			w := f.serve(newRequest("GET", path, ""))
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Empty(t, f.service.recorded())
		})
	}
}

func TestOnlyWritesUseWriteProtectedGroup(t *testing.T) {
	f := newFixture(t, func(c *gin.Context) { c.AbortWithStatus(http.StatusTooManyRequests) })
	for _, tc := range []struct{ method, path, body string }{{"POST", "/reviews", reviewBody}, {"POST", "/jobs", confirmBody}, {"POST", "/jobs/" + testJobID + "/cancel", cancelBody}} {
		require.Equal(t, 429, f.serve(newRequest(tc.method, tc.path, tc.body)).Code)
	}
	require.Empty(t, f.service.recorded())
	require.Equal(t, 200, f.serve(newRequest("GET", "/status", "")).Code)
}

func TestErrorCategoriesNeverLeakUnderlyingDetails(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{actressnamesync.ErrInvalid, 400, "SYNC_INVALID_REQUEST"}, {actressnamesync.ErrDisabled, 503, "SYNC_DISABLED"}, {actressnamesync.ErrUnavailable, 503, "SYNC_UNAVAILABLE"}, {actressnamesync.ErrStopping, 503, "SYNC_STOPPING"}, {context.Canceled, 503, "SYNC_STOPPING"}, {context.DeadlineExceeded, 504, "SYNC_TIMEOUT"}, {actressnamesync.ErrReviewExpired, 410, "SYNC_REVIEW_EXPIRED"}, {actressnamesync.ErrPrincipal, 403, "SYNC_REVIEW_PRINCIPAL"}, {actressnamesync.ErrReviewCapacity, 429, "SYNC_CAPACITY"}, {database.ErrActressSyncQueueFull, 429, "SYNC_CAPACITY"}, {database.ErrActressSyncProtected, 409, "SYNC_PROTECTED"}, {database.ErrActressSyncStale, 409, "SYNC_STALE"}, {database.ErrActressSyncConflict, 409, "SYNC_CONFLICT"}, {database.ErrActressSyncLease, 409, "SYNC_CONFLICT"}, {gorm.ErrRecordNotFound, 404, "SYNC_NOT_FOUND"}, {errors.New("secret raw error"), 500, "SYNC_INTERNAL"},
	}
	for _, tc := range cases {
		t.Run(tc.code+tc.err.Error(), func(t *testing.T) {
			f := newFixture(t)
			f.service.err = fmt.Errorf("/private/artifact.sql actor-token-secret SELECT name FROM actors: %w", tc.err)
			w := f.serve(newRequest("POST", "/jobs", confirmBody))
			require.Equal(t, tc.status, w.Code)
			var payload contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
			require.Equal(t, tc.code, payload.Code)
			for _, secret := range []string{"/private/", "actor-token-secret", "SELECT", "secret raw error"} {
				require.NotContains(t, w.Body.String(), secret)
			}
			if tc.status == 429 {
				require.Equal(t, "5", w.Header().Get("Retry-After"))
			}
		})
	}
}
