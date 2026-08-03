package sources

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAllProvidesDeterministicSources(t *testing.T) {
	registry := actresscache.NewRegistry()
	RegisterAll(registry)
	assert.Equal(t, []string{"legacy-jvthumbs", "minnanoav", "r18dev"}, registry.Names())
	minnano, ok := registry.Create("minnanoav")
	require.True(t, ok)
	assert.Equal(t, "minnanoav", minnano.Name())
	_, ok = registry.Create("missing")
	assert.False(t, ok)
}
