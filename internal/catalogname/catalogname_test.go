package catalogname

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func trusted(id uint, first, last string) record {
	return record{id: id, first: first, last: last, verified: true, origin: "user"}
}

func TestResolveFailClosed(t *testing.T) {
	base := trusted(1, "Yui", "Hatano")
	candidate := trusted(2, "Hatano", "Yui")
	candidate.verified = false
	candidate.origin = "scrape"
	quarantined := base
	quarantined.quarantined = true
	unknownOrigin := base
	unknownOrigin.origin = "legacy"
	scraped := base
	scraped.origin = "scrape"
	imported := base
	imported.origin = "import"
	native := base
	native.japanese = "波多野結衣"
	tests := []struct {
		name, display, reason string
		records               []record
		aliases               []alias
		resolved              bool
	}{
		{name: "given first", display: "Yui Hatano", records: []record{base}, resolved: true},
		{name: "family first", display: "Hatano Yui", records: []record{base}, resolved: true},
		{name: "normalization", display: "  ＹＵＩ　Hatano ", records: []record{base}, resolved: true},
		{name: "import", display: "Hatano Yui", records: []record{imported}, resolved: true},
		{name: "both orders are ambiguous", display: "Hatano Yui", records: []record{base, trusted(2, "Hatano", "Yui")}, reason: "ambiguous"},
		{name: "candidate blocks verified", display: "Hatano Yui", records: []record{base, candidate}, reason: "ambiguous"},
		{name: "candidate alone", display: "Hatano Yui", records: []record{candidate}, reason: "candidate"},
		{name: "quarantine", display: "Hatano Yui", records: []record{quarantined}, reason: "quarantined"},
		{name: "unknown provenance", display: "Hatano Yui", records: []record{unknownOrigin}, reason: "untrusted_origin"},
		{name: "scrape provenance", display: "Hatano Yui", records: []record{scraped}, reason: "untrusted_origin"},
		{name: "native display not renamed", display: "波多野結衣", records: []record{native}, reason: "alias_name_mismatch"},
		{name: "mononym not split", display: "Yui", records: []record{trusted(1, "Yui", "")}, reason: "no_complete_split"},
		{name: "unknown", display: "Someone Else", records: []record{base}, reason: "unknown"},
		{name: "empty", display: " ", records: []record{base}, reason: "unknown"},
		{name: "alias alone not rename", display: "Stage Name", records: []record{base}, aliases: []alias{{"Stage Name", "Hatano Yui"}}, reason: "alias_name_mismatch"},
		{name: "same owner deduplicated", display: "Hatano Yui", records: []record{base}, aliases: []alias{{"Yui Hatano", "Yui Hatano"}, {"Hatano Yui", "Hatano Yui"}}, resolved: true},
		{name: "alias owner blocks", display: "Hatano Yui", records: []record{base, trusted(2, "Other", "Person")}, aliases: []alias{{"Hatano Yui", "Person Other"}}, reason: "ambiguous"},
		{name: "reverse alias owner blocks", display: "Hatano Yui", records: []record{base, trusted(2, "Other", "Person")}, aliases: []alias{{"Yui Hatano", "Person Other"}}, reason: "ambiguous"},
		{name: "all alias owner orders counted", display: "Hatano Yui", records: []record{base, trusted(2, "Other", "Person"), trusted(3, "Person", "Other")}, aliases: []alias{{"Hatano Yui", "Person Other"}}, reason: "ambiguous"},
		{name: "dangling alias blocks", display: "Hatano Yui", records: []record{base}, aliases: []alias{{"Hatano Yui", "Missing Person"}}, reason: "invalid_alias"},
		{name: "compound components preserved", display: "Van Dyke Mary Jane", records: []record{trusted(1, "Mary Jane", "Van Dyke")}, resolved: true},
		{name: "compound swapped alias blocks", display: "Van Dyke Mary Jane", records: []record{trusted(1, "Mary Jane", "Van Dyke"), trusted(2, "Other", "Person")}, aliases: []alias{{"Mary Jane Van Dyke", "Person Other"}}, reason: "ambiguous"},
		{name: "native complete components", display: "山田 花子", records: []record{trusted(1, "花子", "山田")}, resolved: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSnapshot(tt.records, tt.aliases)
			got := s.Resolve(tt.display)
			require.Equal(t, tt.resolved, got.Resolved)
			if tt.resolved {
				require.Equal(t, "catalog_consistent", got.Confidence)
				require.Equal(t, tt.records[0].first, got.FirstName)
				require.Equal(t, tt.records[0].last, got.LastName)
				require.Equal(t, uint(1), got.CatalogID)
			} else {
				require.Equal(t, tt.reason, got.Reason)
				require.Equal(t, "none", got.Confidence)
				require.Empty(t, got.FirstName)
				require.Empty(t, got.LastName)
				require.Zero(t, got.CatalogID)
			}
		})
	}
	require.False(t, (*Snapshot)(nil).Resolve("name").Resolved)
	require.False(t, (&Snapshot{}).Resolve("name").Resolved)
}

func fixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite3", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE actresses(id INTEGER PRIMARY KEY, first_name TEXT, last_name TEXT, japanese_name TEXT, origin TEXT, verified INTEGER, ambiguity_quarantined INTEGER, aliases TEXT);
 CREATE TABLE actress_aliases(alias_name TEXT, canonical_name TEXT);
 INSERT INTO actresses VALUES(1,'Yui','Hatano','','user',1,0,'Stage Name|Other Person');`)
	require.NoError(t, err)
	return db
}

func TestLoadReadOnlyDetachedSnapshot(t *testing.T) {
	// URI punctuation is literal filename data, not SQLite query parameters.
	filename := "catalog #%.db"
	if runtime.GOOS != "windows" {
		filename = "catalog ?mode=rw#%.db"
	}
	path := filepath.Join(t.TempDir(), filename)
	db := fixture(t, path)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	s, err := Load(context.Background(), path)
	require.NoError(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.True(t, s.Resolve("Hatano Yui").Resolved)
	require.Equal(t, "unknown", s.Resolve("Stage Name").Reason, "ordinary aliases are not trusted mappings")
	_, err = db.Exec("UPDATE actresses SET first_name='Changed'")
	require.NoError(t, err)
	require.True(t, s.Resolve("Hatano Yui").Resolved, "snapshot must not hold a live lookup")
	newer, err := Load(context.Background(), path)
	require.NoError(t, err)
	require.False(t, newer.Resolve("Hatano Yui").Resolved, "there is no cross-load cache")
}

func TestLoadHonorsWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db := fixture(t, path)
	_, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; UPDATE actresses SET first_name='Committed'")
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec("UPDATE actresses SET first_name='Uncommitted'")
	require.NoError(t, err)
	s, err := Load(context.Background(), path)
	require.NoError(t, err)
	require.True(t, s.Resolve("Committed Hatano").Resolved)
	require.False(t, s.Resolve("Uncommitted Hatano").Resolved)
	require.NoError(t, tx.Rollback())
}

func TestLoadRejectsMissingAndLegacyWithoutWrites(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.db")
	for _, path := range []string{"", ":memory:", "file:catalog.db?mode=rw", missing, dir} {
		_, err := Load(context.Background(), path)
		require.Error(t, err)
	}
	_, err := os.Stat(missing)
	require.True(t, os.IsNotExist(err))
	legacy := filepath.Join(dir, "legacy.db")
	db, err := sql.Open("sqlite3", legacy)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE actresses(id INTEGER PRIMARY KEY, first_name TEXT)")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	before, err := os.ReadFile(legacy)
	require.NoError(t, err)
	_, err = Load(context.Background(), legacy)
	require.Error(t, err)
	after, err := os.ReadFile(legacy)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestSnapshotCopiesInput(t *testing.T) {
	records := []record{trusted(1, "Yui", "Hatano")}
	aliases := []alias{{"Yui Hatano", "Hatano Yui"}}
	s := newSnapshot(records, aliases)
	records[0].first = "Changed"
	aliases[0].canonical = "Missing"
	require.True(t, s.Resolve("Yui Hatano").Resolved)
}
