package update

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallUpgradeRejectsUnsupportedTargetBeforeDiscovery(t *testing.T) {
	opts, out := upgradeTargetTestOptions(t)
	result, err := installUpgrade(context.Background(), opts, &UpgradeResult{},
		&versionInfo{Version: "v2.0.0"}, upgradeTarget{
			goos: "freebsd", goarch: "amd64",
			locateExecutable: func(override string) (string, error) {
				t.Fatal("unsupported release target must not discover the executable")
				return "", nil
			},
		})
	require.EqualError(t, err, "no release asset for freebsd/amd64")
	require.Nil(t, result)
	require.Empty(t, out.String())
}

func TestInstallUpgradeReportsExecutableDiscoveryFailure(t *testing.T) {
	opts, out := upgradeTargetTestOptions(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "executable-link")
	target := filepath.Join(dir, "javinizer")
	original := []byte("installed binary")
	require.NoError(t, os.WriteFile(target, original, 0600))
	var lookupErr error
	lookups := 0
	result, err := installUpgrade(context.Background(), opts, &UpgradeResult{},
		&versionInfo{Version: "v2.0.0"}, upgradeTarget{
			goos: "linux", goarch: "amd64",
			locateExecutable: func(override string) (string, error) {
				return resolveExePathWithExecutable(override, func() (string, error) {
					lookups++
					// Mirror the empty path returned by failed executable discovery.
					_, lookupErr = os.Lstat(missing)
					return "", lookupErr
				})
			},
		})
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.ErrorIs(t, err, lookupErr)
	require.EqualError(t, err, "locate running binary: "+lookupErr.Error())
	require.Nil(t, result)
	require.Equal(t, 1, lookups)
	require.Empty(t, out.String())
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, original, got)
}

func upgradeTargetTestOptions(t *testing.T) (UpgradeOptions, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	return resolveUpgradeDefaults(UpgradeOptions{
		CurrentVersion: "v1.0.0",
		Out:            out,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("install must not start a download when its local target cannot be selected")
			return nil, nil
		})},
	}), out
}
