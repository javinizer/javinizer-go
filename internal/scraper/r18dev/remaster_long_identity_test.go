package r18dev

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLongRawIdentity(t *testing.T) {
	for _, id := range []string{"5750360vrg00123h", "5755360vrpg00123hd", "5750360vrg00123ai", "h_5750360vrg00123hd"} {
		marker, series := classifyRemaster(id)
		require.NotEmpty(t, marker)
		assert.True(t, isRawRemasterContentIDQuery(id))
		assert.True(t, cidMatchesRemasterQuery(id, id, marker, series))
		assert.False(t, cidMatchesRemasterQuery("1"+id, id, marker, series))
	}
}
