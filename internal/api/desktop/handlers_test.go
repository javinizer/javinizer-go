package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/system"
	"github.com/javinizer/javinizer-go/internal/updater"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type stubUpdater struct {
	mu             sync.Mutex
	status         updater.Status
	upgradeResult  *updater.UpgradeResult
	upgradeErr     error
	upgradeOpts    updater.UpgradeOptions
	upgradeCalled  bool
	relaunchErr    error
	relaunchCalled bool
}

func (s *stubUpdater) Upgrade(ctx context.Context, opts updater.UpgradeOptions) (*updater.UpgradeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upgradeOpts = opts
	s.upgradeCalled = true
	return s.upgradeResult, s.upgradeErr
}

func (s *stubUpdater) Status() updater.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *stubUpdater) Relaunch(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relaunchCalled = true
	return s.relaunchErr
}

func (s *stubUpdater) upgradeCalledNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upgradeCalled
}

func (s *stubUpdater) relaunchCalledNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.relaunchCalled
}

func (s *stubUpdater) upgradeForce() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upgradeOpts.Force
}

type stubDeps struct {
	commandutil.CoreDepsReader
	env     system.Environment
	updater updater.BundleUpdater
}

func (s *stubDeps) InstallEnvironment() system.Environment { return s.env }
func (s *stubDeps) BundleUpdater() updater.BundleUpdater   { return s.updater }

func newDesktopDeps(u updater.BundleUpdater) *stubDeps {
	return &stubDeps{env: system.EnvironmentDesktop, updater: u}
}

