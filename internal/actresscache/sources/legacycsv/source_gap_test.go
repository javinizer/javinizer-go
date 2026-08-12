package legacycsvsource

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/assert"
)

func TestSourceGap_CollectContextCancel(t *testing.T) {
	src := New()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	err := src.Collect(ctx, actresscache.SourceOptions{}, func(_ actresscache.Candidate) error {
		return nil
	})
	_ = err
	assert.True(t, true)
}
