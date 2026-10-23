package database

import "gorm.io/gorm"

const (
	importNamesLegacy = iota
	importNamesManaged
	importNamesRevoked
)

// importNameProvenanceTx reads server-only provenance inside each import retry.
// Neither ordinary model fields nor an import payload can grant management.
func importNameProvenanceTx(tx *gorm.DB, id uint) (int, error) {
	var state int
	err := tx.Raw("SELECT CASE WHEN managed = 1 THEN 1 ELSE 2 END FROM actress_import_name_provenance WHERE actress_id = ?", id).Scan(&state).Error
	return state, err
}

func revokeImportNameManagementTx(tx *gorm.DB, id uint) error {
	return tx.Exec("INSERT INTO actress_import_name_provenance (actress_id, managed) VALUES (?, 0) ON CONFLICT(actress_id) DO UPDATE SET managed = 0", id).Error
}

// restoreImportNameProvenanceTx restores only this import's transaction-local
// before-state after its own writes fired the conservative revocation trigger.
// Legacy overrides remain request-only; revoked identities never become managed.
func restoreImportNameProvenanceTx(tx *gorm.DB, id uint, prior int, previousOrigin string) error {
	if prior == importNamesRevoked || previousOrigin == ActressOriginUser {
		return revokeImportNameManagementTx(tx, id)
	}
	if prior == importNamesManaged {
		return tx.Exec("INSERT INTO actress_import_name_provenance (actress_id, managed) SELECT id, 1 FROM actresses WHERE id = ? AND origin = ? ON CONFLICT(actress_id) DO UPDATE SET managed = 1", id, ActressOriginImport).Error
	}
	return tx.Exec("DELETE FROM actress_import_name_provenance WHERE actress_id = ? AND EXISTS (SELECT 1 FROM actresses WHERE id = ? AND origin = ?)", id, id, ActressOriginImport).Error
}
