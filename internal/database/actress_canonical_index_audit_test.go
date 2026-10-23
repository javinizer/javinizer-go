package database

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// This oracle is the unchanged quarantine implementation from 413cd8e, before
// canonical indexing. Keep it independent of the optimized owner selection.
func auditReferenceQuarantineTx(tx *gorm.DB) error {
	var candidates []models.Actress
	if err := tx.Where("verified = ?", false).Order("id").Find(&candidates).Error; err != nil {
		return wrapDBErr("list", "actress candidates for normalized-key backfill", err)
	}

	// Only candidates receive derived updates. Avoid loading the growing verified
	// catalogue (and unrelated evidence) when there is nothing to reconcile.
	if len(candidates) == 0 {
		return nil
	}

	var collisionCandidateIDs []uint
	if err := tx.Model(&models.CreditCollision{}).
		Distinct("movie_credits.actress_id").
		Joins("JOIN movie_credits ON movie_credits.id = credit_collisions.credit_id AND movie_credits.movie_content_id = credit_collisions.movie_content_id").
		Where("credit_collisions.field = ? AND credit_collisions.status = ? AND movie_credits.suppressed = ?", models.CreditFieldIdentityLink, models.CollisionStatusOpen, false).
		Pluck("movie_credits.actress_id", &collisionCandidateIDs).Error; err != nil {
		return wrapDBErr("list", "live identity-link candidate collisions", err)
	}
	var verified []models.Actress
	if err := tx.Where("verified = ? AND (dmm_id <= 0 OR dmm_id IS NULL)", true).Order("id").Find(&verified).Error; err != nil {
		return wrapDBErr("list", "verified actresses for candidate backfill", err)
	}
	var aliases []models.ActressAlias
	if err := tx.Order("id").Find(&aliases).Error; err != nil {
		return wrapDBErr("list", "actress aliases for candidate backfill", err)
	}
	groups := make(map[string][]int)
	for i := range candidates {
		for key := range candidateEvidenceKeys(&candidates[i]) {
			groups[key] = append(groups[key], i)
		}
	}
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

	ambiguous := make(map[uint]struct{})
	zeroMembers := make(map[int][]int)
	adjacentPositive := make(map[int]map[int][]int)
	for i := range candidates {
		if candidates[i].DMMID <= 0 {
			zeroMembers[find(i)] = append(zeroMembers[find(i)], i)
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
				adjacentPositive[root][candidates[index].DMMID] = append(adjacentPositive[root][candidates[index].DMMID], index)
			}
		}
	}
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
	for _, id := range collisionCandidateIDs {
		ambiguous[id] = struct{}{}
	}

	verifiedDMMlessByKey := make(map[string]struct{})
	verifiedCanonicalKeys := make(map[string]struct{})
	for i := range verified {
		if verified[i].DMMID > 0 {
			continue
		}
		for key := range canonicalActressRepresentationKeys(&verified[i]) {
			verifiedDMMlessByKey[key] = struct{}{}
			verifiedCanonicalKeys[key] = struct{}{}
		}
	}
	for i := range aliases {
		if _, owned := verifiedCanonicalKeys[models.NormalizeActressNameKey(aliases[i].CanonicalName)]; owned {
			if key := models.NormalizeActressNameKey(aliases[i].AliasName); key != "" {
				verifiedDMMlessByKey[key] = struct{}{}
			}
		}
	}
	for i := range candidates {
		if candidates[i].DMMID <= 0 {
			continue
		}
		for key := range candidateEvidenceKeys(&candidates[i]) {
			if _, conflict := verifiedDMMlessByKey[key]; conflict {
				ambiguous[candidates[i].ID] = struct{}{}
				break
			}
		}
	}

	quarantine, unquarantine, clearKeys := make([]uint, 0), make([]uint, 0), make([]uint, 0)
	keyUpdates := make(map[uint]string)
	for i := range candidates {
		candidate := &candidates[i]
		_, desiredQuarantine := ambiguous[candidate.ID]
		desiredKey := ""
		if !desiredQuarantine && candidate.DMMID <= 0 {
			desiredKey = actressNameKey(candidate)
		}
		if candidate.AmbiguityQuarantined != desiredQuarantine {
			if desiredQuarantine {
				quarantine = append(quarantine, candidate.ID)
			} else {
				unquarantine = append(unquarantine, candidate.ID)
			}
		}
		if candidate.NameKey == desiredKey {
			continue
		}
		if desiredKey == "" {
			clearKeys = append(clearKeys, candidate.ID)
		} else {
			keyUpdates[candidate.ID] = desiredKey
		}
	}
	updateIDs := func(ids []uint, column string, value interface{}, operation, label string) error {
		for start := 0; start < len(ids); start += 300 {
			end := start + 300
			if end > len(ids) {
				end = len(ids)
			}
			if err := tx.Model(&models.Actress{}).Where("id IN ?", ids[start:end]).UpdateColumn(column, value).Error; err != nil {
				return wrapDBErr(operation, label, err)
			}
		}
		return nil
	}
	if err := updateIDs(quarantine, colAmbiguityQuarantined, true, "quarantine", "actress candidate derived ambiguity"); err != nil {
		return err
	}
	if err := updateIDs(unquarantine, colAmbiguityQuarantined, false, "unquarantine", "actress candidate derived ambiguity"); err != nil {
		return err
	}
	if err := updateIDs(clearKeys, "name_key", nil, "clear", "legacy actress candidate normalized keys"); err != nil {
		return err
	}
	keyIDs := make([]uint, 0, len(keyUpdates))
	for id := range keyUpdates {
		keyIDs = append(keyIDs, id)
	}
	sort.Slice(keyIDs, func(i, j int) bool { return keyIDs[i] < keyIDs[j] })
	for start := 0; start < len(keyIDs); start += 300 {
		end := start + 300
		if end > len(keyIDs) {
			end = len(keyIDs)
		}
		ids := keyIDs[start:end]
		var statement strings.Builder
		statement.WriteString("UPDATE actresses SET name_key = CASE id")
		args := make([]interface{}, 0, len(ids)*3)
		for _, id := range ids {
			statement.WriteString(" WHEN ? THEN ?")
			args = append(args, id, keyUpdates[id])
		}
		statement.WriteString(" END WHERE id IN (")
		for i, id := range ids {
			if i > 0 {
				statement.WriteByte(',')
			}
			statement.WriteByte('?')
			args = append(args, id)
		}
		statement.WriteByte(')')
		if err := tx.Exec(statement.String(), args...).Error; err != nil {
			return wrapDBErr("backfill", "actress candidate normalized keys", err)
		}
	}
	return propagateQuarantineTransitionsTx(tx, quarantine, unquarantine)
}

