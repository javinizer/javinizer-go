package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ActressAliasRepository persists and queries actress alias records that map
// alternate names to their canonical actress name.
type ActressAliasRepository struct {
	*BaseRepository[models.ActressAlias, uint]
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

// Create inserts a new actress alias record.
func (r *ActressAliasRepository) Create(ctx context.Context, alias *models.ActressAlias) error {
	return r.BaseRepository.Create(ctx, alias)
}

// Upsert inserts the alias when new or updates the existing alias record
// keyed by alias name.
func (r *ActressAliasRepository) Upsert(ctx context.Context, alias *models.ActressAlias) error {
	existing, err := r.FindByAliasName(ctx, alias.AliasName)
	if err != nil {
		if !IsNotFound(err) {
			return err
		}
		return r.Create(ctx, alias)
	}

	alias.ID = existing.ID
	alias.CreatedAt = existing.CreatedAt
	if err := r.GetDB().WithContext(ctx).Save(alias).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress alias %s", alias.AliasName), err)
	}
	return nil
}

// FindByAliasName loads the alias record with the given alias name.
func (r *ActressAliasRepository) FindByAliasName(ctx context.Context, aliasName string) (*models.ActressAlias, error) {
	var alias models.ActressAlias
	err := r.GetDB().WithContext(ctx).First(&alias, "alias_name = ?", aliasName).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress alias %s", aliasName), err)
	}
	return &alias, nil
}

// FindByCanonicalName returns all alias records pointing at the given
// canonical actress name, ordered by alias_name for deterministic output.
func (r *ActressAliasRepository) FindByCanonicalName(ctx context.Context, canonicalName string) ([]models.ActressAlias, error) {
	var aliases []models.ActressAlias
	err := r.GetDB().WithContext(ctx).
		Where("canonical_name = ?", canonicalName).
		Order("alias_name").
		Find(&aliases).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress aliases for %s", canonicalName), err)
	}
	return aliases, nil
}

// List returns all actress alias records.
func (r *ActressAliasRepository) List(ctx context.Context) ([]models.ActressAlias, error) {
	return r.ListAll(ctx)
}

// Delete removes the alias record with the given alias name.
func (r *ActressAliasRepository) Delete(ctx context.Context, aliasName string) error {
	if err := r.GetDB().WithContext(ctx).Delete(&models.ActressAlias{}, "alias_name = ?", aliasName).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("actress alias %s", aliasName), err)
	}
	return nil
}

// GetAliasMap returns a map from each alias name to its canonical actress name.
func (r *ActressAliasRepository) GetAliasMap(ctx context.Context) (map[string]string, error) {
	aliases, err := r.List(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, a := range aliases {
		result[a.AliasName] = a.CanonicalName
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
