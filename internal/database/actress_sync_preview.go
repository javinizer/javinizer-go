package database

import (
	"context"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"gorm.io/gorm"
)

// PreviewOfflineSync reads selected identities and name-management provenance in
// one transaction, then evaluates evidence without writing any application state.
// It neither repairs quarantine nor promotes candidates. User/legacy/revoked
// ownership blocks the whole preview conservatively; there is no field-level
// sync ownership schema yet. A future writer must re-read all protections under
// its lease-fenced canonical mutation transaction, never apply this plan blindly.
func (r *ActressRepository) PreviewOfflineSync(ctx context.Context, ids []uint, snapshot *actresscache.Snapshot, options actresssync.Options) (*actresssync.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := options.ValidateSelection(ids); err != nil {
		return nil, err
	}
	// Own the selection across the transaction; callers must not mutate it during entry.
	ids = append([]uint(nil), ids...)
	var plan *actresssync.Plan
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID                   uint
			DMMID                int
			Verified             bool
			AmbiguityQuarantined bool
			Origin               string
			Managed              bool
			UpdatedAt            time.Time
		}
		if err := tx.Table("actresses AS a").Select("a.id, a.dmm_id, a.verified, a.ambiguity_quarantined, a.origin, a.updated_at, COALESCE(p.managed, 0) AS managed").
			Joins("LEFT JOIN actress_import_name_provenance AS p ON p.actress_id = a.id").Where("a.id IN ?", ids).Scan(&rows).Error; err != nil {
			return err
		}
		found := make(map[uint]actresssync.Target, len(rows))
		for _, row := range rows {
			found[row.ID] = actresssync.Target{ID: row.ID, Exists: true, DMMID: row.DMMID, Verified: row.Verified, Quarantined: row.AmbiguityQuarantined, Protected: row.Origin != ActressOriginImport || !row.Managed, UpdatedAt: row.UpdatedAt}
		}
		targets := make([]actresssync.Target, len(ids))
		for i, id := range ids {
			targets[i] = found[id]
			targets[i].ID = id
		}
		var err error
		plan, err = actresssync.Preview(ctx, targets, snapshot, options)
		return err
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}
