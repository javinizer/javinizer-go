package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOutputConfigMarshalJSON_Flat verifies OutputConfig serializes to a flat
// JSON object (no Template/Operation/MediaFormat/Download nesting), matching
// the YAML layout and the frontend OutputConfig type.
func TestOutputConfigMarshalJSON_Flat(t *testing.T) {
	o := OutputConfig{
		Template:    OutputTemplateConfig{FolderFormat: "<ID>", SubfolderFormat: []string{"<ID>"}, ActressDelimiter: ", ", MaxTitleLength: 100},
		Operation:   OutputOperationConfig{RenameFile: true, SubtitleExtensions: []string{".srt"}},
		MediaFormat: OutputMediaFormatConfig{PosterFormat: "<ID>-poster.jpg", ScreenshotPadding: 1},
		Download:    OutputDownloadConfig{DownloadCover: true, DownloadPoster: false, DownloadTimeout: 60},
	}
	b, err := json.Marshal(&o)
	require.NoError(t, err)
	s := string(b)

	for _, key := range []string{"Template", "Operation", "MediaFormat", "Download"} {
		assert.NotContains(t, s, "\""+key+"\"", "JSON should not nest group %q", key)
	}
	// Decode back and assert on values (robust to Go's HTML escaping of </>).
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	for _, key := range []string{"folder_format", "file_format", "actress_delimiter", "max_title_length", "rename_file", "subtitle_extensions", "poster_format", "screenshot_padding", "download_cover", "download_poster", "download_timeout", "subfolder_format"} {
		assert.Contains(t, m, key, "flat JSON missing key %q", key)
	}
	var ff string
	require.NoError(t, json.Unmarshal(m["folder_format"], &ff))
	assert.Equal(t, "<ID>", ff)
	var dc bool
	require.NoError(t, json.Unmarshal(m["download_cover"], &dc))
	assert.True(t, dc)
	var dp bool
	require.NoError(t, json.Unmarshal(m["download_poster"], &dp))
	assert.False(t, dp)
	var sf []string
	require.NoError(t, json.Unmarshal(m["subfolder_format"], &sf))
	assert.Equal(t, []string{"<ID>"}, sf)
}

// TestOutputConfigUnmarshalJSON_Flat verifies the flat JSON shape round-trips
// back into the grouped sub-struct fields.
func TestOutputConfigUnmarshalJSON_Flat(t *testing.T) {
	raw := `{"folder_format":"<ID>","file_format":"<ID>.mp4","actress_delimiter":", ","max_title_length":100,"rename_file":true,"subtitle_extensions":[".srt",".ass"],"poster_format":"<ID>-poster.jpg","screenshot_padding":1,"download_cover":true,"download_poster":false,"download_extrafanart":true,"download_trailer":true,"download_actress":true,"download_timeout":60}`
	var o OutputConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &o))

	assert.Equal(t, "<ID>", o.Template.FolderFormat)
	assert.Equal(t, "<ID>.mp4", o.Template.FileFormat)
	assert.Equal(t, ", ", o.Template.ActressDelimiter)
	assert.Equal(t, 100, o.Template.MaxTitleLength)
	assert.True(t, o.Operation.RenameFile)
	assert.Equal(t, []string{".srt", ".ass"}, o.Operation.SubtitleExtensions)
	assert.Equal(t, "<ID>-poster.jpg", o.MediaFormat.PosterFormat)
	assert.Equal(t, 1, o.MediaFormat.ScreenshotPadding)
	assert.True(t, o.Download.DownloadCover)
	assert.False(t, o.Download.DownloadPoster)
	assert.True(t, o.Download.DownloadExtrafanart)
	assert.Equal(t, 60, o.Download.DownloadTimeout)
}

// TestOutputConfigUnmarshalJSON_NestedLegacy verifies the legacy nested shape
// ({"Template": {...}, ...}) produced before this fix is still accepted.
func TestOutputConfigUnmarshalJSON_NestedLegacy(t *testing.T) {
	raw := `{"Template":{"folder_format":"X","actress_delimiter":"; "},"Operation":{"rename_file":false},"MediaFormat":{"poster_format":"p.jpg"},"Download":{"download_cover":false,"download_poster":true,"download_extrafanart":true,"download_trailer":true,"download_actress":true,"download_timeout":30}}`
	var o OutputConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &o))

	assert.Equal(t, "X", o.Template.FolderFormat)
	assert.Equal(t, "; ", o.Template.ActressDelimiter)
	assert.False(t, o.Operation.RenameFile)
	assert.Equal(t, "p.jpg", o.MediaFormat.PosterFormat)
	assert.False(t, o.Download.DownloadCover)
	assert.True(t, o.Download.DownloadPoster)
	assert.Equal(t, 30, o.Download.DownloadTimeout)
}