func TestCanonicalIndexAuditDifferential(t *testing.T) {
	names := []string{"", "A B", "Ｂ Ａ", "が", "か\u3099", " Alpha\u3000Beta ", "ALPHA BETA", "Stage 0", "Stage 1", "Stage 2", "Other"}
	for seed := int64(0); seed < 12; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			db := newCreditTestDB(t)
			rng := rand.New(rand.NewSource(seed))
			for i := 0; i < 40; i++ {
				a := models.Actress{FirstName: names[rng.Intn(len(names))], LastName: names[rng.Intn(len(names))], JapaneseName: names[rng.Intn(len(names))], Aliases: "Display only", Verified: rng.Intn(2) == 0, AmbiguityQuarantined: rng.Intn(2) == 0}
				switch rng.Intn(4) {
				case 0:
					a.DMMID = 1000 + i
				case 1:
					a.DMMID = -1
				}
				require.NoError(t, db.Create(&a).Error)
				if rng.Intn(7) == 0 {
					require.NoError(t, db.Model(&a).UpdateColumn("dmm_id", nil).Error)
				}
			}
			for i := 0; i < 10; i++ {
				require.NoError(t, db.Create(&models.ActressAlias{AliasName: fmt.Sprintf("Stage %d", i), CanonicalName: names[rng.Intn(len(names))]}).Error)
			}
			var all []models.Actress
			require.NoError(t, db.Order("id").Find(&all).Error)
			for _, name := range append(names, "Display only") {
				for _, dmm := range []int{-1, 0, 99999} {
					incoming := models.Actress{JapaneseName: name, DMMID: dmm}
					var expected []uint
					for i := range all {
						if (dmm <= 0 || all[i].DMMID == 0) && exactActressNamesMatch(&incoming, &all[i]) {
							// NULL scans into zero, but the SQL positive fallback excludes NULL.
							var eligible int64
							require.NoError(t, db.Model(&models.Actress{}).Where("id = ? AND dmm_id = 0", all[i].ID).Count(&eligible).Error)
							if dmm <= 0 || eligible > 0 {
								expected = append(expected, all[i].ID)
							}
						}
					}
					var actual []uint
					require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
						rows, err := NewActressRepository(&DB{DB: tx}).findImportMatchesByCanonicalUnion(t.Context(), &incoming)
						for _, a := range rows {
							actual = append(actual, a.ID)
						}
						return err
					}))
					require.ElementsMatch(t, expected, actual, "name %q DMM %d", name, dmm)
				}
			}
			capture := func(fn func(*gorm.DB) error) []models.Actress {
				var result []models.Actress
				rollback := errors.New("audit rollback")
				err := db.Transaction(func(tx *gorm.DB) error {
					if err := fn(tx); err != nil {
						return err
					}
					if err := tx.Order("id").Find(&result).Error; err != nil {
						return err
					}
					return rollback
				})
				require.ErrorIs(t, err, rollback)
				return result
			}
			require.Equal(t, capture(auditReferenceQuarantineTx), capture(recomputeActressCandidateQuarantineTx))
		})
	}
}

