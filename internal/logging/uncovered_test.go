package logging

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitLogger_StderrOutputUncovered(t *testing.T) {
	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: "stderr",
	}
	err := InitLogger(cfg)
	assert.NoError(t, err)
}

func TestInitLogger_EmptyOutputDefaultsToStdoutUncovered(t *testing.T) {
	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: "",
	}
	err := InitLogger(cfg)
	assert.Error(t, err, "empty output should return error, not silently default to stdout")
	assert.Contains(t, err.Error(), "no valid log outputs")
}

func TestInitLogger_MultipleCommaOutputsUncovered(t *testing.T) {
	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: "stdout,stderr",
	}
	err := InitLogger(cfg)
	assert.NoError(t, err)
}

func TestInitLogger_FileWithRotationUncovered(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		Level:      "info",
		Format:     "text",
		Output:     tmpDir + "/rotated.log",
		MaxSizeMB:  1,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Compress:   true,
	}
	err := InitLogger(cfg)
	assert.NoError(t, err)
	defer closeLogger()
}

func TestInitLogger_ConfigNilDefaultsUncovered(t *testing.T) {
	err := InitLogger(nil)
	assert.NoError(t, err)
}

func TestGetFileOutputs_MultipleFilesUncovered(t *testing.T) {
	result := GetFileOutputs("stdout,/var/log/a.log,/var/log/b.log")
	assert.Equal(t, []string{"/var/log/a.log", "/var/log/b.log"}, result)
}

func TestGetFileOutputs_OnlyStderrUncovered(t *testing.T) {
	result := GetFileOutputs("stderr")
	assert.Nil(t, result)
}

func TestGetFileOutputs_CommaWithEmptySegmentsUncovered(t *testing.T) {
	result := GetFileOutputs("stdout,,stderr,")
	assert.Nil(t, result)
}
