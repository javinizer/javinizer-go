package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: web/API organize jobs must NOT default to authorized overwrites
// (ForceUpdate). The pre-fix default silently replaced existing destination files.
func TestResolveOrganizeApplyConfig_ForceUpdateDefaultsFalse(t *testing.T) {
	rt := core.NewAPIRuntime(nil)
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	cfg, err := resolveOrganizeApplyConfig(core.NewSnapshotForTesting(rt, core.APIConfig{}), factory, &stubControlledJob{}, contracts.OrganizeRequest{OperationMode: "in-place"})
	require.NoError(t, err)
	assert.False(t, cfg.OrganizeOptions.ForceUpdate, "organize jobs must not authorize overwrite by default")
}
