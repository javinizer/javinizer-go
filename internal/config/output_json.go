package config

import (
	"encoding/json"
)

// OutputConfig groups its settings into named sub-structs using yaml:",inline"
// so the YAML format stays flat (output.folder_format, output.download_cover,
// …) while the Go type system provides named access groups
// (cfg.Output.Template.FolderFormat, cfg.Output.Download.DownloadCover, …).
//
// yaml.",inline" flattens the YAML representation, but encoding/json does NOT
// inline named fields — only embedded (anonymous) fields are inlined. Without
// these methods the JSON wire format would nest the groups
// ({"Template": {...}, "Download": {...}, …}), which mismatches both the flat
// YAML layout and the frontend's flat OutputConfig type. The result was that
// output.download_* (and every other output field) deserialized to undefined
// on the WebUI, leaving download checkboxes unchecked and breaking saves.
//
// MarshalJSON/UnmarshalJSON collapse the four sub-structs into a single flat
// object that matches the YAML layout and the frontend contract. The Go API
// (cfg.Output.Template.*, cfg.Output.Download.*) is unchanged.

// MarshalJSON flattens the four output sub-structs into one JSON object.
func (o OutputConfig) MarshalJSON() ([]byte, error) {
	type flat struct {
		OutputTemplateConfig
		OutputOperationConfig
		OutputMediaFormatConfig
		OutputDownloadConfig
	}
	return json.Marshal(flat{
		OutputTemplateConfig:    o.Template,
		OutputOperationConfig:   o.Operation,
		OutputMediaFormatConfig: o.MediaFormat,
		OutputDownloadConfig:    o.Download,
	})
}

// UnmarshalJSON accepts the flat JSON shape emitted by MarshalJSON (and used
// by the WebUI). It also tolerates the legacy nested shape
// ({"Template": {...}, …}) for backward compatibility with any cached client
// payloads. Each sub-struct's JSON tags are distinct, so unmarshaling the same
// flat bytes into every sub-struct populates only that group's fields.
func (o *OutputConfig) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	if _, nested := probe["Template"]; nested {
		type nestedForm struct {
			Template    OutputTemplateConfig    `json:"Template"`
			Operation   OutputOperationConfig   `json:"Operation"`
			MediaFormat OutputMediaFormatConfig `json:"MediaFormat"`
			Download    OutputDownloadConfig    `json:"Download"`
		}
		var n nestedForm
		if err := json.Unmarshal(data, &n); err != nil {
			return err
		}
		*o = OutputConfig(n)
		return nil
	}

	// Legacy alias: "delimiter" was the pre-rename key for actress_delimiter
	// (still accepted in YAML via LegacyDelimiter + normalize). json:"-" keeps
	// LegacyDelimiter out of JSON entirely, so without this remap a JSON
	// payload carrying the legacy key would be silently dropped. Honor it
	// only when the canonical actress_delimiter is absent so an explicit
	// canonical value always wins (and a stray legacy "" cannot wipe it).
	if _, hasCanonical := probe["actress_delimiter"]; !hasCanonical {
		if raw, hasLegacy := probe["delimiter"]; hasLegacy {
			probe["actress_delimiter"] = raw
			delete(probe, "delimiter")
			data, _ = json.Marshal(probe)
		}
	}

	if err := json.Unmarshal(data, &o.Template); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &o.Operation); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &o.MediaFormat); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &o.Download); err != nil {
		return err
	}
	return nil
}
