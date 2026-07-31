package batch

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveUpdateApplyConfig_MapsOverwriteExistingMedia(t *testing.T) {
	rt := core.NewAPIRuntime(nil)
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	applyCfg, err := resolveUpdateApplyConfig(core.NewSnapshotForTesting(rt, core.APIConfig{}), factory, &stubControlledJob{}, contracts.UpdateRequest{
		OverwriteExistingMedia: true,
	})
	require.NoError(t, err)
	assert.True(t, applyCfg.OverwriteExistingMedia)

	defaultCfg, err := resolveUpdateApplyConfig(core.NewSnapshotForTesting(rt, core.APIConfig{}), factory, &stubControlledJob{}, contracts.UpdateRequest{})
	require.NoError(t, err)
	assert.False(t, defaultCfg.OverwriteExistingMedia)
}

func TestResolveOrganizeApplyConfig_IgnoresOverwriteExistingMediaJSON(t *testing.T) {
	var req contracts.OrganizeRequest
	require.NoError(t, json.Unmarshal([]byte(`{"operation_mode":"in-place","overwrite_existing_media":true}`), &req))
	assert.Equal(t, string(operationmode.OperationModeInPlace), req.OperationMode)

	rt := core.NewAPIRuntime(nil)
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	applyCfg, err := resolveOrganizeApplyConfig(core.NewSnapshotForTesting(rt, core.APIConfig{}), factory, &stubControlledJob{}, req)
	require.NoError(t, err)
	assert.False(t, applyCfg.OverwriteExistingMedia)
}
