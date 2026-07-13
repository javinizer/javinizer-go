package core

// WithReloadLock executes fn while holding reloadMu for writing. This serializes
// fn with config hot-reloads and dump lookup opening/publication. Callers must
// not call ReloadConfig or ReplaceReloadable from fn. Lock order is reloadMu →
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
