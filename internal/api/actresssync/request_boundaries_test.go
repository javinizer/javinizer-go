package actresssync

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type failedRequestBody struct{}

func (failedRequestBody) Read([]byte) (int, error) {
	return 0, errors.New("synthetic interrupted request body")
}
func (failedRequestBody) Close() error { return nil }

func TestMalformedRequestBoundariesNeverInvokeService(t *testing.T) {
	for _, tc := range []struct{ name, method, path, body string }{
		{"job_query", http.MethodGet, "/jobs/" + testJobID + "?unknown=1", ""},
		{"tasks_query", http.MethodGet, "/jobs/" + testJobID + "/tasks?offset=%zz", ""},
		{"review_query", http.MethodPost, "/reviews?unknown=1", reviewBody},
		{"confirm_query", http.MethodPost, "/jobs?unknown=1", confirmBody},
		{"cancel_query", http.MethodPost, "/jobs/" + testJobID + "/cancel?unknown=1", cancelBody},
		{"missing_value", http.MethodPost, "/reviews", `{"choices":`},
		{"broken_object_key", http.MethodPost, "/reviews", `{"choices":[],"`},
		{"broken_array_value", http.MethodPost, "/reviews", `{"choices":[`},
		{"excessive_depth", http.MethodPost, "/reviews", `{"choices":` + strings.Repeat("[", 9) + strings.Repeat("]", 9) + `}`},
		{"bad_object_separator", http.MethodPost, "/reviews", `{"choices":[] "confirmed":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			w := f.serve(newRequest(tc.method, tc.path, tc.body))
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Empty(t, f.service.recorded())
		})
	}
}
func TestRequestReadFailureDoesNotInvokeService(t *testing.T) {
	f := newFixture(t)
	r := newRequest(http.MethodPost, "/reviews", reviewBody)
	r.Body = io.NopCloser(failedRequestBody{})
	w := f.serve(r)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, f.service.recorded())
}
func TestCancelledRequestDoesNotInvokeService(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := f.handler.call(ctx, func(context.Context, syncService) (any, error) {
		t.Fatal("cancelled request reached service")
		return nil, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, f.service.recorded())
}
