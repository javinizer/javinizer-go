package dmm

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestDisplayIdentityTuple_Branches(t *testing.T) {
	s, d, _, m, ok := displayIdentityTuple("rct00156h")
	assert.True(t, ok)
	assert.Equal(t, "rct", s)
	assert.Equal(t, "156", d)
	assert.Equal(t, "h", m)
	_, _, _, m2, ok2 := displayIdentityTuple("dv00899ai")
	assert.True(t, ok2)
	assert.Equal(t, "ai", m2)
	_, _, _, m3, _ := displayIdentityTuple("rct156hd")
	assert.Equal(t, "h", m3)
	_, _, _, _, ok4 := displayIdentityTuple("garbage")
	assert.False(t, ok4)
}
