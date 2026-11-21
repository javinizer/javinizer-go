package api

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	apicore "github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/stretchr/testify/require"
)

func TestCloseServerRuntimeRetainsDependenciesUntilDrain(t *testing.T) {
	for _, tc := range []struct {
		name    string
		drained bool
		wait    time.Duration
		warning string
	}{
		{name: "HTTP not drained"},
		{
			name:    "background drain timed out",
			drained: true,
			wait:    10 * time.Second,
			warning: "API cleanup failed: background tasks did not drain; dependencies remain open",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			defer logging.SetOutput(&output)()

			synctest.Test(t, func(t *testing.T) {
				db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, db.Close()) })
				sqlDB, err := db.DB.DB()
				require.NoError(t, err)
				rt := apicore.NewAPIRuntime(&apicore.APIDeps{CoreDeps: &commandutil.CoreDeps{DB: db}})
				rt.EnsureRuntime()
				complete := sync.OnceFunc(rt.TrackBackgroundTask())
				defer complete()

				started := time.Now()
				closeServerRuntime(rt, tc.drained)

				require.Equal(t, tc.wait, time.Since(started))
				require.ErrorIs(t, rt.ServerCtx().Err(), context.Canceled)
				if tc.warning == "" {
					require.Empty(t, output.String())
				} else {
					require.Contains(t, output.String(), tc.warning)
				}
				require.NoError(t, sqlDB.Ping(), "tracked work can still use SQLite")

				complete()
				output.Reset()
				closeServerRuntime(rt, true)
				require.Empty(t, output.String(), "a completed drain does not warn")
				require.ErrorContains(t, sqlDB.Ping(), "database is closed")
			})
		})
	}
}
