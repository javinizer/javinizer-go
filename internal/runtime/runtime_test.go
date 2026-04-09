package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataDir_Default(t *testing.T) {
	// Clear any existing env var
	origValue := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", origValue)

	os.Unsetenv("JAVINIZER_DATA_DIR")

	result := DataDir()

	assert.Equal(t, "data", result, "Should return default 'data' when env var not set")
}

func TestDataDir_EnvOverride(t *testing.T) {
	origValue := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", origValue)

	expectedDir := "/custom/data/path"
	os.Setenv("JAVINIZER_DATA_DIR", expectedDir)

	result := DataDir()

	assert.Equal(t, expectedDir, result, "Should return env var value when set")
}

func TestDataDir_EmptyEnv(t *testing.T) {
	origValue := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", origValue)

	os.Setenv("JAVINIZER_DATA_DIR", "")

	result := DataDir()

	assert.Equal(t, "data", result, "Should return default when env var is empty string")
}

func TestUpdateStatePath_Default(t *testing.T) {
	origValue := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", origValue)

	os.Unsetenv("JAVINIZER_DATA_DIR")

	result := UpdateStatePath()

	expected := filepath.Join("data", "update_cache.json")
	assert.Equal(t, expected, result, "Should return correct path with default data dir")
}

func TestUpdateStatePath_CustomDataDir(t *testing.T) {
	origValue := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", origValue)

	customDir := "/var/lib/javinizer"
	os.Setenv("JAVINIZER_DATA_DIR", customDir)

	result := UpdateStatePath()

	expected := filepath.Join(customDir, "update_cache.json")
	assert.Equal(t, expected, result, "Should return correct path with custom data dir")
}

func TestUpdateStatePath_RelativeDataDir(t *testing.T) {
	origValue := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", origValue)

	relativeDir := "./mydata"
	os.Setenv("JAVINIZER_DATA_DIR", relativeDir)

	result := UpdateStatePath()

	expected := filepath.Join(relativeDir, "update_cache.json")
	assert.Equal(t, expected, result, "Should handle relative paths correctly")
}

func TestUpdateStatePath_UsesDataDir(t *testing.T) {
	origValue := os.Getenv("JAVINIZER_DATA_DIR")
	defer os.Setenv("JAVINIZER_DATA_DIR", origValue)

	testCases := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "no env set",
			envValue: "",
			expected: filepath.Join("data", "update_cache.json"),
		},
		{
			name:     "custom path",
			envValue: "/opt/javinizer/data",
			expected: filepath.Join("/opt/javinizer/data", "update_cache.json"),
		},
		{
			name:     "relative path",
			envValue: "localdata",
			expected: filepath.Join("localdata", "update_cache.json"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envValue == "" {
				os.Unsetenv("JAVINIZER_DATA_DIR")
			} else {
				os.Setenv("JAVINIZER_DATA_DIR", tc.envValue)
			}

			result := UpdateStatePath()
			assert.Equal(t, tc.expected, result)
		})
	}
}
