package database

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ActressAliasRepository persists and queries actress alias records that map
// alternate names to their canonical actress name.
type ActressAliasRepository struct {
	*BaseRepository[models.ActressAlias, uint]
}

// ErrActressAliasAmbiguous means one normalized alias key points at different
// canonical owners. Legacy databases may contain these rows because alias_name
// was historically unique only in its raw form; callers must not choose one.
var (
	ErrActressAliasAmbiguous = errors.New("normalized actress alias has conflicting canonical owners")
	// ErrActressAliasOwnershipConflict means an ordinary alias claim attempted
	// to replace a normalized key already owned by another identity.
	ErrActressAliasOwnershipConflict = errors.New("normalized actress alias is owned by another canonical identity")
)

func validateNormalizedAliasRows(aliases []models.ActressAlias) error {
	owners := make(map[string]string, len(aliases))
	for i := range aliases {
		aliasKey := models.NormalizeActressNameKey(aliases[i].AliasName)
		ownerKey := models.NormalizeActressNameKey(aliases[i].CanonicalName)
		if existing, ok := owners[aliasKey]; ok && existing != ownerKey {
			return fmt.Errorf("%w: %q", ErrActressAliasAmbiguous, aliases[i].AliasName)
		}
		owners[aliasKey] = ownerKey
	}
	return nil
}

func normalizedActressAliasesTx(tx *gorm.DB, aliasName string) ([]models.ActressAlias, error) {
	key := models.NormalizeActressNameKey(aliasName)
	if key == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var aliases []models.ActressAlias
	if err := tx.Where("alias_name_key = ?", key).Order("id").Find(&aliases).Error; err != nil {
		return nil, err
	}
	if len(aliases) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if err := validateNormalizedAliasRows(aliases); err != nil {
		return nil, err
	}
	return aliases, nil
}

func normalizedAliasesForCanonicalTx(tx *gorm.DB, canonicalName string) ([]models.ActressAlias, error) {
	targetKey := models.NormalizeActressNameKey(canonicalName)
	if targetKey == "" {
		return nil, nil
	}
	var aliases []models.ActressAlias
	if err := tx.Where("canonical_name_key = ?", targetKey).Order("id").Find(&aliases).Error; err != nil {
		return nil, err
	}
	for i := range aliases {
		if models.NormalizeActressNameKey(aliases[i].AliasName) == "" {
			continue
		}
		if _, err := normalizedActressAliasesTx(tx, aliases[i].AliasName); err != nil {
			return nil, err
		}
	}
	return aliases, nil
}

func retargetProvenCanonicalAliasesTx(tx *gorm.DB, sourceActressID, targetActressID uint, oldCanonicalName, newCanonicalName string, provenOwnerKeys map[string]struct{}) error {
	oldKey := models.NormalizeActressNameKey(oldCanonicalName)
	if _, proven := provenOwnerKeys[oldKey]; sourceActressID == 0 || targetActressID == 0 || oldKey == "" || !proven {
		return fmt.Errorf("retarget actress aliases from %q: %w", oldCanonicalName, ErrActressAliasOwnershipConflict)
	}
	aliases, err := normalizedAliasesForCanonicalTx(tx, oldCanonicalName)
	if err != nil {
		return err
	}
	if len(aliases) == 0 {
		return nil
	}
	// A canonical string is not identity. Prove that every actress exposing
	// this representation is one of the two IDs being coalesced before moving
	// any aliases. For a rename, source and target are the same ID.
	allowedOwnerIDs := map[uint]struct{}{sourceActressID: {}, targetActressID: {}}
	var identities []models.Actress
	if err := tx.Unscoped().Find(&identities).Error; err != nil {
		return wrapDBErr("verify", fmt.Sprintf("unique actress alias owner %d", sourceActressID), err)
	}
	for i := range identities {
		if _, allowed := allowedOwnerIDs[identities[i].ID]; allowed {
			continue
		}
		for _, representation := range canonicalActressRepresentations(&identities[i]) {
			if models.NormalizeActressNameKey(representation) == oldKey {
				return fmt.Errorf("retarget actress aliases from %q shared by actresses %d and %d: %w", oldCanonicalName, sourceActressID, identities[i].ID, ErrActressAliasOwnershipConflict)
			}
		}
	}
	ids := make([]uint, len(aliases))
	for i := range aliases {
		ids[i] = aliases[i].ID
	}
	return tx.Model(&models.ActressAlias{}).Where("id IN ?", ids).Updates(map[string]interface{}{
		colCanonicalName:     newCanonicalName,
		"canonical_name_key": models.NormalizeActressNameKey(newCanonicalName),
		colUpdatedAt:         time.Now().UTC(),
	}).Error
}