// TestOutputConfigUnmarshalJSON_LegacyDelimiterAlias verifies the bare legacy
// "delimiter" key is honored as an alias for actress_delimiter on JSON input,
// mirroring the YAML LegacyDelimiter behavior. Canonical wins on conflict.
func TestOutputConfigUnmarshalJSON_LegacyDelimiterAlias(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"legacy-only maps to canonical", `{"delimiter": "; "}`, "; "},
		{"canonical wins over legacy", `{"delimiter": "; ", "actress_delimiter": ", "}`, ", "},
		{"canonical-only", `{"actress_delimiter": " / "}`, " / "},
		{"neither leaves zero value", `{}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var o OutputConfig
			require.NoError(t, json.Unmarshal([]byte(c.raw), &o))
			assert.Equal(t, c.want, o.Template.ActressDelimiter)
		})
	}
}

// TestOutputConfigMarshalJSON_OmitsLegacyDelimiter verifies MarshalJSON emits
// only the canonical actress_delimiter, never the legacy key.
func TestOutputConfigMarshalJSON_OmitsLegacyDelimiter(t *testing.T) {
	o := OutputConfig{Template: OutputTemplateConfig{ActressDelimiter: ", "}}
	b, err := json.Marshal(&o)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Contains(t, m, "actress_delimiter")
	assert.NotContains(t, m, "delimiter")
}

// TestOutputConfigJSON_RoundTrip verifies marshal→unmarshal preserves all
// fields across the four sub-structs.
func TestOutputConfigJSON_RoundTrip(t *testing.T) {
	original := OutputConfig{
		Template:    OutputTemplateConfig{FolderFormat: "<ID>", FileFormat: "<ID>.mp4", SubfolderFormat: []string{"<ID>"}, ActressDelimiter: ", ", MaxTitleLength: 100, MaxPathLength: 240, FirstNameOrder: true, ActressLanguageJA: false},
		Operation:   OutputOperationConfig{OperationMode: "move", RenameFile: true, AllowRevert: false, GroupActress: true, GroupActressName: "@Group", MoveSubtitles: true, SubtitleExtensions: []string{".srt", ".ass"}},
		MediaFormat: OutputMediaFormatConfig{PosterFormat: "<ID>-poster.jpg", MaxPosterHeight: 0, FanartFormat: "<ID>-fanart.jpg", TrailerFormat: "<ID>-trailer.mp4", ScreenshotFormat: "fanart<INDEX>.jpg", ScreenshotFolder: "extrafanart", ScreenshotPadding: 1, ActressFolder: ".actors", ActressFormat: "<ACTORNAME>.jpg"},
		Download:    OutputDownloadConfig{DownloadCover: true, DownloadPoster: true, DownloadExtrafanart: false, DownloadTrailer: true, DownloadActress: false, DownloadTimeout: 90},
	}
	b, err := json.Marshal(&original)
	require.NoError(t, err)
	var restored OutputConfig
	require.NoError(t, json.Unmarshal(b, &restored))

	assert.Equal(t, original.Template, restored.Template)
	assert.Equal(t, original.Operation, restored.Operation)
	assert.Equal(t, original.MediaFormat, restored.MediaFormat)
	assert.Equal(t, original.Download, restored.Download)
}

// TestOutputConfigUnmarshalJSON_InvalidJSON verifies invalid input surfaces an
// error from UnmarshalJSON. Two cases: (1) syntactically invalid JSON is
// rejected by json.Unmarshal's own checkValid before UnmarshalJSON runs;
// (2) valid JSON of the wrong type (array) reaches UnmarshalJSON and fails
// the probe unmarshal into map[string]json.RawMessage, exercising the
// top-level error-return branch.
func TestOutputConfigUnmarshalJSON_InvalidJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"malformed json", "{not json"},
		{"valid json wrong type (array)", "[]"},
		{"valid json wrong type (number)", "42"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var o OutputConfig
			assert.Error(t, json.Unmarshal([]byte(c.raw), &o))
		})
	}
}

// TestOutputConfigUnmarshalJSON_NestedPathError verifies a malformed nested
// payload (Template present but not an object) errors via the nested branch.
func TestOutputConfigUnmarshalJSON_NestedPathError(t *testing.T) {
	var o OutputConfig
	err := json.Unmarshal([]byte(`{"Template":"not-an-object"}`), &o)
	assert.Error(t, err)
}

// TestOutputConfigUnmarshalJSON_FlatSubStructErrors verifies type-mismatched
// values in each flat sub-struct field surface an error from every per-group
// unmarshal branch (Template/Operation/MediaFormat/Download).
func TestOutputConfigUnmarshalJSON_FlatSubStructErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"template field wrong type", `{"max_title_length":"not-int"}`},
		{"operation field wrong type", `{"rename_file":"not-bool"}`},
		{"mediaformat field wrong type", `{"screenshot_padding":"not-int"}`},
		{"download field wrong type", `{"download_timeout":"not-int"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var o OutputConfig
			err := json.Unmarshal([]byte(c.raw), &o)
			assert.Error(t, err)
		})
	}
}

