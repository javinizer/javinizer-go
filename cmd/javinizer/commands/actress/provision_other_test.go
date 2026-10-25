//go:build !darwin && !linux

package actress

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvisionUnsupportedBeforeOpeningInput(t *testing.T) {
	options := validProvisionOptions()
	options.artifactDir = t.TempDir()
	options.dumpPath = filepath.Join(t.TempDir(), "not-present.sql")
	var output bytes.Buffer
	err := runProvisionCache(t.Context(), &output, options)
	if err == nil || !strings.Contains(err.Error(), "supported only on Linux and macOS") || output.Len() != 0 {
		t.Fatalf("writer did not fail closed before source access: %v %s", err, output.String())
	}
	if file, err := openProvisionInput(options.dumpPath, 100); err == nil || file != nil {
		t.Fatal("unsupported platform opened dump")
	}
	entries, err := os.ReadDir(options.artifactDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsupported writer changed store: %v %v", entries, err)
	}
}
