package system

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
)

func TestConfigSvcFinal_ActressFieldScraperNotFound(t *testing.T) {
	reg := scraperutil.NewScraperRegistry()
	reg.RegisterInstance(&mockScraperNoMovieSys{})
	deps := &core.APIDeps{CoreDeps: &commandutil.CoreDeps{ScraperRegistry: reg}}
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Priority.Fields = map[string][]string{"actress": {"dmm", "nonexistent"}}
	err := validatePriorityFieldCapabilities(deps, cfg)
	assert.NoError(t, err)
}