// claimNormalizedActressAliasTx is the ordinary create/accept mode. Existing
// ownership is immutable: equivalent same-owner claims are idempotent, while a
// different owner fails so the caller transaction can roll back.
func claimNormalizedActressAliasTx(tx *gorm.DB, alias *models.ActressAlias) error {
	existing, err := normalizedActressAliasesTx(tx, alias.AliasName)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := tx.Create(alias).Error; err != nil {
			if isLocked(err) {
				return fmt.Errorf("claim actress alias %q encountered a concurrent owner: %w", alias.AliasName, ErrActressAliasOwnershipConflict)
			}
			return wrapDBErr("create", fmt.Sprintf("actress alias %s", alias.AliasName), err)
		}
		return nil
	}
	if err != nil {
		return wrapDBErr("find", fmt.Sprintf("actress alias %s", alias.AliasName), err)
	}
	existingOwner := existing[0].CanonicalName
	if models.NormalizeActressNameKey(existingOwner) != models.NormalizeActressNameKey(alias.CanonicalName) {
		return fmt.Errorf("claim actress alias %q for %q (currently owned by %q): %w", alias.AliasName, alias.CanonicalName, existingOwner, ErrActressAliasOwnershipConflict)
	}
	alias.ID = existing[0].ID
	alias.CreatedAt = existing[0].CreatedAt
	return nil
}

func backfillActressAliasNameKeys(ctx context.Context, db *gorm.DB) error {
	var aliases []models.ActressAlias
	if err := db.WithContext(ctx).Order("id").Find(&aliases).Error; err != nil {
		return wrapDBErr("list", "actress aliases for normalized-key backfill", err)
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range aliases {
			updates := map[string]interface{}{
				"alias_name_key":     models.NormalizeActressNameKey(aliases[i].AliasName),
				"canonical_name_key": models.NormalizeActressNameKey(aliases[i].CanonicalName),
			}
			if err := tx.Model(&models.ActressAlias{}).Where("id = ?", aliases[i].ID).UpdateColumns(updates).Error; err != nil {
				return wrapDBErr("backfill", fmt.Sprintf("actress alias %d normalized keys", aliases[i].ID), err)
			}
		}
		return nil
	})
}

