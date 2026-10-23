// Package catalogname offers optional, read-only name-component enrichment.
// Catalog consistency is not linguistic verification or proof of identity.
package catalogname

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

// Decision contains components only when a unique trusted catalog row agrees
// with the original display. CatalogID is informational, never an identity link.
type Decision struct {
	Resolved   bool
	FirstName  string
	LastName   string
	CatalogID  uint
	Confidence string
	Reason     string
}

type record struct {
	id                            uint
	first, last, japanese, origin string
	verified, quarantined         bool
}
type alias struct{ name, canonical string }

// Snapshot is an immutable value detached from its source database. A zero
// Snapshot is empty and safe for concurrent Resolve calls.
type Snapshot struct {
	canonical      map[string][]record
	aliasOwners    map[string][]record
	invalidAliases map[string]bool
}

// Load reads an existing ordinary SQLite file in one consistent transaction.
// It never initializes a database, migrates schema, repairs indexes, or mutates
// catalog state. WAL is intentionally honored (immutable mode is not used).
// SQLite may use its shared-memory sidecar as part of a coherent WAL read;
// read-only here means no catalog/schema changes, not bypassing SQLite locking.
func Load(ctx context.Context, path string) (*Snapshot, error) {
	if strings.TrimSpace(path) == "" || path == ":memory:" || strings.HasPrefix(strings.ToLower(path), "file:") || strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("catalog path must be an ordinary existing SQLite file")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("catalog path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("catalog file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("catalog path is not a regular file")
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{"mode": {"ro"}, "_query_only": {"1"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, fmt.Errorf("open catalog: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("read catalog: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var records []record
	var aliases []alias
	rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(first_name,''), COALESCE(last_name,''), COALESCE(japanese_name,''), COALESCE(origin,''), COALESCE(verified,0), COALESCE(ambiguity_quarantined,0) FROM actresses`)
	if err != nil {
		return nil, fmt.Errorf("read catalog actresses (current schema required): %w", err)
	}
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.first, &r.last, &r.japanese, &r.origin, &r.verified, &r.quarantined); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("read catalog actress: %w", err)
		}
		records = append(records, r)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("read catalog actresses: %w", err)
	}
	rows, err = tx.QueryContext(ctx, `SELECT COALESCE(alias_name,''), COALESCE(canonical_name,'') FROM actress_aliases`)
	if err != nil {
		return nil, fmt.Errorf("read curated catalog aliases (current schema required): %w", err)
	}
	for rows.Next() {
		var a alias
		if err := rows.Scan(&a.name, &a.canonical); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("read catalog alias: %w", err)
		}
		aliases = append(aliases, a)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("read catalog aliases: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("finish catalog snapshot: %w", err)
	}
	return newSnapshot(records, aliases), nil
}

// newSnapshot builds private indexes once, copying record values rather than
// retaining caller-owned slices. Resolve never changes these indexes.
func newSnapshot(records []record, aliases []alias) *Snapshot {
	s := &Snapshot{canonical: make(map[string][]record), aliasOwners: make(map[string][]record), invalidAliases: make(map[string]bool)}
	for _, r := range records {
		seen := make(map[string]bool)
		for _, k := range representations(r) {
			if k != "" && !seen[k] {
				s.canonical[k] = append(s.canonical[k], r)
				seen[k] = true
			}
		}
	}
	// The alias table has no per-row provenance. Presence does not authorize
	// enrichment: these mappings only add possible owners/blockers. A successful
	// result must independently match the trusted canonical components below.
	for _, a := range aliases {
		name := key(a.name)
		if name == "" {
			continue
		}
		matches := s.canonical[key(a.canonical)]
		if len(matches) == 0 {
			s.invalidAliases[name] = true
		}
		s.aliasOwners[name] = append(s.aliasOwners[name], matches...)
	}
	return s
}

func key(s string) string { return models.NormalizeActressNameKey(s) }
func representations(r record) []string {
	return []string{key(r.first + " " + r.last), key(r.last + " " + r.first), key(r.japanese)}
}

// orderKeys conservatively considers every possible boundary between two name
// components. This preserves internal compound-name order, unlike token reversal.
func orderKeys(display string) map[string]bool {
	parts := strings.Fields(key(display))
	keys := map[string]bool{key(display): true}
	for i := 1; i < len(parts); i++ {
		keys[strings.Join(parts[i:], " ")+" "+strings.Join(parts[:i], " ")] = true
	}
	delete(keys, "")
	return keys
}

// Resolve never guesses a split. All canonical and curated-alias owners are
// unioned before eligibility is considered, including unverified candidates.
// Ordinary actresses.aliases metadata is deliberately not loaded or trusted.
func (s *Snapshot) Resolve(display string) Decision {
	fail := func(reason string) Decision { return Decision{Confidence: "none", Reason: reason} }
	target := key(display)
	if target == "" || s == nil {
		return fail("unknown")
	}
	owners := make(map[uint]record)
	for _, r := range s.canonical[target] {
		owners[r.id] = r
	}
	invalidAlias := false
	for lookup := range orderKeys(display) {
		if s.invalidAliases[lookup] {
			invalidAlias = true
		}
		for _, r := range s.aliasOwners[lookup] {
			owners[r.id] = r
		}
	}
	if invalidAlias {
		return fail("invalid_alias")
	}
	if len(owners) > 1 {
		return fail("ambiguous")
	}
	if len(owners) == 0 {
		return fail("unknown")
	}
	for _, r := range owners {
		if r.quarantined {
			return fail("quarantined")
		}
		if !r.verified {
			return fail("candidate")
		}
		if r.origin != "user" && r.origin != "import" {
			return fail("untrusted_origin")
		}
		if key(r.first) == "" || key(r.last) == "" {
			return fail("no_complete_split")
		}
		if target != key(r.first+" "+r.last) && target != key(r.last+" "+r.first) {
			return fail("alias_name_mismatch")
		}
		return Decision{Resolved: true, FirstName: r.first, LastName: r.last, CatalogID: r.id, Confidence: "catalog_consistent", Reason: "matched"}
	}
	return fail("unknown")
}