func TestCanonicalIndexAuditRawMutationsAndRollback(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,first_name,last_name,japanese_name) VALUES(500,0,NULL,NULL,'Old')").Error)
	lookup := func(tx *gorm.DB, name string, ids ...uint) {
		rows, err := NewActressRepository(&DB{DB: tx}).findImportMatchesByCanonicalUnion(t.Context(), &models.Actress{JapaneseName: name})
		require.NoError(t, err)
		actual := make([]uint, 0, len(rows))
		for _, a := range rows {
			actual = append(actual, a.ID)
		}
		require.ElementsMatch(t, ids, actual)
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		lookup(tx, "Old", 500)
		require.NoError(t, tx.Exec("UPDATE actresses SET first_name='Ａ', last_name='Ｂ', japanese_name=NULL WHERE id=500").Error)
		lookup(tx, "Old")
		lookup(tx, "a b", 500)
		lookup(tx, "b a", 500)
		require.NoError(t, tx.Exec("UPDATE actresses SET id=501,first_name='Next',last_name=NULL WHERE id=500").Error)
		lookup(tx, "a b")
		lookup(tx, "Next", 501)
		require.NoError(t, tx.Exec("INSERT OR REPLACE INTO actresses(id,dmm_id,japanese_name) VALUES(501,0,'Replacement')").Error)
		lookup(tx, "Next")
		lookup(tx, "Replacement", 501)
		return nil
	}))
	rollback := errors.New("audit rollback")
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec("UPDATE actresses SET japanese_name='Rolled back' WHERE id=501").Error)
		lookup(tx, "Rolled back", 501)
		lookup(tx, "Replacement")
		return rollback
	}), rollback)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		lookup(tx, "Replacement", 501)
		lookup(tx, "Rolled back")
		require.NoError(t, tx.Exec("DELETE FROM actresses WHERE id=501").Error)
		lookup(tx, "Replacement")
		return nil
	}))
	var remaining int64
	require.NoError(t, db.Table("actress_canonical_keys").Count(&remaining).Error)
	require.Zero(t, remaining)
	require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&remaining).Error)
	require.Zero(t, remaining)
}

