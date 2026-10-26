package models

// SuccessfulUndoDisabled is a durable policy decision, not the current setting.
// Legacy ledgers without a policy and incomplete/failed operations retain recovery.
func (g GeneratedFilesJSON) SuccessfulUndoDisabled() bool {
	return g.UndoRetention != nil && !*g.UndoRetention && g.ApplyTerminal != nil && g.ApplyTerminal.Outcome == "success"
}