// backfillActressCandidateNameKeys upgrades persisted candidate keys after a
// normalization algorithm change. Equivalent legacy candidates are preserved,
// quarantined, and left keyless so lookup can detect and reject the ambiguity.
func backfillActressCandidateNameKeys(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []models.Actress
		if err := tx.Where("verified = ?", false).Order("id").Find(&candidates).Error; err != nil {
			return wrapDBErr("list", "actress candidates for normalized-key backfill", err)
		}

		groups := make(map[string][]int)
		for i := range candidates {
			for key := range candidateEvidenceKeys(&candidates[i]) {
				groups[key] = append(groups[key], i)
			}
		}

		// Build connected components only among DMM-less candidates. Positive
		// DMM IDs are hard identity boundaries and are never connected to a
		// component that has evidence for another positive ID.
		parent := make([]int, len(candidates))
		for i := range parent {
			parent[i] = i
		}
		var find func(int) int
		find = func(i int) int {
			if parent[i] != i {
				parent[i] = find(parent[i])
			}
			return parent[i]
		}
		union := func(a, b int) {
			ra, rb := find(a), find(b)
			if ra != rb {
				parent[rb] = ra
			}
		}
		for _, group := range groups {
			first := -1
			for _, index := range group {
				if candidates[index].DMMID > 0 {
					continue
				}
				if first < 0 {
					first = index
				} else {
					union(first, index)
				}
			}
		}

		zeroMembers := make(map[int][]int)
		adjacentPositive := make(map[int]map[int][]int)
		for i := range candidates {
			if candidates[i].DMMID <= 0 {
				root := find(i)
				zeroMembers[root] = append(zeroMembers[root], i)
			}
		}
		for _, group := range groups {
			roots := make(map[int]struct{})
			positives := make([]int, 0)
			for _, index := range group {
				if candidates[index].DMMID <= 0 {
					roots[find(index)] = struct{}{}
				} else {
					positives = append(positives, index)
				}
			}
			for root := range roots {
				if adjacentPositive[root] == nil {
					adjacentPositive[root] = make(map[int][]int)
				}
				for _, index := range positives {
					dmm := candidates[index].DMMID
					adjacentPositive[root][dmm] = append(adjacentPositive[root][dmm], index)
				}
			}
		}

		ambiguous := make(map[uint]struct{})
		for root, members := range zeroMembers {
			positiveIDs := adjacentPositive[root]
			if len(members) > 1 || len(positiveIDs) > 0 {
				for _, index := range members {
					ambiguous[candidates[index].ID] = struct{}{}
				}
			}
			if len(positiveIDs) == 1 {
				for _, indexes := range positiveIDs {
					for _, index := range indexes {
						ambiguous[candidates[index].ID] = struct{}{}
					}
				}
			}
		}
		// Legacy duplicate rows carrying the SAME positive DMM remain one
		// ambiguous identity; migration 19's partial unique index prevents new
		// duplicates. Distinct positive IDs are deliberately never marked.
		for _, group := range groups {
			byDMM := make(map[int][]int)
			for _, index := range group {
				if candidates[index].DMMID > 0 {
					byDMM[candidates[index].DMMID] = append(byDMM[candidates[index].DMMID], index)
				}
			}
			for _, indexes := range byDMM {
				if len(indexes) > 1 {
					for _, index := range indexes {
						ambiguous[candidates[index].ID] = struct{}{}
					}
				}
			}
		}

		keyedIDs := make([]uint, 0, len(candidates))
		for i := range candidates {
			if candidates[i].NameKey != "" {
				keyedIDs = append(keyedIDs, candidates[i].ID)
			}
		}
		if len(keyedIDs) > 0 {
			if err := tx.Model(&models.Actress{}).Where("id IN ?", keyedIDs).UpdateColumn("name_key", nil).Error; err != nil {
				return wrapDBErr("clear", "legacy actress candidate normalized keys", err)
			}
		}

		for i := range candidates {
			candidate := &candidates[i]
			if _, conflict := ambiguous[candidate.ID]; conflict {
				if err := tx.Model(&models.Actress{}).Where("id = ?", candidate.ID).UpdateColumn(colAmbiguityQuarantined, true).Error; err != nil {
					return wrapDBErr("quarantine", fmt.Sprintf("actress candidate %d normalized representations", candidate.ID), err)
				}
				continue
			}
			// Rows quarantined by an older name-only backfill must be repaired
			// when the DMM-compatible partition no longer considers them
			// ambiguous. Positive-DMM candidates remain intentionally keyless.
			if candidate.AmbiguityQuarantined {
				if err := tx.Model(&models.Actress{}).Where("id = ?", candidate.ID).UpdateColumn(colAmbiguityQuarantined, false).Error; err != nil {
					return wrapDBErr("unquarantine", fmt.Sprintf("actress candidate %d normalized representations", candidate.ID), err)
				}
			}
			if candidate.DMMID > 0 {
				continue
			}
			if key := actressNameKey(candidate); key != "" {
				if err := tx.Model(&models.Actress{}).Where("id = ?", candidate.ID).UpdateColumn("name_key", key).Error; err != nil {
					return wrapDBErr("backfill", fmt.Sprintf("actress candidate %d normalized key", candidate.ID), err)
				}
			}
		}
		return nil
	})
}

