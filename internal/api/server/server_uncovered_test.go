package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestIsSameOrigin_Uncovered(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		host   string
		scheme string
		expect bool
	}{
		{
			name:   "same origin http",
			origin: "http://localhost:8080",
			host:   "localhost:8080",
			scheme: "http",
			expect: true,
		},
		{
			name:   "different port",
			origin: "http://localhost:3000",
			host:   "localhost:8080",
			scheme: "http",
			expect: false,
		},
		{
			name:   "different host",
			origin: "http://other-host:8080",
			host:   "localhost:8080",
			scheme: "http",
			expect: false,
		},
		{
			name:   "different scheme",
			origin: "https://localhost:8080",
			host:   "localhost:8080",
			scheme: "http",
			expect: false,
		},
		{
			name:   "empty origin is same",
			origin: "",
			host:   "localhost:8080",
			scheme: "http",
			expect: true,
		},
		{
			name:   "default http port",
			origin: "http://localhost",
			host:   "localhost",
			scheme: "http",
			expect: true,
		},
		{
			name:   "default https port",
			origin: "https://localhost",
			host:   "localhost",
			scheme: "https",
			expect: true,
		},
		{
			name:   "invalid origin URL",
			origin: "://invalid",
			host:   "localhost",
			scheme: "http",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host
			if tt.scheme == "https" {
				req.TLS = &tls.ConnectionState{}
			}
			result := isSameOrigin(tt.origin, req)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestIsOriginAllowed_Uncovered(t *testing.T) {
	tests := []struct {
		name           string
		origin         string
		allowedOrigins []string
		expect         bool
	}{
		{
			name:           "exact match",
			origin:         "http://localhost:3000",
			allowedOrigins: []string{"http://localhost:3000"},
			expect:         true,
		},
		{
			name:           "no match",
			origin:         "http://evil.com",
			allowedOrigins: []string{"http://localhost:3000"},
			expect:         false,
		},
		{
			name:           "wildcard ignored",
			origin:         "http://evil.com",
			allowedOrigins: []string{"*"},
			expect:         false,
		},
		{
			name:           "empty allowed list",
			origin:         "http://localhost:3000",
			allowedOrigins: []string{},
			expect:         false,
		},
		{
			name:           "multiple origins match",
			origin:         "http://localhost:3000",
			allowedOrigins: []string{"http://other.com", "http://localhost:3000"},
			expect:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOriginAllowed(tt.origin, tt.allowedOrigins)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestAcceptsHTML_Uncovered(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		expect bool
	}{
		{"text/html", "text/html", true},
		{"text/html with quality", "text/html;q=0.9", true},
		{"zero quality", "text/html;q=0", false},
		{"zero quality decimal", "text/html;q=0.0", false},
		{"mixed accept", "application/json, text/html;q=0.9", true},
		{"application/json only", "application/json", false},
		{"empty accept", "", false},
		{"text/html with zero quality after other types", "application/json;q=1.0, text/html;q=0.000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Request.Header.Set("Accept", tt.accept)
			result := acceptsHTML(c)
			assert.Equal(t, tt.expect, result)
		})
	}
}
