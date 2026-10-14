package database

import "gorm.io/gorm"

// runCandidateQuarantineMutationTx owns one atomic mutation boundary. Internal
// callers that already own a larger transaction must use explicitly named
// deferred primitives and let that owner finalize once instead.
func runCandidateQuarantineMutationTx(tx *gorm.DB, mutate func(*gorm.DB) error) error {
	return retryOnLocked(func() error {
		return tx.Transaction(func(scoped *gorm.DB) error {
			if err := mutate(scoped); err != nil {
				return err
			}
			return recomputeActressCandidateQuarantineTx(scoped)
		})
	})
}
