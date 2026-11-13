package core

// WithReloadLock executes fn while holding reloadMu for writing. This serializes
// fn with config hot-reloads and dump lookup opening/publication. Callers must
// not call methods that acquire reloadMu from fn, including ReloadConfig,
// ReplaceReloadable, Snapshot, GetAPIConfig, or workflow/batch factory getters.
// Use explicitly Locked methods when provided. Lock order is reloadMu →
// CoreDeps.mu, so calling CoreDeps methods from fn is safe.
func (r *APIRuntime) WithReloadLock(fn func() error) error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	return fn()
}

// LockReload holds the reload lock until the returned function is called.
func (r *APIRuntime) LockReload() func() {
	r.reloadMu.Lock()
	return r.reloadMu.Unlock
}
