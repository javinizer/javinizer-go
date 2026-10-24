package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJavLibraryNameOverrideRoutesRequireAuthentication(t *testing.T) {
	router, deps := setupAuthenticatedTestServer(t)
	require.NoError(t, deps.Auth.Setup("admin", "password123"))
	endpoint := "/api/v1/scrapers/javlibrary/name-overrides/aa6sc"
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		request := httptest.NewRequest(method, endpoint, strings.NewReader(`{"source_name":"Yorimoto Shiori","first_name":"Shiori","last_name":"Yorimoto","expected_rule":null}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	}
	session, err := deps.Auth.Login("admin", "password123", false)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, endpoint, nil)
	request.AddCookie(&http.Cookie{Name: "javinizer_session", Value: session})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.JSONEq(t, `{"rule":null}`, response.Body.String())
}
