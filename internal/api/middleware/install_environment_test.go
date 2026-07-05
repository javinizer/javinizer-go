package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallEnvironmentInjector_StampsContext(t *testing.T) {
	tests := []struct {
		name   string
		env    system.Environment
		wantOK bool
		want   system.Environment
	}{
		{"desktop", system.EnvironmentDesktop, true, system.EnvironmentDesktop},
		{"docker", system.EnvironmentDocker, true, system.EnvironmentDocker},
		{"cli", system.EnvironmentCLI, true, system.EnvironmentCLI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			deps := &commandutil.CoreDeps{}
			deps.SetInstallEnvironment(tt.env)

			router := gin.New()
			router.Use(InstallEnvironmentInjector(deps))
			router.GET("/test", func(c *gin.Context) {
				got, ok := apperrors.InstallEnvironmentFromContext(c.Request.Context())
				require.Equal(t, tt.wantOK, ok)
				assert.Equal(t, tt.want, got)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestInstallEnvironmentInjector_NilDepsDefaultsToCLI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(InstallEnvironmentInjector(nil))
	router.GET("/test", func(c *gin.Context) {
		got, ok := apperrors.InstallEnvironmentFromContext(c.Request.Context())
		require.True(t, ok)
		assert.Equal(t, system.EnvironmentCLI, got)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestInstallEnvironmentInjector_TypedNilDepsDefaultsToCLI guards against the
// typed-nil interface pitfall: a (*commandutil.CoreDeps)(nil) stored in an
// interface satisfies != nil, so a plain `deps != nil` guard would call
// deps.InstallEnvironment() on a nil pointer and panic. The injector accepts
// the concrete pointer directly so the nil check works.
func TestInstallEnvironmentInjector_TypedNilDepsDefaultsToCLI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var typedNil *commandutil.CoreDeps
	router := gin.New()
	router.Use(InstallEnvironmentInjector(typedNil))
	router.GET("/test", func(c *gin.Context) {
		got, ok := apperrors.InstallEnvironmentFromContext(c.Request.Context())
		require.True(t, ok)
		assert.Equal(t, system.EnvironmentCLI, got)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
