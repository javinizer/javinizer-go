package scraperconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func overrideFixture() ActressNameOverrides {
	return ActressNameOverrides{"aa6sc": {SourceName: "Yorimoto Shiori", FirstName: "Shiori", LastName: "Yorimoto"}}
}

func TestActressNameOverrideValidation(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		rule     ActressNameOverride
		valid    bool
	}{
		{"valid", "aa6sc", overrideFixture()["aa6sc"], true},
		{"whitespace", "aa6sc", ActressNameOverride{" Yorimoto　 Shiori ", " Shiori ", "Yorimoto"}, true},
		{"given first", "jnka", ActressNameOverride{"Jenna Jameson", "Jenna", "Jameson"}, true},
		{"compound", "compound", ActressNameOverride{"de Example Mary Jane", "Mary Jane", "de Example"}, true},
		{"empty ID", "", overrideFixture()["aa6sc"], false},
		{"URL instead of ID", "vl_star.php?s=aa6sc", overrideFixture()["aa6sc"], false},
		{"ID whitespace", " aa6sc", overrideFixture()["aa6sc"], false},
		{"long ID", strings.Repeat("a", 65), overrideFixture()["aa6sc"], false},
		{"empty", "aa6sc", ActressNameOverride{}, false},
		{"missing source", "aa6sc", ActressNameOverride{"", "Shiori", "Yorimoto"}, false},
		{"missing first", "aa6sc", ActressNameOverride{"Yorimoto Shiori", "", "Yorimoto Shiori"}, false},
		{"missing last", "aa6sc", ActressNameOverride{"Yorimoto Shiori", "Yorimoto Shiori", ""}, false},
		{"rename", "aa6sc", ActressNameOverride{"Yorimoto Shiori", "Other", "Person"}, false},
		{"native", "aa6sc", ActressNameOverride{"依本 しおり", "しおり", "依本"}, false},
		{"stage", "stage", ActressNameOverride{"RION", "RI", "ON"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				settings := ScraperSettings{Enabled: enabled, ActressNameOverrides: ActressNameOverrides{tc.id: tc.rule}}
				err := settings.Validate("javlibrary")
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "actress_name_overrides")
				}
			}
		})
	}
	settings := ScraperSettings{ActressNameOverrides: overrideFixture()}
	require.ErrorContains(t, settings.Validate("dmm"), "only by javlibrary")
	require.NoError(t, ActressNameOverrides(nil).Validate("dmm"))
}

func TestActressNameOverrideRoundTripCloneAndDefaults(t *testing.T) {
	original := ScraperSettings{ActressNameOverrides: overrideFixture()}
	// The override and explicit numeric zeros must survive the same decoder,
	// clone, and serialization path without changing each other.
	require.NoError(t, json.Unmarshal([]byte(`{"timeout":0,"rate_limit":0,"retry_count":0}`), &original))
	clone := original.Clone()
	delete(clone.ActressNameOverrides, "aa6sc")
	require.Len(t, original.ActressNameOverrides, 1)
	var empty ScraperSettings
	empty.MergeDefaultsFrom(original)
	require.Empty(t, empty.ActressNameOverrides, "never inherit actor-specific mappings")
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			var data []byte
			var err error
			if format == "json" {
				data, err = json.Marshal(&original)
			} else {
				data, err = yaml.Marshal(&original)
			}
			require.NoError(t, err)
			var got ScraperSettings
			if format == "json" {
				err = json.Unmarshal(data, &got)
			} else {
				err = yaml.Unmarshal(data, &got)
			}
			require.NoError(t, err)
			require.Equal(t, original.ActressNameOverrides, got.ActressNameOverrides)
			got.MergeDefaultsFrom(ScraperSettings{Timeout: 30, RateLimit: 500, RetryCount: 3})
			require.Zero(t, got.Timeout)
			require.Zero(t, got.RateLimit)
			require.Zero(t, got.RetryCount)
			require.True(t, got.HasExplicitRetryCount())
			got.ActressNameOverrides["aa6sc"] = ActressNameOverride{}
			require.Equal(t, overrideFixture(), original.ActressNameOverrides)
		})
	}
}

func TestActressNameOverrideReplacementDecode(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		for _, tc := range []struct {
			name, json, yaml string
			count            int
			wantErr          bool
		}{
			{"omitted", `{}`, `{}`, 1, false},
			{"empty", `{"actress_name_overrides":{}}`, `actress_name_overrides: {}`, 0, false},
			{"null", `{"actress_name_overrides":null}`, `actress_name_overrides: null`, 0, false},
			{"replace", `{"actress_name_overrides":{"jnka":{"source_name":"Jenna Jameson","first_name":"Jenna","last_name":"Jameson"}}}`, `actress_name_overrides: {jnka: {source_name: Jenna Jameson, first_name: Jenna, last_name: Jameson}}`, 1, false},
			{"malformed", `{"actress_name_overrides":42}`, `actress_name_overrides: 42`, 1, true},
		} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				s := ScraperSettings{ActressNameOverrides: overrideFixture()}
				var err error
				if format == "json" {
					err = json.Unmarshal([]byte(tc.json), &s)
				} else {
					err = yaml.Unmarshal([]byte(tc.yaml), &s)
				}
				if tc.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.Len(t, s.ActressNameOverrides, tc.count)
				if tc.name == "replace" {
					_, old := s.ActressNameOverrides["aa6sc"]
					require.False(t, old)
				}
				if tc.wantErr {
					require.Equal(t, overrideFixture(), s.ActressNameOverrides)
				}
			})
		}
	}
}
