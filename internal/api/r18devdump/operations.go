package r18devdump

// tryAcquireDumpOp atomically claims the shared dump-operation guard. Every
// mutating dump operation (download, update, upload, clear) holds it for its
// whole lifetime, giving 409 symmetry in every direction.
func (h *dumpHandler) tryAcquireDumpOp() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.running = true
	return true
}

func (h *dumpHandler) releaseDumpOp() {
	h.mu.Lock()
	h.running = false
	h.mu.Unlock()
}
