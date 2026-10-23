package scrape

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/catalogname"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/javlibrary"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func nameCatalogFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "names.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE actresses (id INTEGER PRIMARY KEY,first_name TEXT,last_name TEXT,japanese_name TEXT,verified BOOLEAN,origin TEXT,ambiguity_quarantined BOOLEAN);
 CREATE TABLE actress_aliases (alias_name TEXT, canonical_name TEXT);
 INSERT INTO actresses VALUES (1,'Shiori','Yorimoto','依本しおり',1,'user',0),(2,'Jenna','Jameson','ジェナ ジェイムソン',1,'import',0);`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return path
}

func catalogRawFixture() *models.ScraperResult {
	return &models.ScraperResult{Source: "javlibrary", Actresses: []models.ActressInfo{
		{DMMID: 123, FirstName: "Yorimoto", LastName: "Shiori", ThumbURL: "https://example.invalid/thumb.jpg"}, {FirstName: "Jenna", LastName: "Jameson"}, {JapaneseName: "依本しおり"}, {FirstName: "RION"},
	}, CastNameEvidence: []models.CastNameEvidence{
		{SourceID: "aa6sc", DisplayName: "Yorimoto Shiori"}, {SourceID: "jnka", DisplayName: "Jenna Jameson"}, {SourceID: "native", DisplayName: "依本しおり"}, {SourceID: "stage", DisplayName: "RION"},
	}}
}

func TestCatalogNameOutputDoesNotMutateRaw(t *testing.T) {
	snapshot, err := catalogname.Load(context.Background(), nameCatalogFixture(t))
	require.NoError(t, err)
	raw := catalogRawFixture()
	before := raw.Clone()
	out := enrichCatalogNames(raw, snapshot)
	require.Equal(t, before, raw)
	require.Equal(t, "Shiori", out.Enriched.Actresses[0].FirstName)
	require.Equal(t, "Yorimoto", out.Enriched.Actresses[0].LastName)
	require.True(t, out.Decisions[0].Applied)
	require.Equal(t, raw.Actresses[0].DMMID, out.Enriched.Actresses[0].DMMID)
	require.Equal(t, raw.Actresses[0].ThumbURL, out.Enriched.Actresses[0].ThumbURL)
	require.Equal(t, "catalog_consistent", out.Decisions[0].Confidence)
	require.False(t, out.Decisions[1].Applied)
	require.Equal(t, raw.Actresses[2:], out.Enriched.Actresses[2:])
	require.Equal(t, "native_name_preserved", out.Decisions[2].Reason)
	raw.CastNameEvidence[0].ManualOverride = true
	out = enrichCatalogNames(raw, snapshot)
	require.Equal(t, raw.Actresses[0], out.Enriched.Actresses[0])
	require.Equal(t, "manual_override_preserved", out.Decisions[0].Reason)
	raw.CastNameEvidence[0].ManualOverride = false
	raw.CastNameEvidence[0].SourceID = ""
	out = enrichCatalogNames(raw, snapshot)
	require.Equal(t, "missing_source_evidence", out.Decisions[0].Reason)
	raw.CastNameEvidence[0].SourceID = "aa6sc"
	raw.CastNameEvidence[0].DisplayName = "山田 花子"
	out = enrichCatalogNames(raw, snapshot)
	require.Equal(t, "native_name_preserved", out.Decisions[0].Reason, "decoded native evidence must not be enriched even when raw parser saw entities")
	require.Equal(t, raw.Actresses[0], out.Enriched.Actresses[0])
	raw.CastNameEvidence = nil
	out = enrichCatalogNames(raw, snapshot)
	require.Equal(t, raw.Actresses, out.Enriched.Actresses)
	raw = catalogRawFixture()
	raw.Source = "other"
	require.Equal(t, raw.Actresses, enrichCatalogNames(raw, snapshot).Enriched.Actresses)
}

func TestCatalogNamesCLIOptInAndSnapshotRefresh(t *testing.T) {
	path := nameCatalogFixture(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("config_version: 3\nlogging:\n  output: stderr\n"), 0600))
	t.Setenv("JAVINIZER_CONFIG", configPath)
	original := bootstrapQueryOnlyDeps
	t.Cleanup(func() { bootstrapQueryOnlyDeps = original })
	raw := catalogRawFixture()
	bootstrapQueryOnlyDeps = func(*config.Config) (*commandutil.CoreDeps, error) {
		registry := scraperutil.NewScraperRegistry()
		registry.RegisterInstance(&stubScraper{name: "javlibrary", enabled: true, result: raw})
		return &commandutil.CoreDeps{ScraperRegistry: registry}, nil
	}
	run := func(catalog bool) []byte {
		cmd := NewCommand()
		args := []string{"HJMO-732", "--output", "json", "--scrapers", "javlibrary"}
		if catalog {
			args = append(args, "--catalog-names", path)
		}
		cmd.SetArgs(args)
		var b bytes.Buffer
		cmd.SetOut(&b)
		require.NoError(t, cmd.Execute(), b.String())
		return b.Bytes()
	}
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	var plain models.ScraperResult
	require.NoError(t, json.Unmarshal(run(false), &plain))
	require.Equal(t, "Yorimoto", plain.Actresses[0].FirstName)
	var enriched catalogNameOutput
	require.NoError(t, json.Unmarshal(run(true), &enriched))
	require.Equal(t, "Shiori", enriched.Enriched.Actresses[0].FirstName)
	require.Equal(t, "Yorimoto", enriched.Raw.Actresses[0].FirstName)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "enrichment does not change catalog bytes")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO actresses VALUES(3,'Yorimoto','Shiori','別人',1,'user',0)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, json.Unmarshal(run(true), &enriched))
	require.False(t, enriched.Decisions[0].Applied, "new collisions invalidate previous resolution without a persistent cache")
	require.Equal(t, "Yorimoto", enriched.Enriched.Actresses[0].FirstName)
}

func TestCatalogNamesCLIValidation(t *testing.T) {
	for _, args := range [][]string{
		{"X", "--catalog-names", "catalog.db"},
		{"X", "--output", "json", "--scrapers", "javlibrary", "--catalog-names="},
		{"X", "--output", "json", "--scrapers", "r18dev", "--catalog-names", "catalog.db"},
		{"X", "--output", "json", "--scrapers", "javlibrary", "--catalog-names", filepath.Join(t.TempDir(), "missing.db")},
	} {
		cmd := NewCommand()
		cmd.SetArgs(args)
		var b bytes.Buffer
		cmd.SetOut(&b)
		cmd.SetErr(&b)
		require.Error(t, cmd.Execute())
	}
}

func TestCatalogNamesCLIRealSourceFixture(t *testing.T) {
	// Instantiate the real parser directly in the test bootstrap so the
	// production provider-host allowlist remains unchanged for loopback fixtures.
	original := bootstrapQueryOnlyDeps
	t.Cleanup(func() { bootstrapQueryOnlyDeps = original })
	bootstrapQueryOnlyDeps = func(cfg *config.Config) (*commandutil.CoreDeps, error) {
		registry := scraperutil.NewScraperRegistry()
		javlibrary.Register(registry)
		registration, ok := registry.Get("javlibrary")
		require.True(t, ok)
		instance, err := registration.Constructor(scraperutil.ScraperDeps{Settings: *cfg.Scrapers.Overrides["javlibrary"]})
		if err != nil {
			return nil, err
		}
		registry.RegisterInstance(instance)
		return &commandutil.CoreDeps{ScraperRegistry: registry}, nil
	}
	path := nameCatalogFixture(t)
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO actresses VALUES(3,'花子','山田','山田花子',1,'user',0)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		require.Equal(t, "/en/vl_searchbyid.php", r.URL.Path)
		_, _ = w.Write([]byte(`<html><head><title>HJMO-732 Source-shaped fixture - JAVLibrary</title></head><body><div id="video_info"></div>
