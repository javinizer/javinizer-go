package config

import (
	"encoding/json"
	"strings"
)

// NFOConfig groups its settings into named sub-structs (Feature, Format,
// Extra) using yaml.",inline" so the YAML layout stays flat under "nfo:"
// (nfo.enabled, nfo.first_name_order, …) while the Go type system provides
// named access groups (n.Feature.Enabled, n.Format.FirstNameOrder, …).
//
// yaml.",inline" flattens the YAML representation, but encoding/json does NOT
// inline named fields — only embedded (anonymous) fields are inlined.
// Without these methods the JSON wire format would nest the groups
// ({"Feature": {...}, "Format": {...}, "Extra": {...}}), which mismatches the
// flat YAML layout and the frontend's flat NFOConfig type. The result was that
// every nfo.* field (enabled, first_name_order, actress_language_ja,
// unknown_actress_mode, …) deserialized to undefined on the WebUI, leaving NFO
// checkboxes unchecked regardless of config.yaml and silently dropping saves.
//
// MarshalJSON/UnmarshalJSON collapse the three sub-structs into a single flat
// object that matches the YAML layout and the frontend contract. The Go API
// (n.Feature.*, n.Format.*, n.Extra.*) is unchanged.

// MarshalJSON flattens the three NFO sub-structs into one JSON object.
func (n NFOConfig) MarshalJSON() ([]byte, error) {
	type flat struct {
		NFOFeatureConfig
		NFOFormatConfig
		NFOExtraConfig
	}
	return json.Marshal(flat{
		NFOFeatureConfig: n.Feature,
		NFOFormatConfig:  n.Format,
		NFOExtraConfig:   n.Extra,
	})
}

// UnmarshalJSON accepts the flat JSON shape emitted by MarshalJSON (and used
// by the WebUI). It also tolerates the legacy nested shape
// ({"Feature": {...}, …}) for backward compatibility. Each sub-struct's JSON
// tags are distinct, so unmarshaling the same flat bytes into every sub-struct
// populates only that group's fields.
func (n *NFOConfig) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	if _, nested := probe["Feature"]; nested {
		type nestedForm struct {
			Feature NFOFeatureConfig `json:"Feature"`
			Format  NFOFormatConfig  `json:"Format"`
			Extra   NFOExtraConfig   `json:"Extra"`
		}
		var v nestedForm
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*n = NFOConfig(v)
		n.applyDocumentedTrueDefaults(probe)
		return nil
	}

	if err := json.Unmarshal(data, &n.Feature); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &n.Format); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &n.Extra); err != nil {
		return err
	}
	n.applyDocumentedTrueDefaults(probe)
	return nil
}

// documentedTrueFeatureDefaults lists NFO feature toggles whose documented
// default is true. The config API decodes a fresh NFOConfig, so a payload that
// omits them would otherwise persist the Go zero value (false) and silently
// change behaviour for clients that predate the key.
var documentedTrueFeatureDefaults = []struct {
	key    string
	target func(*NFOFeatureConfig) *bool
}{
	{key: "include_fanart", target: func(f *NFOFeatureConfig) *bool { return &f.IncludeFanart }},
	{key: "include_trailer", target: func(f *NFOFeatureConfig) *bool { return &f.IncludeTrailer }},
	{key: "include_actress_images", target: func(f *NFOFeatureConfig) *bool { return &f.IncludeActressImages }},
}

// applyDocumentedTrueDefaults restores the documented default for every toggle
// the JSON payload omitted. An explicit false is always honoured.
func (n *NFOConfig) applyDocumentedTrueDefaults(probe map[string]json.RawMessage) {
	for _, entry := range documentedTrueFeatureDefaults {
		if jsonPayloadHasKey(probe, entry.key) {
			continue
		}
		*entry.target(&n.Feature) = true
	}
}

// jsonPayloadHasKey reports whether a key appears at the top level of the flat
// payload or inside one of the legacy grouped objects.
func jsonPayloadHasKey(probe map[string]json.RawMessage, key string) bool {
	if raw, ok := probe[key]; ok && !isJSONNull(raw) {
		return true
	}
	for _, raw := range probe {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err != nil {
			continue
		}
		if nestedRaw, ok := nested[key]; ok && !isJSONNull(nestedRaw) {
			return true
		}
	}
	return false
}

// isJSONNull reports an explicit JSON null, which callers treat as "not
// specified" so the documented default still applies.
func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