// NewActressAliasRepository constructs an ActressAliasRepository backed by
// the given DB.
func NewActressAliasRepository(db *DB) *ActressAliasRepository {
	return &ActressAliasRepository{
		BaseRepository: NewBaseRepository[models.ActressAlias, uint](
			db, "actress alias",
			func(a models.ActressAlias) string { return a.AliasName },
			WithNewEntity[models.ActressAlias, uint](func() models.ActressAlias { return models.ActressAlias{} }),
		),
	}
}

// Create claims a normalized alias without replacing another owner.
func (r *ActressAliasRepository) Create(ctx context.Context, alias *models.ActressAlias) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return claimNormalizedActressAliasTx(tx, alias)
	})
}

// Upsert inserts the alias when new or updates the existing alias record
// keyed by alias name.
func (r *ActressAliasRepository) Upsert(ctx context.Context, alias *models.ActressAlias) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return claimNormalizedActressAliasTx(tx, alias)
	})
}

// UpsertTx upserts an alias within the given transaction.
func (r *ActressAliasRepository) UpsertTx(tx *gorm.DB, alias *models.ActressAlias) error {
	return claimNormalizedActressAliasTx(tx, alias)
}

// FindByAliasName loads the alias record with the given normalized alias name.
func (r *ActressAliasRepository) FindByAliasName(ctx context.Context, aliasName string) (*models.ActressAlias, error) {
	aliases, err := normalizedActressAliasesTx(r.GetDB().WithContext(ctx), aliasName)
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress alias %s", aliasName), err)
	}
	return &aliases[0], nil
}

// FindByCanonicalName returns every alias whose canonical owner normalizes to
// the given name. Conflicting normalized alias owners fail closed.
func (r *ActressAliasRepository) FindByCanonicalName(ctx context.Context, canonicalName string) ([]models.ActressAlias, error) {
	aliases, err := normalizedAliasesForCanonicalTx(r.GetDB().WithContext(ctx), canonicalName)
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress aliases for %s", canonicalName), err)
	}
	sort.SliceStable(aliases, func(i, j int) bool { return aliases[i].AliasName < aliases[j].AliasName })
	return aliases, nil
}

// List returns all actress alias records.
func (r *ActressAliasRepository) List(ctx context.Context) ([]models.ActressAlias, error) {
	return r.ListAll(ctx)
}

// Delete removes the alias record with the given alias name.
func (r *ActressAliasRepository) Delete(ctx context.Context, aliasName string) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		aliases, err := normalizedActressAliasesTx(tx, aliasName)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return wrapDBErr("find", fmt.Sprintf("actress alias %s", aliasName), err)
		}
		ids := make([]uint, len(aliases))
		for i := range aliases {
			ids[i] = aliases[i].ID
		}
		if err := tx.Delete(&models.ActressAlias{}, ids).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("actress alias %s", aliasName), err)
		}
		return nil
	})
}

// GetAliasMap returns one deterministic mapping per normalized alias key.
func (r *ActressAliasRepository) GetAliasMap(ctx context.Context) (map[string]string, error) {
	var aliases []models.ActressAlias
	if err := r.GetDB().WithContext(ctx).Order("id").Find(&aliases).Error; err != nil {
		return nil, wrapDBErr("list", "actress aliases", err)
	}
	if err := validateNormalizedAliasRows(aliases); err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, a := range aliases {
		key := models.NormalizeActressNameKey(a.AliasName)
		if key == "" {
			continue
		}
		if _, exists := result[key]; !exists {
			result[key] = a.CanonicalName
		}
	}
	return result, nil
}

// AliasGroup is the set of all known names for a single performer: the
// canonical name plus every alias that resolves to it.
type AliasGroup struct {
	Canonical string   // The canonical (preferred) name; empty when name is unknown.
	Names     []string // Canonical first, then aliases, deduplicated, order-stable.
}

