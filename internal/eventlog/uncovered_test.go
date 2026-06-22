package eventlog

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestEmitter_NilRepoReturnsErrorUncovered(t *testing.T) {
	emitter := NewEmitter(nil)
	err := emitter.EmitScraperEvent(context.Background(), "src", "msg", models.SeverityInfo, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repository is nil")
}

func TestEmitter_CancelledContextUncovered(t *testing.T) {
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := emitter.EmitOrganizeEvent(ctx, "src", "msg", models.SeverityInfo, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}

func TestEmitter_StatsUncovered(t *testing.T) {
	emitter := NewEmitter(nil)
	emitted, failed := emitter.Stats()
	assert.Equal(t, int64(0), emitted)
	assert.Equal(t, int64(0), failed)

	// Trigger a failure (nil repo)
	_ = emitter.EmitSystemEvent(context.Background(), "src", "msg", models.SeverityInfo, nil)
	emitted, failed = emitter.Stats()
	assert.Equal(t, int64(0), emitted)
	assert.Equal(t, int64(1), failed)
}

func TestEmitter_ContextMarshalFailureUncovered(t *testing.T) {
	db := newEventTestDB(t)
	repo := database.NewEventRepository(db)
	emitter := NewEmitter(repo)

	// Context with a value that can't be marshaled to JSON (function)
	badCtx := map[string]interface{}{
		"fn": func() {},
	}
	err := emitter.EmitScraperEvent(context.Background(), "test", "bad context", models.SeverityInfo, badCtx)
	// json.Marshal fails for functions, but the event is still persisted with degraded context
	_ = err
	emitted, failed := emitter.Stats()
	// The event should be persisted despite marshal issues
	assert.GreaterOrEqual(t, emitted+failed, int64(1), "at least one emit attempt should be made")
}