// TestNFOConfigMarshalJSON_Flat verifies NFOConfig serializes to a flat JSON
// object (no Feature/Format/Extra nesting), matching the frontend NFOConfig.
func TestNFOConfigMarshalJSON_Flat(t *testing.T) {
	n := NFOConfig{
		Feature: NFOFeatureConfig{Enabled: true, PerFile: false, IncludeFanart: true, IncludeTrailer: false, IncludeStreamDetails: true, IncludeOriginalPath: false, ActressAsTag: true, AddGenericRole: false, AltNameRole: true},
		Format:  NFOFormatConfig{DisplayTitle: "<TITLE>", FilenameTemplate: "<ID>.nfo", FirstNameOrder: true, ActressLanguageJA: false, RatingSource: "r18dev", Tagline: "tl", UnknownActressMode: "skip", UnknownActressText: "Unknown"},
		Extra:   NFOExtraConfig{Tag: []string{"a", "b"}, Credits: []string{"c"}},
	}
	b, err := json.Marshal(&n)
	require.NoError(t, err)
	s := string(b)

	for _, key := range []string{"Feature", "Format", "Extra"} {
		assert.NotContains(t, s, "\""+key+"\"", "JSON should not nest group %q", key)
	}
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))
	for _, key := range []string{"enabled", "first_name_order", "unknown_actress_mode", "tag", "credits", "include_fanart", "actress_as_tag", "alt_name_role", "display_title", "filename_template"} {
		assert.Contains(t, m, key, "flat JSON missing key %q", key)
	}
	var en bool
	require.NoError(t, json.Unmarshal(m["enabled"], &en))
	assert.True(t, en)
	var fno bool
	require.NoError(t, json.Unmarshal(m["first_name_order"], &fno))
	assert.True(t, fno)
	var uam string
	require.NoError(t, json.Unmarshal(m["unknown_actress_mode"], &uam))
	assert.Equal(t, "skip", uam)
	var tag []string
	require.NoError(t, json.Unmarshal(m["tag"], &tag))
	assert.Equal(t, []string{"a", "b"}, tag)
	var credits []string
	require.NoError(t, json.Unmarshal(m["credits"], &credits))
	assert.Equal(t, []string{"c"}, credits)
}

// TestNFOConfigUnmarshalJSON_Flat verifies the flat JSON shape round-trips
// back into the grouped sub-struct fields.
func TestNFOConfigUnmarshalJSON_Flat(t *testing.T) {
	raw := `{"enabled":true,"per_file":false,"include_fanart":true,"include_trailer":false,"include_stream_details":true,"include_originalpath":false,"actress_as_tag":true,"add_generic_role":false,"alt_name_role":true,"display_title":"<TITLE>","filename_template":"<ID>.nfo","first_name_order":true,"actress_language_ja":false,"rating_source":"r18dev","tagline":"tl","unknown_actress_mode":"skip","unknown_actress_text":"Unknown","tag":["a","b"],"credits":["c"]}`
	var n NFOConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &n))

	assert.True(t, n.Feature.Enabled)
	assert.False(t, n.Feature.PerFile)
	assert.True(t, n.Feature.IncludeFanart)
	assert.True(t, n.Feature.IncludeStreamDetails)
	assert.True(t, n.Feature.ActressAsTag)
	assert.True(t, n.Feature.AltNameRole)
	assert.Equal(t, "<TITLE>", n.Format.DisplayTitle)
	assert.Equal(t, "<ID>.nfo", n.Format.FilenameTemplate)
	assert.True(t, n.Format.FirstNameOrder)
	assert.Equal(t, "r18dev", n.Format.RatingSource)
	assert.Equal(t, "skip", string(n.Format.UnknownActressMode))
	assert.Equal(t, "Unknown", n.Format.UnknownActressText)
	assert.Equal(t, []string{"a", "b"}, n.Extra.Tag)
	assert.Equal(t, []string{"c"}, n.Extra.Credits)
}

