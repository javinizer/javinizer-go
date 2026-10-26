package update

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type edgeTransport func(*http.Request) (*http.Response, error)

func (f edgeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type edgeReadFailure struct{}

func (edgeReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (edgeReadFailure) Close() error             { return nil }

func TestOfflineCheckerMalformedEndpointNeverRequests(t *testing.T) {
	calls := 0
	c := newGitHubCheckerWithBaseURL("owner/repo", "https://%invalid")
	c.httpClient = &http.Client{Transport: edgeTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, os.ErrPermission })}
	_, err := c.CheckLatestVersion(context.Background())
	require.ErrorContains(t, err, "failed to create request")
	_, _, err = c.getRecentReleases(context.Background(), 10)
	require.ErrorContains(t, err, "failed to create request")
	require.Zero(t, calls)
}

func TestOfflineRecentReleasesRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   io.ReadCloser
		want   string
	}{
		{"unexpected status", http.StatusBadGateway, io.NopCloser(strings.NewReader("upstream failure")), "status 502"},
		{"read failure", http.StatusOK, edgeReadFailure{}, "failed to read response"},
		{"invalid JSON", http.StatusOK, io.NopCloser(strings.NewReader("[")), "failed to parse response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newGitHubChecker("owner/repo")
			calls := 0
			c.httpClient = &http.Client{Transport: edgeTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "/repos/owner/repo/releases", r.URL.Path)
				return &http.Response{StatusCode: tc.status, Body: tc.body, Header: make(http.Header)}, nil
			})}
			versions, etag, err := c.getRecentReleases(context.Background(), 10)
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, versions)
			require.Empty(t, etag)
			require.Equal(t, 1, calls)
		})
	}
}

type edgeChecker func(context.Context) (*versionInfo, error)

func (f edgeChecker) CheckLatestVersion(ctx context.Context) (*versionInfo, error) { return f(ctx) }

func TestOfflineFreshCheckPreservesCacheWhenPublicationFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewServiceWithOptions(UpdateConfig{Enabled: true}, ServiceOptions{StatePath: path, Checker: edgeChecker(func(context.Context) (*versionInfo, error) { return &versionInfo{Version: "v99.0.0"}, nil })})
	old := &updateState{Version: "v1.0.0", Source: UpdateSourceCached}
	require.NoError(t, s.store.SaveState(old))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(path+".tmp", 0700))
	got, err := s.ForceCheck(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v99.0.0", got.Version)
	require.Equal(t, UpdateSourceFresh, got.Source)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, old, s.store.GetState())
}

func TestOfflineStateRenameFailureCleansStageAndPreservesTarget(t *testing.T) {
	for _, helper := range []bool{false, true} {
		t.Run(map[bool]string{false: "store", true: "helper"}[helper], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			require.NoError(t, os.Mkdir(path, 0700))
			sentinel := filepath.Join(path, "keep")
			require.NoError(t, os.WriteFile(sentinel, []byte("original"), 0600))
			state := &updateState{Version: "v2.0.0"}
			store := newStateStore(path, defaultCheckInterval)
			var err error
			if helper {
				err = saveStateToFile(afero.NewOsFs(), path, state)
			} else {
				err = store.SaveState(state)
			}
			require.Error(t, err)
			require.NoFileExists(t, path+".tmp")
			require.Nil(t, store.GetState())
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "original", string(data))
		})
	}
}

func TestOfflineBackgroundPanicClosesJoin(t *testing.T) {
	s := NewServiceWithOptions(UpdateConfig{Enabled: true}, ServiceOptions{StatePath: filepath.Join(t.TempDir(), "state.json"), Checker: edgeChecker(func(context.Context) (*versionInfo, error) { panic("offline checker failure") })})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := s.StartBackgroundCheck(ctx, time.Nanosecond)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("panic recovery did not close join")
	}
	require.NoFileExists(t, s.statePath)
}

func TestOfflineBackgroundCancellationJoinsWithoutCache(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	s := NewServiceWithOptions(UpdateConfig{Enabled: true}, ServiceOptions{StatePath: filepath.Join(t.TempDir(), "state.json"), Checker: edgeChecker(func(ctx context.Context) (*versionInfo, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() })})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { defer close(finished); s.BackgroundCheck(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("checker never entered")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled check did not finish")
	}
	require.NoFileExists(t, s.statePath)
}

func TestOfflineBackgroundNonpositiveIntervalCanStop(t *testing.T) {
	s := NewServiceWithOptions(UpdateConfig{Enabled: true}, ServiceOptions{StatePath: filepath.Join(t.TempDir(), "state.json"), Checker: edgeChecker(func(context.Context) (*versionInfo, error) {
		t.Error("normalized interval unexpectedly checked")
		return nil, os.ErrPermission
	})})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case <-s.StartBackgroundCheck(ctx, 0):
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled ticker did not stop")
	}
}

func TestOfflineUpgradeStagingFailureNeverDownloadsAsset(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(parent, []byte("preserve parent"), 0600))
	calls := 0
	client := &http.Client{Transport: edgeTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		require.True(t, strings.HasSuffix(r.URL.Path, "/checksums.txt"), "must stop before requesting executable bytes")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("0", 64) + "  fixture-asset\n"))}, nil
	})}
	err := downloadAndReplace(context.Background(), UpgradeOptions{Repo: "fixture/repo", DownloadBaseURL: "https://offline.invalid", HTTPClient: client, Out: io.Discard}, filepath.Join(parent, "javinizer"), "v99.0.0", "fixture-asset")
	require.ErrorContains(t, err, "create temp file")
	require.Equal(t, 1, calls)
	data, err := os.ReadFile(parent)
	require.NoError(t, err)
	require.Equal(t, "preserve parent", string(data))
}
