package database

import "gorm.io/gorm"

// recomputeActressCandidateQuarantineTx skips only a known-clean persisted
// revision in the caller's transaction. Never cache this decision across retries.
func recomputeActressCandidateQuarantineTx(tx *gorm.DB) error {
	return reconcileActressQuarantineRevisionTx(tx, false)
}

func reconcileActressQuarantineRevisionTx(tx *gorm.DB, force bool) error {
	tx = tx.Session(&gorm.Session{})
	var state struct {
		Revision         int64
		ConsumedRevision int64
	}
	// Take (not Find) makes missing state fail closed rather than look clean.
	if err := tx.Table("actress_quarantine_state").Where("id = 1").Take(&state).Error; err != nil {
		return wrapDBErr("read", "actress quarantine revision", err)
	}
	if !force && state.Revision == state.ConsumedRevision {
		return nil
	}
	if err := recomputeActressCandidateQuarantineUnconditionallyTx(tx); err != nil {
		return err
	}
	// NameKey is both input and output of the single-pass oracle. Its writes may
	// invalidate the revision again; consume only the captured revision, never the
	// current one, and do not loop to a fixed point or hide the next repair pass.
	if state.ConsumedRevision == state.Revision {
		return nil // Forced startup pass already has this revision consumed.
	}
	if err := tx.Exec("UPDATE actress_quarantine_state SET consumed_revision = ? WHERE id = 1", state.Revision).Error; err != nil {
		return wrapDBErr("consume", "actress quarantine revision", err)
	}
	return nil
}