// TestNFOConfigUnmarshalJSON_NestedLegacy verifies the legacy nested shape
// ({"Feature": {...}, ...}) is still accepted.
func TestNFOConfigUnmarshalJSON_NestedLegacy(t *testing.T) {
	raw := `{"Feature":{"enabled":false,"per_file":true},"Format":{"first_name_order":false,"unknown_actress_mode":"fallback","unknown_actress_text":"N/A"},"Extra":{"tag":["x"]}}`
	var n NFOConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &n))

	assert.False(t, n.Feature.Enabled)
	assert.True(t, n.Feature.PerFile)
	assert.False(t, n.Format.FirstNameOrder)
	assert.Equal(t, "fallback", string(n.Format.UnknownActressMode))
	assert.Equal(t, "N/A", n.Format.UnknownActressText)
	assert.Equal(t, []string{"x"}, n.Extra.Tag)
}

// TestNFOConfigJSON_RoundTrip verifies marshal→unmarshal preserves all fields.
func TestNFOConfigJSON_RoundTrip(t *testing.T) {
	original := NFOConfig{
		Feature: NFOFeatureConfig{Enabled: true, PerFile: true, IncludeFanart: false, IncludeTrailer: true, IncludeStreamDetails: true, IncludeOriginalPath: true, ActressAsTag: false, AddGenericRole: true, AltNameRole: false},
		Format:  NFOFormatConfig{DisplayTitle: "<TITLE>", FilenameTemplate: "<ID>.nfo", FirstNameOrder: false, ActressLanguageJA: true, RatingSource: "dmm", Tagline: "", UnknownActressMode: "fallback", UnknownActressText: "Unknown Actress"},
		Extra:   NFOExtraConfig{Tag: []string{"tag1", "tag2"}, Credits: []string{"dir", "studio"}},
	}
	b, err := json.Marshal(&original)
	require.NoError(t, err)
	var restored NFOConfig
	require.NoError(t, json.Unmarshal(b, &restored))

	assert.Equal(t, original.Feature, restored.Feature)
	assert.Equal(t, original.Format, restored.Format)
	assert.Equal(t, original.Extra, restored.Extra)
}

// TestNFOConfigUnmarshalJSON_InvalidJSON verifies invalid input surfaces an
// error from UnmarshalJSON. The valid-JSON-wrong-type (array) case reaches
// UnmarshalJSON and fails the probe unmarshal into map[string]json.RawMessage,
// exercising the top-level error-return branch.
func TestNFOConfigUnmarshalJSON_InvalidJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"malformed json", "}bad json{"},
		{"valid json wrong type (array)", "[]"},
		{"valid json wrong type (number)", "42"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var n NFOConfig
			assert.Error(t, json.Unmarshal([]byte(c.raw), &n))
		})
	}
}

// TestNFOConfigUnmarshalJSON_NestedPathError verifies a malformed nested
// payload (Feature present but not an object) errors via the nested branch.
func TestNFOConfigUnmarshalJSON_NestedPathError(t *testing.T) {
	var n NFOConfig
	err := json.Unmarshal([]byte(`{"Feature":"not-an-object"}`), &n)
	assert.Error(t, err)
}

// TestNFOConfigUnmarshalJSON_FlatSubStructErrors verifies type-mismatched
// values in each flat sub-struct field surface an error from every per-group
// unmarshal branch (Feature/Format/Extra).
func TestNFOConfigUnmarshalJSON_FlatSubStructErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"feature field wrong type", `{"enabled":"not-bool"}`},
		{"format field wrong type", `{"first_name_order":"not-bool"}`},
		{"extra field wrong type", `{"tag":"not-an-array"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var n NFOConfig
			err := json.Unmarshal([]byte(c.raw), &n)
			assert.Error(t, err)
		})
	}
}

// TestNFOConfigUnmarshalJSON_NullArrays verifies null array fields decode to
// nil slices without error (backend serializes empty tag/credits as null).
func TestNFOConfigUnmarshalJSON_NullArrays(t *testing.T) {
	raw := `{"enabled":true,"tag":null,"credits":null}`
	var n NFOConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &n))
	assert.True(t, n.Feature.Enabled)
	assert.Nil(t, n.Extra.Tag)
	assert.Nil(t, n.Extra.Credits)
}

// TestOutputConfigMarshalJSON_DoesNotEmitLegacyDelimiterField ensures the
// json:"-" LegacyDelimiter field never appears in output (belt-and-suspenders
// alongside TestOutputConfigMarshalJSON_OmitsLegacyDelimiter).
func TestOutputConfigMarshalJSON_DoesNotEmitLegacyDelimiterField(t *testing.T) {
	o := OutputConfig{Template: OutputTemplateConfig{LegacyDelimiter: "legacy", ActressDelimiter: "canonical"}}
	b, err := json.Marshal(&o)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(b), "legacy"), "LegacyDelimiter must not serialize: %s", b)
	assert.Contains(t, string(b), `"actress_delimiter":"canonical"`)
}
