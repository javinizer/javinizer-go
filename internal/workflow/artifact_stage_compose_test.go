package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDockerComposeStagingIdentity(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "docker-compose.yml"))
	require.NoError(t, err)
	var compose struct {
		Services map[string]struct {
			Hostname    string   `yaml:"hostname"`
			Environment []string `yaml:"environment"`
			Volumes     []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(body, &compose))
	for name, hostname := range map[string]string{"javinizer": "javinizer", "javinizer-dev": "javinizer-dev"} {
		t.Run(name, func(t *testing.T) {
			service, ok := compose.Services[name]
			require.True(t, ok)
			assert.Equal(t, hostname, service.Hostname)
			assert.Contains(t, service.Environment, "JAVINIZER_HOME=/javinizer")
			assert.Contains(t, service.Volumes, "./data:/javinizer")
		})
	}
}
