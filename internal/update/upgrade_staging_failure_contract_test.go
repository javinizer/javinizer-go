package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
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

// stagingFailureFile retains the real file for writes, descriptor ownership,
// hashing and removal. Only the selected persistence operation reports failure.
type stagingFailureFile struct {
	*os.File
	syncErr, closeErr error
	events            *[]string
	syncContents      []byte
	syncReadErr       error
}

func (f *stagingFailureFile) Sync() error {
	*f.events = append(*f.events, "sync")
	f.syncContents, f.syncReadErr = os.ReadFile(f.Name())
	if f.syncErr != nil {
		return &os.PathError{Op: "sync", Path: f.Name(), Err: f.syncErr}
	}
	return f.File.Sync()
}

func (f *stagingFailureFile) Close() error {
	*f.events = append(*f.events, "close")
	if err := f.File.Close(); err != nil {
		return err
	}
	if f.closeErr != nil {
		return &os.PathError{Op: "close", Path: f.Name(), Err: f.closeErr}
	}
	return nil
}

func TestUpgradeStagingFailuresPreserveInstalledBinary(t *testing.T) {
	// The debug logger is shared; these cases must not run in parallel.
	logger := logging.WithField("test", t.Name()).Logger
	level := logger.GetLevel()
	logger.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() { logger.SetLevel(level) })

	syncFailure := errors.New("fixture flush rejected")
	closeFailure := errors.New("fixture close rejected")
	for _, tc := range []struct {
		name              string
		syncErr, closeErr error
		wantErr           error
		wantMessage       string
		wantEvents        []string
		wantCloseLog      bool
	}{
		{
			name: "flush fails", syncErr: syncFailure, wantErr: syncFailure,
			wantMessage: "flush asset", wantEvents: []string{"create", "download", "sync", "close"},
		},
		{
			name: "explicit close fails", closeErr: closeFailure, wantErr: closeFailure,
			wantMessage: "close asset", wantEvents: []string{"create", "download", "sync", "close", "close"},
		},
		{
			name: "cleanup close does not mask flush failure", syncErr: syncFailure, closeErr: closeFailure,
			wantErr: syncFailure, wantMessage: "flush asset", wantCloseLog: true,
			wantEvents: []string{"create", "download", "sync", "close"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "javinizer")
			original := []byte("previous executable")
			require.NoError(t, os.WriteFile(target, original, 0o700))
			before, err := os.Stat(target)
			require.NoError(t, err)
			payload := []byte("verified replacement executable")
			asset := "fixture-asset"
			checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(payload), asset)
			var events, requests []string
			var stage *stagingFailureFile
			client := &http.Client{Transport: edgeTransport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.Path)
				body := []byte(checksum)
				if strings.HasSuffix(r.URL.Path, "/"+asset) {
					require.NotNil(t, stage)
					events = append(events, "download")
					body = payload
				} else {
					require.Equal(t, "/fixture/repo/releases/download/v99.0.0/checksums.txt", r.URL.Path)
					require.Nil(t, stage, "checksums must be fetched before creating the stage")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
			})}
			var out, log bytes.Buffer
			restoreOutput := logging.SetOutput(&log)
			t.Cleanup(restoreOutput)
			err = downloadAndReplaceWithStagingFile(context.Background(), UpgradeOptions{
				Repo: "fixture/repo", DownloadBaseURL: "https://offline.invalid", HTTPClient: client, Out: &out,
			}, target, "v99.0.0", asset, func(stageDir, pattern string) (upgradeStagingFile, error) {
				require.Equal(t, dir, stageDir, "atomic publication requires the target filesystem")
				require.Equal(t, ".javinizer-upgrade-*.tmp", pattern)
				require.Nil(t, stage, "only one staging resource may be allocated")
				file, createErr := os.CreateTemp(stageDir, pattern)
				require.NoError(t, createErr)
				stage = &stagingFailureFile{File: file, syncErr: tc.syncErr, closeErr: tc.closeErr, events: &events}
				t.Cleanup(func() { _ = file.Close() })
				events = append(events, "create")
				return stage, nil
			})
			require.ErrorIs(t, err, tc.wantErr)
			require.ErrorContains(t, err, tc.wantMessage)
			var fileErr *os.PathError
			require.ErrorAs(t, err, &fileErr)
			require.Equal(t, stage.Name(), fileErr.Path)
			require.Equal(t, tc.wantEvents, events)
			require.NoError(t, stage.syncReadErr)
			require.Equal(t, payload, stage.syncContents, "flush must run only after the full payload has been written")
			require.Equal(t, []string{
				"/fixture/repo/releases/download/v99.0.0/checksums.txt",
				"/fixture/repo/releases/download/v99.0.0/" + asset,
			}, requests)
			if tc.wantCloseLog {
				require.Contains(t, log.String(), "closing temp upgrade file:")
				require.Contains(t, log.String(), closeFailure.Error())
				require.False(t, errors.Is(err, closeFailure), "cleanup must not replace the primary failure")
			} else {
				require.NotContains(t, log.String(), "closing temp upgrade file:", "an already-closed file must not produce a cleanup error")
			}
			require.NotContains(t, log.String(), "Checksum verified", "a non-durable stage must not advance to verification")
			require.NotContains(t, out.String(), "Installing...")
			_, err = stage.File.Write([]byte("closed"))
			require.ErrorIs(t, err, os.ErrClosed, "failure must release the real staging descriptor")
			require.NoFileExists(t, stage.Name())
			after, err := os.Stat(target)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after), "failure must not replace the installed inode")
			require.Equal(t, before.Mode(), after.Mode())
			retained, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, original, retained)
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "failure must leave neither a stage nor a backup")
			require.Equal(t, filepath.Base(target), entries[0].Name())
		})
	}
}
