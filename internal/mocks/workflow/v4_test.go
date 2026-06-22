package workflowmocks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockWorkflowInterfaceV4(t *testing.T) {
	m := NewMockWorkflowInterface(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
	assert.NotNil(t, m)
}
