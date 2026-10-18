package fsutil

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProbeProcessLiveness_Mapping(t *testing.T) {
	old := replacementProbePIDAliveAware
	defer func() { replacementProbePIDAliveAware = old }()
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDAlive }
	assert.Equal(t, ProcessAlive, ProbeProcessLiveness(123))
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDDead }
	assert.Equal(t, ProcessDead, ProbeProcessLiveness(123))
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDUnprobeable }
	assert.Equal(t, ProcessUnknown, ProbeProcessLiveness(123))
}

func TestProbeProcess_WrappersAgainstSelf(t *testing.T) {
	assert.Equal(t, ProcessAlive, ProbeProcessLiveness(os.Getpid()))
	assert.NotPanics(t, func() { ProbeProcessStartTime(os.Getpid()) })
}
