package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseAfterHTTPDrainWithoutCoreDependencies(t *testing.T) {
	// A partially initialized runtime can own cancellation/token cleanup without
	// having acquired a database or scraper registry yet.
	rt := NewAPIRuntime(&APIDeps{})
	ctx := rt.ServerCtx()
	require.NoError(t, rt.CloseAfterHTTPDrain(time.Second))
	require.Error(t, ctx.Err(), "partial initialization must still cancel runtime work")
	require.Nil(t, rt.TrackServerTask(), "cleanup must close new task admission")
	require.NoError(t, rt.CloseAfterHTTPDrain(time.Second))
}
