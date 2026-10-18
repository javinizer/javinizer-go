package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type proofCleanupUnlinkFailureFS struct {
	afero.Fs
	denied   bool
	attempts int
}

func (f *proofCleanupUnlinkFailureFS) Remove(path string) error {
	if f.denied && strings.HasSuffix(path, artifactStageProofSuffix) && strings.Contains(path, artifactStageQuarantineMark) {
		if _, err := f.Fs.Stat(path); err == nil {
			f.attempts++
			return os.ErrPermission
		}
	}
	return f.Fs.Remove(path)
}
func (f *proofCleanupUnlinkFailureFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

func TestArtifactProofCleanupFailureIsWarned(t *testing.T) {
	for _, lane := range []string{"cleanup", "sweep"} {
		t.Run(lane, func(t *testing.T) {
			parent := t.TempDir()
			base := afero.NewOsFs()
			root := filepath.Join(parent, artifactStageDirPrefix+"synthetic")
			require.NoError(t, base.MkdirAll(root, 0755))
			writeArtifactStageManifest(base, root)
			logPath := filepath.Join(parent, "cleanup.log")
			require.NoError(t, logging.InitLogger(&logging.Config{Level: "warn", Format: "json", Output: logPath}))
			defer logging.CloseLogger()
			fs := &proofCleanupUnlinkFailureFS{Fs: base, denied: true}
			if lane == "cleanup" {
				(&artifactStage{fs: fs, root: root}).cleanup()
			} else {
				markArtifactStageCompleted(base, root)
				sweepArtifactStaging(fs, parent)
			}
			require.Equal(t, 1, fs.attempts)
			entries, err := afero.ReadDir(base, parent)
			require.NoError(t, err)
			var proof string
			for _, entry := range entries {
				require.False(t, entry.IsDir())
				if strings.HasSuffix(entry.Name(), artifactStageProofSuffix) {
					proof = filepath.Join(parent, entry.Name())
				}
			}
			require.NotEmpty(t, proof)
			fs.denied = false
			sweepArtifactStaging(fs, parent)
			require.FileExists(t, proof)
			logging.CloseLogger()
			body, err := os.ReadFile(logPath)
			require.NoError(t, err)
			t.Logf("lane=%s proof_remove_attempts=%d residue=%s logfile=%s", lane, fs.attempts, proof, string(body))
			var entry proofCleanupLogEntry
			require.NoError(t, json.Unmarshal(body, &entry))
			require.Equal(t, "warning", entry.Level)
			require.Equal(t, fmt.Sprintf("artifact staging proof cleanup retained %s (staging tree removed): %v; preserve this sidecar for inspection", proof, os.ErrPermission), entry.Message)
		})
	}
}

// Decode the structured message before comparing filesystem paths: log
// formatters escape Windows backslashes in their serialized output.
type proofCleanupLogEntry struct {
	Level   string `json:"level"`
	Message string `json:"msg"`
}

func TestArtifactProofCleanupLogPathSerialization(t *testing.T) {
	for _, path := range []string{
		"/synthetic/sorting/.javinizer-apply-fixture.rm.token.proof",
		`C:\synthetic\sorting\.javinizer-apply-fixture.rm.token.proof`,
	} {
		t.Run(path, func(t *testing.T) {
			message := fmt.Sprintf("artifact staging proof cleanup retained %s (staging tree removed): %v; preserve this sidecar for inspection", path, os.ErrPermission)
			logger := logrus.New()
			var output bytes.Buffer
			logger.SetOutput(&output)
			for _, formatter := range []logrus.Formatter{&logrus.TextFormatter{DisableTimestamp: true}, &logrus.JSONFormatter{DisableTimestamp: true}} {
				output.Reset()
				logger.SetFormatter(formatter)
				logger.Warn(message)
				if strings.Contains(path, `\`) {
					require.NotContains(t, output.String(), path, "raw Windows path must reproduce the serialized-path mismatch")
				}
			}
			var entry proofCleanupLogEntry
			require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
			require.Equal(t, "warning", entry.Level)
			require.Equal(t, message, entry.Message)
		})
	}
}
