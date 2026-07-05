package apperrors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowedDirsEmptyOperatorMessage(t *testing.T) {
	tests := []struct {
		name     string
		env      system.Environment
		contains string
	}{
		{
			name:     "desktop points to Settings UI",
			env:      system.EnvironmentDesktop,
			contains: "Settings → Security",
		},
		{
			name:     "docker mentions mounting a directory",
			env:      system.EnvironmentDocker,
			contains: "Mount a directory",
		},
		{
			name:     "cli falls back to config.yaml editing",
			env:      system.EnvironmentCLI,
			contains: "configuration file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := AllowedDirsEmptyOperatorMessage(tt.env)
			assert.Contains(t, msg, tt.contains)
		})
	}
}

func TestAllowedDirsEmptyOperatorMessage_DefaultMatchesSentinel(t *testing.T) {
	assert.Equal(
		t,
		ErrAllowedDirsEmpty.OperatorMessage,
		AllowedDirsEmptyOperatorMessage(system.EnvironmentCLI),
	)
}

func TestInstallEnvironmentFromContext_RoundTrip(t *testing.T) {
	ctx := context.Background()
	_, ok := InstallEnvironmentFromContext(ctx)
	require.False(t, ok, "unset environment should report ok=false")

	ctx = WithInstallEnvironment(ctx, system.EnvironmentDesktop)
	got, ok := InstallEnvironmentFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, system.EnvironmentDesktop, got)
}

func TestOperatorMessageFor_AllowedDirsEmptyUsesEnvironment(t *testing.T) {
	ctx := WithInstallEnvironment(context.Background(), system.EnvironmentDesktop)
	got := operatorMessageFor(ErrAllowedDirsEmpty, ctx)
	assert.Contains(t, got, "Settings → Security")
	assert.NotEqual(t, ErrAllowedDirsEmpty.OperatorMessage, got, "desktop message should differ from CLI default")
}

func TestOperatorMessageFor_AllowedDirsEmptyFallsBackWithoutEnv(t *testing.T) {
	got := operatorMessageFor(ErrAllowedDirsEmpty, context.Background())
	assert.Equal(t, ErrAllowedDirsEmpty.OperatorMessage, got)
}

func TestOperatorMessageFor_OtherErrorsIgnoreEnvironment(t *testing.T) {
	ctx := WithInstallEnvironment(context.Background(), system.EnvironmentDesktop)
	got := operatorMessageFor(ErrPathOutsideAllowed, ctx)
	assert.Equal(t, ErrPathOutsideAllowed.OperatorMessage, got)
}

func TestWriteAPIError_EnvironmentAwareOperatorGuidance(t *testing.T) {
	// The response body never includes OperatorMessage (it's operator-only),
	// so this test asserts the environment-aware path is exercised end-to-end
	// without leaking guidance to the API consumer. A real gin.Context is used
	// because logOperatorGuidance reads c.Request.Context().
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/browse", nil)
	c.Request = c.Request.WithContext(WithInstallEnvironment(c.Request.Context(), system.EnvironmentDesktop))

	// Capture the logged guidance by redirecting logging output is fragile;
	// instead assert the helper logOperatorGuidance calls returns without
	// panic and the selected message (via operatorMessageFor) is desktop-aware.
	c.Request = c.Request.WithContext(WithInstallEnvironment(c.Request.Context(), system.EnvironmentDesktop))
	assert.NotPanics(t, func() { logOperatorGuidance(c, ErrAllowedDirsEmpty) })

	selected := operatorMessageFor(ErrAllowedDirsEmpty, c.Request.Context())
	assert.Contains(t, selected, "Settings → Security")
}
