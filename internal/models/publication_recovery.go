package models

// PublicationRecovery is the authoritative result of an attempted staged
// rollback. Managed paths are reconciled, never added to the ordinary Delete
// list by failure completion. Only carried install proofs authorize recovery.
type PublicationRecovery struct {
	Managed    []string
	Deletes    []DeleteEntry
	Moves      []FileMove
	Unresolved bool
}