func TestCanonicalIndexAuditPlainSQLiteWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw-writer.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = raw.Exec("INSERT INTO actresses(id,dmm_id,japanese_name) VALUES(100,0,'外部')")
	require.NoError(t, err)
	lookup := func(name string, count int) {
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			rows, err := canonicalActressesByKeysTx(tx, []string{models.NormalizeActressNameKey(name)}, "")
			require.NoError(t, err)
			require.Len(t, rows, count)
			return nil
		}))
	}
	lookup("外部", 1)
	_, err = raw.Exec("UPDATE actresses SET japanese_name='更新' WHERE id=100")
	require.NoError(t, err)
	lookup("外部", 0)
	lookup("更新", 1)
	_, err = raw.Exec("UPDATE actresses SET japanese_name='Pending' WHERE id=100")
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER audit_fail_key BEFORE INSERT ON actress_canonical_keys BEGIN SELECT RAISE(ABORT, 'audit index write failure'); END").Error)
	err = db.Transaction(func(tx *gorm.DB) error { return syncActressCanonicalKeysTx(tx) })
	require.ErrorContains(t, err, "audit index write failure")
	var dirty int64
	require.NoError(t, db.Table("actress_canonical_keys_dirty").Where("actress_id=100").Count(&dirty).Error)
	require.EqualValues(t, 1, dirty)
	require.NoError(t, db.Exec("DROP TRIGGER audit_fail_key").Error)
	lookup("Pending", 1)
	lookup("更新", 0)
	_, err = raw.Exec("DELETE FROM actresses WHERE id=100")
	require.NoError(t, err)
	lookup("Pending", 0)
}

func TestCanonicalIndexAuditConcurrentRenameRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retry.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = raw.Exec("INSERT INTO actresses(id,dmm_id,japanese_name,verified,origin) VALUES(100,0,'Before',1,'user')")
	require.NoError(t, err)
	renamed := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("audit:concurrent-rename", func(tx *gorm.DB) {
		if renamed || tx.Statement.Table != "actresses" || !strings.Contains(tx.Statement.SQL.String(), "id IN") {
			return
		}
		renamed = true
		_, err := raw.Exec("UPDATE actresses SET japanese_name='After' WHERE id=100")
		require.NoError(t, err)
	}))
	incoming := models.Actress{JapaneseName: "Before"}
	require.NoError(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
	require.NoError(t, db.Callback().Query().Remove("audit:concurrent-rename"))
	require.True(t, renamed, "must exercise the independent writer during index refresh")
	require.NotEqualValues(t, 100, incoming.ID, "retried lookup must not claim the renamed identity")
	var original models.Actress
	require.NoError(t, db.First(&original, 100).Error)
	require.Equal(t, "After", original.JapaneseName)
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		rows, err := canonicalActressesByKeysTx(tx, []string{"after"}, "")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.EqualValues(t, 100, rows[0].ID)
		rows, err = canonicalActressesByKeysTx(tx, []string{"before"}, "")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, incoming.ID, rows[0].ID)
		return nil
	}))
}

func TestCanonicalIndexAuditRetargetOwnerProof(t *testing.T) {
	for _, dmm := range []interface{}{nil, -1, 0, 2345} {
		for _, verified := range []bool{false, true} {
			t.Run(fmt.Sprintf("dmm_%v/verified_%v", dmm, verified), func(t *testing.T) {
				db := newCreditTestDB(t)
				require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,japanese_name,verified,origin) VALUES(100,0,'New Owner',1,'user'),(101,?,NULL,?, 'scrape'),(102,0,NULL,0,'scrape')", dmm, verified).Error)
				require.NoError(t, db.Exec("UPDATE actresses SET aliases='Old Owner',name_key='old owner' WHERE id=102").Error)
				alias := models.ActressAlias{AliasName: "Curated", CanonicalName: "Old Owner"}
				require.NoError(t, db.Create(&alias).Error)
				require.NoError(t, db.Transaction(syncActressCanonicalKeysTx))
				// Conflict arrives through a raw nullable-name update after cache hydration.
				require.NoError(t, db.Exec("UPDATE actresses SET first_name='Ｏｗｎｅｒ',last_name='Ｏｌｄ' WHERE id=101").Error)
				proof := map[string]struct{}{"old owner": {}}
				err := db.Transaction(func(tx *gorm.DB) error {
					return retargetProvenCanonicalAliasesDeferredTx(tx, 100, 100, "Old Owner", "New Owner", proof)
				})
				require.ErrorIs(t, err, ErrActressAliasOwnershipConflict, "all roles/DMMs sharing a canonical representation block retarget")
				require.NoError(t, db.First(&alias, alias.ID).Error)
				require.Equal(t, "Old Owner", alias.CanonicalName)
				require.NoError(t, db.Exec("DELETE FROM actresses WHERE id=101").Error)
				require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
					return retargetProvenCanonicalAliasesDeferredTx(tx, 100, 100, "Old Owner", "New Owner", proof)
				}), "inline alias and candidate NameKey must not constitute canonical ownership")
				require.NoError(t, db.First(&alias, alias.ID).Error)
				require.Equal(t, "New Owner", alias.CanonicalName)
			})
		}
	}
}

