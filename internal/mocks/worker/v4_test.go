package workermocks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockControlledJobV4(t *testing.T) {
	m := NewMockControlledJob(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockEditableJobV4(t *testing.T) {
	m := NewMockEditableJob(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}
