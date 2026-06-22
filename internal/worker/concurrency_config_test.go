package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewConcurrencyConfig_AppliesDefaults(t *testing.T) {
	cc := newConcurrencyConfig(0, 0, defaultMaxWorkers, defaultWorkerTimeout)
	assert.Equal(t, defaultMaxWorkers, cc.MaxWorkers, "zero MaxWorkers should default to defaultMaxWorkers")
	assert.Equal(t, defaultWorkerTimeout, cc.WorkerTimeout, "zero WorkerTimeout should default to defaultWorkerTimeout")
}

func TestNewConcurrencyConfig_PreservesPositiveValues(t *testing.T) {
	cc := newConcurrencyConfig(3, 10*time.Second, defaultMaxWorkers, defaultWorkerTimeout)
	assert.Equal(t, 3, cc.MaxWorkers, "positive MaxWorkers should be preserved")
	assert.Equal(t, 10*time.Second, cc.WorkerTimeout, "positive WorkerTimeout should be preserved")
}

func TestNewConcurrencyConfig_NegativeValuesGetDefaults(t *testing.T) {
	cc := newConcurrencyConfig(-1, -1, defaultMaxWorkers, defaultWorkerTimeout)
	assert.Equal(t, defaultMaxWorkers, cc.MaxWorkers, "negative MaxWorkers should default to defaultMaxWorkers")
	assert.Equal(t, defaultWorkerTimeout, cc.WorkerTimeout, "negative WorkerTimeout should default to defaultWorkerTimeout")
}

func TestNewConcurrencyConfig_ApplyPhaseDefault(t *testing.T) {
	// Apply phase uses defaultMaxWorkers=1 (I/O-bound file operations)
	cc := newConcurrencyConfig(0, 0, 1, defaultWorkerTimeout)
	assert.Equal(t, 1, cc.MaxWorkers, "apply phase should default MaxWorkers to 1")
	assert.Equal(t, defaultWorkerTimeout, cc.WorkerTimeout, "apply phase should use defaultWorkerTimeout")
}

func TestNewConcurrencyConfig_CustomDefaults(t *testing.T) {
	cc := newConcurrencyConfig(0, 0, 10, 30*time.Second)
	assert.Equal(t, 10, cc.MaxWorkers, "should use custom default MaxWorkers")
	assert.Equal(t, 30*time.Second, cc.WorkerTimeout, "should use custom default WorkerTimeout")
}