func TestCanonicalIndexAuditRetargetIndexFailureRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	owner := models.Actress{DMMID: 98765, JapaneseName: "Old Owner"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &owner))
	alias := models.ActressAlias{AliasName: "Curated", CanonicalName: "Old Owner"}
	require.NoError(t, db.Create(&alias).Error)
	_, err := NewMovieRepository(db).Upsert(t.Context(), creditMovie("audit-index-rollback", []models.MovieCredit{{CreditedName: "Old Owner", Source: "fixture", Scraped: models.Actress{DMMID: owner.DMMID, JapaneseName: "Old Owner"}}}))
	require.NoError(t, err)
	require.NoError(t, db.Transaction(syncActressCanonicalKeysTx))
	type snapshot struct {
		Actresses  []models.Actress
		Aliases    []models.ActressAlias
		Credits    []models.MovieCredit
		Collisions []models.CreditCollision
		Movies     []models.Movie
		Provenance int
	}
	capture := func() snapshot {
		var s snapshot
		require.NoError(t, db.Order("id").Find(&s.Actresses).Error)
		require.NoError(t, db.Order("id").Find(&s.Aliases).Error)
		require.NoError(t, db.Order("id").Find(&s.Credits).Error)
		require.NoError(t, db.Order("id").Find(&s.Collisions).Error)
		require.NoError(t, db.Order("content_id").Find(&s.Movies).Error)
		var err error
		s.Provenance, err = importNameProvenanceTx(db.DB, owner.ID)
		require.NoError(t, err)
		return s
	}
	before := capture()
	require.Equal(t, 1, before.Provenance, "fixture must be managed, not legacy or manually revoked")
	require.NoError(t, db.Exec("CREATE TRIGGER audit_retarget_index_fail BEFORE INSERT ON actress_canonical_keys WHEN NEW.name_key='new owner' BEGIN SELECT RAISE(ABORT, 'audit retarget index failure'); END").Error)
	incoming := models.Actress{ID: owner.ID, DMMID: owner.DMMID, JapaneseName: "New Owner"}
	_, err = repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true})
	require.ErrorContains(t, err, "audit retarget index failure")
	require.Equal(t, before, capture(), "index-write failure must roll back identity, ownership, aliases, credits, collisions and render state")
	require.NoError(t, db.Exec("DROP TRIGGER audit_retarget_index_fail").Error)
	_, err = repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.NoError(t, db.First(&owner, owner.ID).Error)
	require.Equal(t, "New Owner", owner.JapaneseName)
	require.NoError(t, db.First(&alias, alias.ID).Error)
	require.Equal(t, "New Owner", alias.CanonicalName)
	provenance, err := importNameProvenanceTx(db.DB, owner.ID)
	require.NoError(t, err)
	require.Equal(t, 1, provenance)
}

func TestCanonicalIndexAuditUnscopedChunkIsolation(t *testing.T) {
	db := newCreditTestDB(t)
	rows := make([]models.Actress, 600)
	keys := make([]string, 0, 601)
	for i := range rows {
		rows[i] = models.Actress{JapaneseName: fmt.Sprintf("Chunk %d", i)}
		keys = append(keys, fmt.Sprintf("chunk %d", i))
	}
	require.NoError(t, db.CreateInBatches(&rows, 100).Error)
	keys = append(keys, "chunk 0") // Cross-chunk duplicate must not duplicate identity.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		unscoped := tx.Unscoped()
		matches, err := canonicalActressesByKeysTx(unscoped, keys, "")
		require.NoError(t, err)
		require.Len(t, matches, 600)
		// The supplied chained handle must remain usable after queue table, limits,
		// and all chunk-specific predicates have been applied internally.
		var all []models.Actress
		require.NoError(t, unscoped.Find(&all).Error)
		require.Len(t, all, 600)
		return nil
	}))
	var dirty int64
	require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
	require.Zero(t, dirty)
}
