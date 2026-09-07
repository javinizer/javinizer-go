package core

// shutdownDeps gracefully shuts down runtime resources in APIRuntime.
//
//nolint:unused // used by same-package tests
func shutdownDeps(rt *APIRuntime) {
	if rt == nil {
		return
	}
	rs := rt.GetRuntime()
	if rs == nil {
		return
	}
	rs.Shutdown()
}
