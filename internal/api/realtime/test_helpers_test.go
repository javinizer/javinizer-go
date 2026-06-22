package realtime

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

var testRuntimeDeps *core.APIDeps

func initTestWebSocket(t *testing.T) {
	rs := core.NewRuntimeState()
	testkit.InitTestWebSocket(t, rs)
	deps := &core.APIDeps{}
	rt := core.NewAPIRuntime(deps)
	rt.Runtime = rs
	testkit.SetTestRuntime(deps, rt)
	testRuntimeDeps = deps
}
