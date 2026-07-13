package core

// WithReloadLock executes fn while holding reloadMu for writing. This serializes
// fn with config hot-reloads (ReloadConfig/ReplaceReloadable both publish under
// reloadMu), so a concurrent PUT /api/v1/config cannot swap the dump closer
// while the dump handler is closing/importing. Callers must NOT call back into
// ReloadConfig/ReplaceReloadable from fn — those acquire reloadMu and would
// deadlock; perform any reload from outside the callback. Lock order is
// reloadMu → CoreDeps.mu, so calling CoreDeps methods from fn is safe.
func (r *APIRuntime) WithReloadLock(fn func() error) error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	return fn()
}
