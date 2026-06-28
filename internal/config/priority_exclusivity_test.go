package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestPerFieldOverride_PresentEmptyVsAbsent locks in the raw-accessor contract
// for the "is there an override?" question: a PRESENT key (even with an
// explicit empty slice) returns a non-nil value, while an ABSENT key returns
// nil. This is the RAW accessor — it does NOT decide resolution. Resolution
// sites (GetFieldPriority, aggregator.resolvePriorities) use `len(fp) > 0`, so
// a present-empty [] inherits the global priority (commit 9f882f22's documented
// intent: "[] still means 'inherit global'"); deliberate suppression uses the
// ["__skip__"] sentinel. PerFieldOverride is kept as a raw accessor so callers
// can distinguish "explicitly stored []" from "absent" if needed (e.g. for
// diagnostics/UI), even though both now resolve to inherit. A nil slice stored
// under a present key is treated as nil (inherit), matching the
// null/undefined = inherit contract.
func TestPerFieldOverride_PresentEmptyVsAbsent(t *testing.T) {
	p := &PriorityConfig{
		Priority: []string{"r18dev", "dmm"},
		Fields: map[string][]string{
			"series": {},      // present empty (deliberate empty field)
			"title":  {"dmm"}, // present non-empty
			"genre":  nil,     // present nil (null/undefined ⇒ inherit)
			// "maker" absent
		},
	}

	// Present empty []string{} → non-nil empty slice (deliberate empty field).
	series := p.PerFieldOverride("series")
	require.NotNil(t, series, "present [] must be non-nil so callers honor it as an override")
	assert.Equal(t, []string{}, series)
	assert.Len(t, series, 0)

	// Present non-empty → returned as-is.
	assert.Equal(t, []string{"dmm"}, p.PerFieldOverride("title"))

	// Present nil → returns nil (null/undefined ⇒ inherit global).
	assert.Nil(t, p.PerFieldOverride("genre"), "present nil slice ⇒ nil (inherit), same as absent")

	// Absent key → nil (no override, inherit global).
	assert.Nil(t, p.PerFieldOverride("maker"), "absent key ⇒ nil (inherit global)")

	// Nil receiver → nil (no override).
	var nilP *PriorityConfig
	assert.Nil(t, nilP.PerFieldOverride("series"))
}

// TestPriorityConfig_EmptySliceRoundTrip_JSON proves a present `[]` survives the
// JSON MarshalJSON/UnmarshalJSON path used by the config API (PUT /config). An
// empty list that silently reverts to absent would make "Remove all" + Save
// indistinguishable from "inherit" — the bug this round-trip guards against.
func TestPriorityConfig_EmptySliceRoundTrip_JSON(t *testing.T) {
	p := PriorityConfig{
		Priority: []string{"r18dev", "dmm"},
		Fields: map[string][]string{
			"series": {},      // deliberate empty field
			"title":  {"dmm"}, // non-empty override
		},
	}

	data, err := json.Marshal(p)
	require.NoError(t, err)
	t.Logf("marshaled JSON: %s", data)
	// The present-empty key must be emitted as [] (not dropped).
	assert.Contains(t, string(data), `"series":[]`)

	var rt PriorityConfig
	require.NoError(t, json.Unmarshal(data, &rt))

	// Present empty survives as a non-nil []string{} (override still present).
	series, ok := rt.Fields["series"]
	require.True(t, ok, "series key must survive the JSON round-trip")
	require.NotNil(t, series, "present [] must remain a non-nil empty slice, not nil (nil ⇒ inherit)")
	assert.Equal(t, []string{}, series)

	// And it resolves as the inherited global priority (present [] ⇒ inherit,
	// NOT "consult no scrapers").
	assert.Equal(t, []string{"r18dev", "dmm"}, rt.GetFieldPriority("series"))
	assert.NotNil(t, rt.GetFieldPriority("series"), "present [] inherits global (non-nil), not an empty override")

	// Non-empty override survives too.
	assert.Equal(t, []string{"dmm"}, rt.GetFieldPriority("title"))

	// Absent key still inherits global.
	assert.Equal(t, []string{"r18dev", "dmm"}, rt.GetFieldPriority("maker"))
}

// TestPriorityConfig_EmptySliceRoundTrip_YAML proves a present `[]` survives the
// YAML marshal/unmarshal path (the config.yaml file format). yaml.v3 emits a
// non-nil empty slice as `[]` and decodeFromMap restores it as a non-nil
// []string{} — so the on-disk config round-trips a deliberate empty field.
func TestPriorityConfig_EmptySliceRoundTrip_YAML(t *testing.T) {
	p := PriorityConfig{
		Priority: []string{"r18dev", "dmm"},
		Fields: map[string][]string{
			"series": {},      // deliberate empty field
			"title":  {"dmm"}, // non-empty override
		},
	}

	data, err := yaml.Marshal(p)
	require.NoError(t, err)
	t.Logf("marshaled YAML:\n%s", data)
	assert.Contains(t, string(data), "series: []")

	var rt PriorityConfig
	require.NoError(t, yaml.Unmarshal(data, &rt))

	series, ok := rt.Fields["series"]
	require.True(t, ok, "series key must survive the YAML round-trip")
	require.NotNil(t, series, "present [] must remain a non-nil empty slice after YAML round-trip")
	assert.Equal(t, []string{}, series)
	// Present [] inherits global (commit 9f882f22: "[] still means 'inherit global'").
	assert.Equal(t, []string{"r18dev", "dmm"}, rt.GetFieldPriority("series"))
}

// TestConfig_EmptySliceRoundTrip_SaveLoad exercises the REAL persistence path
// (config.Save → config.yaml → config.Load) to prove a deliberate empty
// per-field override survives an actual save+reload of the whole config. This
// is the end-to-end guard for the "Remove all" + Save bug: if Save dropped the
// empty slice (or merged it away), reloading would show "Inherited" again.
func TestConfig_EmptySliceRoundTrip_SaveLoad(t *testing.T) {
	cfg := &Config{
		ConfigVersion: CurrentConfigVersion,
		Scrapers: ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
		Metadata: MetadataConfig{
			Priority: PriorityConfig{
				Priority: []string{"r18dev", "dmm"},
				Fields: map[string][]string{
					"series": {},      // deliberate empty field
					"title":  {"dmm"}, // non-empty override
				},
			},
		},
	}

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	require.NoError(t, Save(cfg, cfgPath))

	// The on-disk YAML must contain the present-empty override as `series: []`.
	onDisk, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	t.Logf("on-disk config.yaml:\n%s", onDisk)
	assert.Contains(t, string(onDisk), "series: []")

	loaded, err := Load(cfgPath)
	require.NoError(t, err)

	series, ok := loaded.Metadata.Priority.Fields["series"]
	require.True(t, ok, "series override must survive Save+Load")
	require.NotNil(t, series, "present [] must reload as a non-nil empty slice (nil ⇒ inherit)")
	assert.Equal(t, []string{}, series)

	// Effective priority for series inherits global (present [] ⇒ inherit, not
	// a deliberate empty field).
	assert.Equal(t, []string{"r18dev", "dmm"}, loaded.Metadata.Priority.GetFieldPriority("series"))

	// Non-empty override and global priority survive too.
	assert.Equal(t, []string{"dmm"}, loaded.Metadata.Priority.GetFieldPriority("title"))
	assert.Equal(t, []string{"r18dev", "dmm"}, loaded.Metadata.Priority.GetFieldPriority("maker"))
}