<span class="star"><a href="vl_star.php?s=aa6sc" rel="tag">Yorimoto Shiori</a></span>
<span class="star"><a href="vl_star.php?s=jnka" rel="tag">Jenna Jameson</a></span>
<span class="star"><a href="vl_star.php?s=native">&#x5c71;&#x7530; &#x82b1;&#x5b50;</a></span></body></html>`))
	}))
	defer server.Close()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configText := "config_version: 3\nlogging:\n  output: stderr\nscrapers:\n  priority: [javlibrary]\n  javlibrary:\n    enabled: true\n    language: en\n    base_url: " + server.URL + "\n"
	require.NoError(t, os.WriteFile(configPath, []byte(configText), 0600))
	t.Setenv("JAVINIZER_CONFIG", configPath)
	for _, override := range []bool{false, true} {
		if override {
			require.NoError(t, os.WriteFile(configPath, []byte(configText+"    actress_name_overrides:\n      aa6sc:\n        source_name: Yorimoto Shiori\n        first_name: Yorimoto\n        last_name: Shiori\n"), 0600))
		}
		cmd := NewCommand()
		cmd.SetArgs([]string{"HJMO-732", "--output", "json", "--scrapers", "javlibrary", "--catalog-names", path})
		var b bytes.Buffer
		cmd.SetOut(&b)
		require.NoError(t, cmd.Execute(), b.String())
		var out catalogNameOutput
		require.NoError(t, json.Unmarshal(b.Bytes(), &out))
		require.Len(t, out.Enriched.Actresses, 3)
		require.Equal(t, "Yorimoto", out.Raw.Actresses[0].FirstName)
		if override {
			require.Equal(t, "Yorimoto", out.Enriched.Actresses[0].FirstName)
			require.Equal(t, "manual_override_preserved", out.Decisions[0].Reason)
		} else {
			require.Equal(t, "Shiori", out.Enriched.Actresses[0].FirstName)
		}
		require.Equal(t, "Jenna", out.Enriched.Actresses[1].FirstName)
		require.Equal(t, out.Raw.Actresses[2], out.Enriched.Actresses[2])
		require.Equal(t, "native_name_preserved", out.Decisions[2].Reason)
	}
	require.EqualValues(t, 2, requests.Load(), "one source request per invocation, no profile enrichment calls")
}