// GetAliasGroup resolves a name to its full known-names group. The input may
// be either an alias or a canonical name. When the name is not present in the
// alias table at all, Canonical is empty and Names is nil — callers should
// treat this as "no known aliases, nothing to choose between". The returned
// Names slice is deduplicated and order-stable (canonical first, then aliases
// in the order the database returns them).
func (r *ActressAliasRepository) GetAliasGroup(ctx context.Context, name string) (AliasGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return AliasGroup{}, nil
	}

	// Resolve the canonical form. Prefer treating `name` as a canonical when
	// rows point at it; only follow the alias mapping when `name` is not itself
	// a canonical. This avoids returning the wrong group when a name is both a
	// former name (alias) of one performer and the current name (canonical) of
	// another. Order is deterministic (FindByCanonicalName sorts by alias_name)
	// so the dropdown is stable.
	canonical := name
	matching, err := r.FindByCanonicalName(ctx, name)
	if err != nil {
		return AliasGroup{}, err
	}
	if len(matching) > 0 {
		canonical = matching[0].CanonicalName
		lowestID := matching[0].ID
		for i := 1; i < len(matching); i++ {
			if matching[i].ID < lowestID {
				lowestID = matching[i].ID
				canonical = matching[i].CanonicalName
			}
		}
	}
	if len(matching) == 0 {
		// `name` is not a canonical — try following it as an alias. The alias
		// row itself guarantees FindByCanonicalName returns at least one row.
		if a, ferr := r.FindByAliasName(ctx, name); ferr == nil {
			canonical = a.CanonicalName
			matching, err = r.FindByCanonicalName(ctx, canonical)
			if err != nil {
				return AliasGroup{}, err
			}
		} else if !IsNotFound(ferr) {
			return AliasGroup{}, ferr
		}
	}
	// FindByCanonicalName uses a Find() query, which returns an empty slice
	// (not IsNotFound) when nothing matches. An empty result means the name is
	// neither an alias nor a canonical in the table — there is no group.
	if len(matching) == 0 {
		return AliasGroup{}, nil
	}

	seen := make(map[string]struct{}, len(matching)+1)
	names := make([]string, 0, len(matching)+1)
	add := func(n string) {
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}

	add(canonical)
	for _, a := range matching {
		add(a.AliasName)
	}

	return AliasGroup{Canonical: canonical, Names: names}, nil
}

var defaultActressAliases []models.ActressAlias

func init() {
	// Curated rename mappings for well-known AV actresses who have changed
	// stage names. Canonical form is the most current name. Each alias maps
	// directly to the canonical (no transitive chains) so that single-hop
	// resolution collapses all of a performer's credits into one entry.
	//
	// Sources: ja.wikipedia.org and community wikis (seesaawiki av_neme,
	// av-wiki.net), cross-checked across multiple titles.
	defaultActressAliases = []models.ActressAlias{
		// 新セリナ — renamed 青木桃 → 朝日芹奈 (2022-10) → 堤セリナ (2024-03) → 新セリナ (2025-04)
		{AliasName: "青木桃", CanonicalName: "新セリナ"},
		{AliasName: "朝日芹奈", CanonicalName: "新セリナ"},
		{AliasName: "堤セリナ", CanonicalName: "新セリナ"},
		// 尾崎えりか — renamed 与田さくら → 尾崎えりか (2022-09)
		{AliasName: "与田さくら", CanonicalName: "尾崎えりか"},
		// 日向ゆら — renamed 広瀬みつき → 日向ゆら (2022-08)
		{AliasName: "広瀬みつき", CanonicalName: "日向ゆら"},
	}
}

// SeedDefaultActressAliases inserts the built-in default actress alias mappings
// into the repository. Existing user-curated aliases are preserved: only
// alias names that are not already present are inserted, so a user's choice of
// canonical name for an alias is never overwritten by the seed.
func SeedDefaultActressAliases(ctx context.Context, repo ActressAliasRepositoryInterface) {
	for i := range defaultActressAliases {
		a := defaultActressAliases[i]
		_, err := repo.FindByAliasName(ctx, a.AliasName)
		if err == nil {
			// Already present (possibly user-curated) — leave it untouched.
			continue
		}
		if !IsNotFound(err) {
			logging.Warnf("failed to seed actress alias %q: %v", a.AliasName, err)
			continue
		}
		if err := repo.Create(ctx, &a); err != nil {
			logging.Warnf("failed to seed actress alias %q: %v", a.AliasName, err)
		}
	}
}
