package file

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
)

func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *core.ServerDependencies {
	return testkit.CreateTestDeps(t, cfg, configFile)
}
