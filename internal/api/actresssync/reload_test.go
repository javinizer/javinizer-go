package actresssync

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/stretchr/testify/require"
)

func TestReloadHasNoCallerArtifactAuthority(t *testing.T) {
	for _, body := range []string{"", "{}", " \n {} \t"} {
		f := newFixture(t)
		w := f.serve(newRequest("POST", "/sources/reload", body))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
		require.Equal(t, []serviceCall{{method: "reload"}}, f.service.recorded())
	}
	for _, body := range []string{
		`null`, `[]`, `{} {}`, `{"path":"/tmp"}`, `{"url":"https://example.com"}`,
		`{"now":"2026-01-01"}`, `{"artifact_digest":"` + testReviewID + `"}`,
		`{"snapshot":{}}`, `{"confirmed":true}`, `{"choices":[]}`, `{"path":"a","path":"b"}`,
		string([]byte{0xff}),
	} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t)
			w := f.serve(newRequest("POST", "/sources/reload", body))
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Empty(t, f.service.recorded())
		})
	}
	for _, query := range []string{"path=/tmp", "artifact_digest=" + testReviewID, "q=test", "now=1"} {
		f := newFixture(t)
		w := f.serve(newRequest("POST", "/sources/reload?"+query, "{}"))
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Empty(t, f.service.recorded())
	}
	f := newFixture(t)
	w := f.serve(newRequest("POST", "/sources/reload", strings.Repeat(" ", maxBodyBytes+1)))
	require.Equal(t, 413, w.Code)
	require.Empty(t, f.service.recorded())
}

func TestReloadRequiresAuthenticationIntentOriginAndWriteRateLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*http.Request)
		status int
	}{
		{"authentication", func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"intent", func(r *http.Request) { r.Header.Del(intentHeader) }, 403},
		{"origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"content type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			r := newRequest("POST", "/sources/reload", "{}")
			tc.modify(r)
			w := f.serve(r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Empty(t, f.service.recorded())
		})
	}
	f := newFixture(t, func(c *gin.Context) { c.AbortWithStatus(429) })
	w := f.serve(newRequest("POST", "/sources/reload", "{}"))
	require.Equal(t, 429, w.Code)
	require.Empty(t, f.service.recorded())
}

func TestReloadSafeErrorCategories(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{actressnamesync.ErrReloadBusy, 429, "SYNC_RELOAD_BUSY"},
		{actressnamesync.ErrReloadFailed, 503, "SYNC_RELOAD_FAILED"},
		{actressnamesync.ErrStaleGeneration, 409, "SYNC_STALE_GENERATION"},
	} {
		f := newFixture(t)
		f.service.err = fmt.Errorf("/private/artifact.json: %w", tc.err)
		w := f.serve(newRequest("POST", "/sources/reload", "{}"))
		require.Equal(t, tc.status, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), tc.code)
		require.NotContains(t, w.Body.String(), "/private/")
	}
}

func TestDisplayedDigestIsPassedOnlyAsEqualityPrecondition(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/sources?offset=1&artifact_digest=" + testReviewID, ""},
		{"POST", "/reviews", strings.TrimSuffix(reviewBody, "}") + `,"artifact_digest":"` + testReviewID + `"}`},
	} {
		f := newFixture(t)
		w := f.serve(newRequest(tc.method, tc.path, tc.body))
		require.Less(t, w.Code, 300, w.Body.String())
		require.Len(t, f.service.recorded(), 1)
		require.Equal(t, testReviewID, f.service.recorded()[0].digest)
	}
	for _, digest := range []string{"wrong", strings.Repeat("A", 64), strings.Repeat("0", 65)} {
		f := newFixture(t)
		w := f.serve(newRequest("GET", "/sources?artifact_digest="+digest, ""))
		require.Equal(t, 400, w.Code)
		require.Empty(t, f.service.recorded())
	}
}
