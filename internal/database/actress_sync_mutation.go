package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// revokeActressSyncNameTx records explicit manual intent independently of import
// provenance. Even a same-value clear/edit fences outstanding confirmations.
func revokeActressSyncNameTx(tx *gorm.DB, id uint) error {
	if err := tx.Exec("INSERT INTO actress_sync_name_guards (actress_id, protected) VALUES (?, 1) ON CONFLICT(actress_id) DO UPDATE SET protected = 1", id).Error; err != nil {
		return err
	}
	if err := tx.Model(&actressSyncBinding{}).Where("actress_id = ? AND status = ?", id, "active").Update(colStatus, "revoked").Error; err != nil {
		return err
	}
	return tx.Model(&ActressSyncTask{}).Where("actress_id = ? AND status IN ?", id, []string{syncTaskPending, syncTaskLeased}).Updates(map[string]any{colStatus: "invalidated", syncColReason: "manual_protection"}).Error
}

// ProtectOfflineSyncName records manual protection without requiring a value
// change. It never grants or restores ownership, and cannot be undone by rebinding.
func (r *ActressRepository) ProtectOfflineSyncName(ctx context.Context, id uint) error {
	if id == 0 {
		return ErrActressSyncProtected
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actor models.Actress
		if err := tx.First(&actor, id).Error; err != nil {
			return err
		}
		return revokeActressSyncNameTx(tx, id)
	})
}

// applyConfirmedJapaneseNameTx is the sync-specific canonical mutation boundary.
// The caller already holds write admission and a validated, fenced task. It does
// not promote, change identity IDs, import source aliases or restore projections.
func applyConfirmedJapaneseNameTx(tx *gorm.DB, actor *models.Actress, name string) error {
	contentIDs, err := movieContentIDsForActressesTx(tx, actor.ID)
	if err != nil {
		return err
	}
	before, err := captureMovieRenderSnapshotsTx(tx, contentIDs)
	if err != nil {
		return err
	}
	if err := tx.Model(&models.Actress{}).Where("id = ?", actor.ID).Update(colJapaneseName, name).Error; err != nil {
		return err
	}
	if err := deleteActressTranslationsTx(tx, actor.ID); err != nil {
		return err
	}
	if err := refreshIdentitySnapshotCreditsTx(tx, actor.ID, actor, actor.FirstName, actor.LastName, name); err != nil {
		return err
	}
	if err := transitionActressCanonicalNamesTx(tx, actor.ID, actor); err != nil {
		return err
	}
	if err := reconcileActressCollisionsTx(tx, actor.ID); err != nil {
		return err
	}
	if err := recomputeActressCandidateQuarantineTx(tx); err != nil {
		return err
	}
	var after models.Actress
	if err := tx.First(&after, actor.ID).Error; err != nil {
		return err
	}
	if !after.Verified || after.AmbiguityQuarantined || after.Origin != ActressOriginImport || after.DMMID != actor.DMMID {
		return ErrActressSyncProtected
	}
	if err := invalidateChangedMovieRenderInputsTx(tx, before, contentIDs); err != nil {
		return err
	}
	// Only this validated transaction may restore the before-state revoked by
	// its own Japanese-name UPDATE. No generic import restores sync entitlements.
	if err := restoreImportNameProvenanceTx(tx, actor.ID, importNamesManaged, actor.Origin); err != nil {
		return err
	}
	result := tx.Exec("UPDATE actress_sync_name_guards SET protected = 0 WHERE actress_id = ?", actor.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("sync name protection state disappeared")
	}
	return nil
}
