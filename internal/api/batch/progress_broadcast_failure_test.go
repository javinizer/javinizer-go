package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	ws "github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func TestBroadcastProgressContainsSerializationFailure(t *testing.T) {
	runtime := core.NewRuntimeState()
	hub := ws.NewHub()
	runtime.SetWebSocketHubForTesting(hub)

	logger := logging.WithField("test", "progress broadcast").Logger
	oldHooks := logger.ReplaceHooks(make(logrus.LevelHooks))
	oldLevel := logger.GetLevel()
	logger.SetLevel(logrus.WarnLevel)
	hook := logrustest.NewLocal(logger)
	t.Cleanup(func() {
		logger.ReplaceHooks(oldHooks)
		logger.SetLevel(oldLevel)
	})

	// MessageArgs is an open-ended map. Unsupported JSON values must be
	// contained at the broadcast boundary rather than panic a batch worker.
	message := &ws.ProgressMessage{
		JobID:       "serialization-failure-job",
		MessageArgs: map[string]any{"unsupported": make(chan struct{})},
	}
	require.NotPanics(t, func() { broadcastProgress(runtime, message) })
	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	require.Equal(t, logrus.WarnLevel, entries[0].Level)
	require.Contains(t, entries[0].Message, "Failed to broadcast progress update for job serialization-failure-job")
	require.Contains(t, entries[0].Message, "json: unsupported type")
	require.Same(t, hub, runtime.WebSocketHub())

	hook.Reset()
	message.MessageArgs = map[string]any{"filename": "fixture.mp4"}
	require.NotPanics(t, func() { broadcastProgress(runtime, message) })
	require.Empty(t, hook.AllEntries(), "a rejected frame must not poison later progress updates")
}