func performUpgradeRequest(t *testing.T, deps commandutil.CoreDepsReader, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.POST("/desktop/upgrade", upgrade(deps))

	req := httptest.NewRequest(http.MethodPost, "/desktop/upgrade", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestUpgrade_Handler(t *testing.T) {
	cases := []struct {
		name         string
		deps         commandutil.CoreDepsReader
		body         string
		wantCode     int
		wantBodySub  string
		wantUpgrade  bool
		wantForce    bool
		wantRelaunch bool
	}{
		{
			name:        "404 non-desktop environment",
			deps:        &stubDeps{env: system.EnvironmentCLI, updater: &stubUpdater{}},
			wantCode:    http.StatusNotFound,
			wantUpgrade: false,
		},
		{
			name:        "404 no bundle updater",
			deps:        &stubDeps{env: system.EnvironmentDesktop, updater: nil},
			wantCode:    http.StatusNotFound,
			wantUpgrade: false,
		},
		{
			name: "409 upgrade already in progress",
			deps: &stubDeps{env: system.EnvironmentDesktop, updater: &stubUpdater{
				status: updater.Status{State: updater.StateDownloading},
			}},
			wantCode:    http.StatusConflict,
			wantUpgrade: false,
		},
		{
			name: "200 success triggers relaunch after response",
			deps: newDesktopDeps(&stubUpdater{
				status:        updater.Status{State: updater.StateIdle},
				upgradeResult: &updater.UpgradeResult{LatestVersion: "1.2.3"},
			}),
			body:         `{"force":true}`,
			wantCode:     http.StatusOK,
			wantBodySub:  "relaunching",
			wantUpgrade:  true,
			wantForce:    true,
			wantRelaunch: true,
		},
		{
			name: "500 upgrade error",
			deps: newDesktopDeps(&stubUpdater{
				status:     updater.Status{State: updater.StateIdle},
				upgradeErr: errors.New("simulated download failure"),
			}),
			wantCode:    http.StatusInternalServerError,
			wantUpgrade: true,
		},
		{
			name: "200 up-to-date does not relaunch",
			deps: newDesktopDeps(&stubUpdater{
				status:        updater.Status{State: updater.StateIdle},
				upgradeResult: &updater.UpgradeResult{UpToDate: true, LatestVersion: "1.2.3"},
			}),
			wantCode:     http.StatusOK,
			wantBodySub:  "up-to-date",
			wantUpgrade:  true,
			wantRelaunch: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performUpgradeRequest(t, tc.deps, tc.body)
			assert.Equal(t, tc.wantCode, rec.Code)

			if tc.wantBodySub != "" {
				assert.Contains(t, rec.Body.String(), tc.wantBodySub)
			}

			stub, _ := extractStub(tc.deps)

			if tc.wantUpgrade {
				require.NotNil(t, stub)
				assert.Eventually(t, stub.upgradeCalledNow, time.Second, 5*time.Millisecond, "Upgrade must be called")
			}
			if tc.wantForce {
				require.NotNil(t, stub)
				assert.True(t, stub.upgradeForce(), "Force must propagate to Upgrade")
			}
			if tc.wantRelaunch {
				require.NotNil(t, stub)
				assert.Eventually(t, stub.relaunchCalledNow, time.Second, 5*time.Millisecond, "Relaunch must be invoked after the response flushes")
			} else if stub != nil {
				assert.Never(t, stub.relaunchCalledNow, 100*time.Millisecond, 10*time.Millisecond, "Relaunch must not be invoked for non-relaunching paths")
			}

			if tc.wantRelaunch {
				var resp upgradeResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Equal(t, "relaunching", resp.Status)
				assert.Equal(t, "1.2.3", resp.Version)
			}
		})
	}
}

func TestUpgrade_InvalidBodyReturns400(t *testing.T) {
	deps := newDesktopDeps(&stubUpdater{status: updater.Status{State: updater.StateIdle}})
	rec := performUpgradeRequest(t, deps, "{not valid json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid request body")
}

func extractStub(deps commandutil.CoreDepsReader) (*stubUpdater, bool) {
	sd, ok := deps.(*stubDeps)
	if !ok {
		return nil, false
	}
	su, ok := sd.updater.(*stubUpdater)
	return su, ok
}

func TestUpgrade_EmptyBodyDefaultsForceFalse(t *testing.T) {
	stub := &stubUpdater{
		status:        updater.Status{State: updater.StateIdle},
		upgradeResult: &updater.UpgradeResult{LatestVersion: "2.0.0"},
	}
	rec := performUpgradeRequest(t, newDesktopDeps(stub), "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Eventually(t, stub.upgradeCalledNow, time.Second, 5*time.Millisecond)
	assert.False(t, stub.upgradeForce(), "empty body must default force to false")
}

func TestUpgrade_ErrAlreadyInProgressReturns409(t *testing.T) {
	stub := &stubUpdater{
		status:     updater.Status{State: updater.StateIdle},
		upgradeErr: updater.ErrAlreadyInProgress,
	}
	rec := performUpgradeRequest(t, newDesktopDeps(stub), `{}`)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "already in progress")
}

func TestUpgradeStatus_Handler(t *testing.T) {
	t.Run("404 non-desktop", func(t *testing.T) {
		deps := &stubDeps{env: system.EnvironmentCLI, updater: &stubUpdater{}}
		router := gin.New()
		router.GET("/desktop/upgrade/status", upgradeStatus(deps))

		req := httptest.NewRequest(http.MethodGet, "/desktop/upgrade/status", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("404 no updater", func(t *testing.T) {
		deps := &stubDeps{env: system.EnvironmentDesktop, updater: nil}
		router := gin.New()
		router.GET("/desktop/upgrade/status", upgradeStatus(deps))

		req := httptest.NewRequest(http.MethodGet, "/desktop/upgrade/status", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("200 returns current status", func(t *testing.T) {
		stub := &stubUpdater{status: updater.Status{State: updater.StateRelaunching, Version: "1.2.3"}}
		deps := newDesktopDeps(stub)
		router := gin.New()
		router.GET("/desktop/upgrade/status", upgradeStatus(deps))

		req := httptest.NewRequest(http.MethodGet, "/desktop/upgrade/status", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp updater.Status
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, updater.StateRelaunching, resp.State)
		assert.Equal(t, "1.2.3", resp.Version)
	})
}
