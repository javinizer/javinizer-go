package core

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskConfigSnapshot_NotSetReturnsNil(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{})
	assert.Nil(t, rt.DiskConfigSnapshot())
}

func TestDiskConfigSnapshot_AfterSetInitialConfigsReturnsClone(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{})
	cfg := config.DefaultConfig(nil, nil)
	rt.SetInitialConfigs(cfg, cfg.Clone())

	snap := rt.DiskConfigSnapshot()
	require.NotNil(t, snap)
	assert.Equal(t, cfg.ConfigVersion, snap.ConfigVersion)

	// Mutating the returned snapshot must not affect the stored one.
	snap.ConfigVersion = 9999
	snap2 := rt.DiskConfigSnapshot()
	require.NotNil(t, snap2)
	assert.NotEqual(t, 9999, snap2.ConfigVersion)
}

func TestSetInitialConfigs_NilDiskCfgSetsSnapshotNil(t *testing.T) {
	rt := NewAPIRuntime(&APIDeps{})
	rt.SetInitialConfigs(nil, nil)
	assert.Nil(t, rt.DiskConfigSnapshot())
}

func TestSetInitialConfigs_NilDepsSkipsConfigPublish(t *testing.T) {
	deps := &APIDeps{}
	rt := NewAPIRuntime(deps)
	disk := config.DefaultConfig(nil, nil)
	rt.SetInitialConfigs(nil, disk)
	snap := rt.DiskConfigSnapshot()
	require.NotNil(t, snap)
}
