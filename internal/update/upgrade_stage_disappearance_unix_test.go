//go:build darwin || linux

package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type upgradeVerificationWriter func([]byte) (int, error)

func (write upgradeVerificationWriter) Write(p []byte) (int, error) { return write(p) }

func TestUpgradeVerifiedStageDisappearancePreservesInstalledBinary(t *testing.T) {
	// The global logger is restored below; this test must not run in parallel.
	dir := t.TempDir()
	target := filepath.Join(dir, "javinizer")
	original := []byte("previous executable")
	require.NoError(t, os.WriteFile(target, original, 0o700))
	before, err := os.Stat(target)
	require.NoError(t, err)
	asset := "fixture-asset"
	download := []byte("verified replacement executable")
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(download), asset)
	var requests []string
	client := &http.Client{Transport: edgeTransport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.Path)
		body := download
		if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
			body = []byte(checksum)
		} else {
			require.True(t, strings.HasSuffix(r.URL.Path, "/"+asset))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}

	logger := logging.WithField("test", t.Name()).Logger
	previousLevel := logger.GetLevel()
	logger.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() { logger.SetLevel(previousLevel) })
	removedStage := ""
	restoreOutput := logging.SetOutput(upgradeVerificationWriter(func(p []byte) (int, error) {
		if !bytes.Contains(p, []byte("Checksum verified for ")) {
			return len(p), nil
		}
		// The synchronous verification diagnostic is an existing boundary after
		// hashing and before chmod. Remove the real, regular staged file here
		// to model concurrent filesystem cleanup without sleeps or fake files.
		require.Empty(t, removedStage, "only one asset should be verified")
		stages, err := filepath.Glob(filepath.Join(dir, ".javinizer-upgrade-*.tmp"))
		require.NoError(t, err)
		require.Len(t, stages, 1)
		stageInfo, err := os.Lstat(stages[0])
		require.NoError(t, err)
		require.True(t, stageInfo.Mode().IsRegular())
		staged, err := os.ReadFile(stages[0])
		require.NoError(t, err)
		require.Equal(t, download, staged)
		require.NoError(t, os.Remove(stages[0]))
		removedStage = stages[0]
		return len(p), nil
	}))
	t.Cleanup(restoreOutput)

	var out bytes.Buffer
	err = downloadAndReplace(context.Background(), UpgradeOptions{
		Repo: "fixture/repo", DownloadBaseURL: "https://offline.invalid", HTTPClient: client, Out: &out,
	}, target, "v99.0.0", asset)
	require.ErrorContains(t, err, "chmod asset")
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NotEmpty(t, removedStage, "the verified-stage boundary must be exercised")
	require.Equal(t, []string{
		"/fixture/repo/releases/download/v99.0.0/checksums.txt",
		"/fixture/repo/releases/download/v99.0.0/" + asset,
	}, requests)
	require.NotContains(t, out.String(), "Installing...")
	after, err := os.Stat(target)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "the installed inode must remain untouched")
	require.Equal(t, before.Mode(), after.Mode())
	retained, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, original, retained)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "failure must leave no staging file or backup")
	require.Equal(t, filepath.Base(target), entries[0].Name())
}
