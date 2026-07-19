package scraperconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyConfig_UnmarshalJSON_ProfilesOmittedRetainsPreSeeded(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"main":   {URL: "seed-main"},
		"backup": {URL: "seed-backup"},
	}}
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true}`), &p))
	assert.True(t, p.Enabled)
	assert.Len(t, p.Profiles, 2, "omitted profiles must retain pre-seeded entries")
	_, hasBackup := p.Profiles["backup"]
	assert.True(t, hasBackup)
}

func TestProxyConfig_UnmarshalJSON_ProfilesMappingReplacesPreSeeded(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"main":   {URL: "seed-main"},
		"backup": {URL: "seed-backup"},
	}}
	require.NoError(t, json.Unmarshal([]byte(`{"profiles":{"main":{"url":"http://main"},"custom":{"url":"http://custom"}}}`), &p))
	_, hasMain := p.Profiles["main"]
	_, hasCustom := p.Profiles["custom"]
	_, hasBackup := p.Profiles["backup"]
	assert.True(t, hasMain)
	assert.True(t, hasCustom)
	assert.False(t, hasBackup, "explicit mapping must replace pre-seeded defaults")
}

func TestProxyConfig_UnmarshalJSON_ProfilesNullClearsPreSeeded(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"main":   {URL: "seed-main"},
		"backup": {URL: "seed-backup"},
	}}
	require.NoError(t, json.Unmarshal([]byte(`{"profiles":null}`), &p))
	assert.Empty(t, p.Profiles, "explicit null must clear pre-seeded defaults")
}

func TestProxyConfig_UnmarshalJSON_NullTombstoneEntriesStripped(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"main":   {URL: "seed-main"},
		"backup": {URL: "seed-backup"},
	}}
	require.NoError(t, json.Unmarshal([]byte(`{"profiles":{"main":{"url":"http://main"},"backup":null}}`), &p))
	_, hasMain := p.Profiles["main"]
	_, hasBackup := p.Profiles["backup"]
	assert.True(t, hasMain)
	assert.False(t, hasBackup, "null tombstone entry must be dropped, not materialised as zero-value profile")
	assert.Equal(t, "http://main", p.Profiles["main"].URL)
}

func TestProxyConfig_UnmarshalJSON_OmittedScalarsRetainPreSeeded(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{
		Enabled:        true,
		Profile:        "seed",
		DefaultProfile: "seed-default",
		Profiles:       map[string]ProxyProfile{"main": {URL: "seed-main"}},
	}
	require.NoError(t, json.Unmarshal([]byte(`{"profiles":{"main":{"url":"http://main"}}}`), &p))
	assert.True(t, p.Enabled, "omitted enabled must retain pre-seeded value")
	assert.Equal(t, "seed", p.Profile, "omitted profile must retain pre-seeded value")
	assert.Equal(t, "seed-default", p.DefaultProfile, "omitted default_profile must retain pre-seeded value")
}

func TestProxyConfig_UnmarshalJSON_MalformedRejected(t *testing.T) {
	t.Parallel()

	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()
		var p ProxyConfig
		assert.Error(t, (&p).UnmarshalJSON([]byte(`{invalid`)), "malformed JSON must surface a decode error")
	})
	t.Run("profiles as array", func(t *testing.T) {
		t.Parallel()
		var p ProxyConfig
		assert.Error(t, json.Unmarshal([]byte(`{"profiles":[1,2]}`), &p), "profiles must be an object or null")
	})
	t.Run("profiles as scalar", func(t *testing.T) {
		t.Parallel()
		var p ProxyConfig
		assert.Error(t, json.Unmarshal([]byte(`{"profiles":"x"}`), &p), "profiles must be an object or null")
	})
}

func TestProxyConfig_UnmarshalJSON_LegacyFieldRejected(t *testing.T) {
	t.Parallel()

	cases := []string{"url", "username", "password", "use_main_proxy"}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			var p ProxyConfig
			err := json.Unmarshal([]byte(`{"`+field+`":"x"}`), &p)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no longer supported")
		})
	}
}

func TestProxyProfile_UnmarshalJSON_Valid(t *testing.T) {
	t.Parallel()

	var p ProxyProfile
	require.NoError(t, json.Unmarshal([]byte(`{"url":"http://x","username":"u","password":"p"}`), &p))
	assert.Equal(t, "http://x", p.URL)
	assert.Equal(t, "u", p.Username)
	assert.Equal(t, "p", p.Password)
}

func TestStripNullProfileEntriesJSON(t *testing.T) {
	t.Parallel()

	t.Run("empty raw is no-op", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {}}
		require.NoError(t, stripNullProfileEntriesJSON(nil, &profiles))
		assert.Len(t, profiles, 1)
	})
	t.Run("null raw clears map", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {URL: "http://main"}}
		require.NoError(t, stripNullProfileEntriesJSON(json.RawMessage(`null`), &profiles))
		assert.Nil(t, profiles, "null occurrence must clear the map")
	})
	t.Run("null entry deleted, others merged", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {URL: "http://main"}, "backup": {}}
		require.NoError(t, stripNullProfileEntriesJSON(json.RawMessage(`{"main":{"url":"http://main"},"backup":null}`), &profiles))
		_, hasMain := profiles["main"]
		_, hasBackup := profiles["backup"]
		assert.True(t, hasMain)
		assert.False(t, hasBackup, "null tombstone entry must be deleted")
		assert.Equal(t, "http://main", profiles["main"].URL)
	})
	t.Run("mapping into nil map allocates", func(t *testing.T) {
		t.Parallel()
		var profiles map[string]ProxyProfile
		require.NoError(t, stripNullProfileEntriesJSON(json.RawMessage(`{"main":{"url":"http://main"}}`), &profiles))
		assert.Equal(t, "http://main", profiles["main"].URL)
	})
	t.Run("empty object entry kept as zero value", func(t *testing.T) {
		t.Parallel()
		var profiles map[string]ProxyProfile
		require.NoError(t, stripNullProfileEntriesJSON(json.RawMessage(`{"a":{}}`), &profiles))
		_, hasA := profiles["a"]
		assert.True(t, hasA, "empty object must not be treated as a tombstone")
	})
	t.Run("bareword raw returns error", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {URL: "http://main"}}
		err := stripNullProfileEntriesJSON(json.RawMessage(`malformed`), &profiles)
		require.Error(t, err)
		_, hasMain := profiles["main"]
		assert.True(t, hasMain, "malformed raw must leave profiles untouched")
	})
	t.Run("malformed mapping returns error", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {URL: "http://main"}}
		err := stripNullProfileEntriesJSON(json.RawMessage(`{malformed`), &profiles)
		require.Error(t, err)
		assert.Len(t, profiles, 1, "malformed raw must leave profiles untouched")
	})
	t.Run("array raw returns error", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {}}
		err := stripNullProfileEntriesJSON(json.RawMessage(`[1,2]`), &profiles)
		require.Error(t, err)
		assert.Len(t, profiles, 1, "array raw must leave profiles untouched")
	})
	t.Run("scalar raw returns error", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {}}
		err := stripNullProfileEntriesJSON(json.RawMessage(`"x"`), &profiles)
		require.Error(t, err)
		assert.Len(t, profiles, 1, "scalar raw must leave profiles untouched")
	})
	t.Run("malformed entry value returns error", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {}}
		err := stripNullProfileEntriesJSON(json.RawMessage(`{"a":{malformed}}`), &profiles)
		require.Error(t, err)
		_, hasMain := profiles["main"]
		assert.True(t, hasMain, "malformed entry must leave profiles untouched")
	})
	t.Run("entry value wrong type returns error", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {}}
		err := stripNullProfileEntriesJSON(json.RawMessage(`{"a":[1,2]}`), &profiles)
		require.Error(t, err)
		_, hasMain := profiles["main"]
		assert.True(t, hasMain, "wrong-type entry must leave profiles untouched")
	})
}

func TestProxyConfig_UnmarshalJSON_ReusedReceiverUnchangedOnError(t *testing.T) {
	t.Parallel()

	seed := func() ProxyConfig {
		return ProxyConfig{
			Enabled:        true,
			Profile:        "seed",
			DefaultProfile: "seed-default",
			Profiles: map[string]ProxyProfile{
				"main":   {URL: "seed-main"},
				"backup": {URL: "seed-backup"},
			},
		}
	}

	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()
		p := seed()
		err := (&p).UnmarshalJSON([]byte(`{invalid`))
		require.Error(t, err)
		assert.True(t, p.Enabled)
		assert.Equal(t, "seed", p.Profile)
		assert.Equal(t, "seed-main", p.Profiles["main"].URL)
	})

	t.Run("profiles type error", func(t *testing.T) {
		t.Parallel()
		p := seed()
		err := json.Unmarshal([]byte(`{"profiles":[1,2]}`), &p)
		require.Error(t, err)
		assert.True(t, p.Enabled, "enabled must be unchanged on profiles decode error")
		assert.Equal(t, "seed", p.Profile)
		assert.Equal(t, "seed-main", p.Profiles["main"].URL, "pre-seeded profiles must survive profiles decode error")
		_, hasBackup := p.Profiles["backup"]
		assert.True(t, hasBackup)
	})

	t.Run("enabled type error", func(t *testing.T) {
		t.Parallel()
		p := seed()
		err := json.Unmarshal([]byte(`{"enabled":[1,2,3]}`), &p)
		require.Error(t, err)
		assert.True(t, p.Enabled, "enabled must be unchanged on type error")
		assert.Equal(t, "seed", p.Profile)
		assert.Equal(t, "seed-main", p.Profiles["main"].URL)
	})

	t.Run("late field error after profiles decoded", func(t *testing.T) {
		t.Parallel()
		p := seed()
		err := json.Unmarshal([]byte(`{"profiles":{"main":{"url":"http://main"}},"enabled":[1,2,3]}`), &p)
		require.Error(t, err)
		assert.True(t, p.Enabled, "enabled must be unchanged when late field errors")
		assert.Equal(t, "seed", p.Profile)
		_, hasBackup := p.Profiles["backup"]
		assert.True(t, hasBackup, "pre-seeded backup must survive late-field decode error")
		assert.Equal(t, "seed-main", p.Profiles["main"].URL, "decoded tmp profiles must not leak into receiver on error")
	})
}

func TestProxyConfig_UnmarshalJSON_UnknownFieldIgnored(t *testing.T) {
	t.Parallel()

	var p ProxyConfig
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true,"future_field":"value"}`), &p))
	assert.True(t, p.Enabled)
}

func TestProxyProfile_UnmarshalJSON_UnknownFieldIgnored(t *testing.T) {
	t.Parallel()

	var p ProxyProfile
	require.NoError(t, json.Unmarshal([]byte(`{"url":"http://x","future_field":"value"}`), &p))
	assert.Equal(t, "http://x", p.URL)
}

func TestProxyConfig_UnmarshalJSON_MixedCaseLegacyVariantRejected(t *testing.T) {
	t.Parallel()

	cases := []string{"URL", "Url", "uRl", "Username", "Password", "Use_Main_Proxy", "USE_MAIN_PROXY"}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			p := ProxyConfig{Enabled: true, Profile: "seed", Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}}}
			err := json.Unmarshal([]byte(`{"`+field+`":"x"}`), &p)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no longer supported")
			assert.True(t, p.Enabled, "receiver must be unchanged on rejection error")
			assert.Equal(t, "seed", p.Profile)
			_, hasKeep := p.Profiles["keep"]
			assert.True(t, hasKeep, "pre-seeded profile must survive rejection error")
		})
	}
}

func TestProxyProfile_UnmarshalJSON_MixedCaseKnownFieldNotRejected(t *testing.T) {
	t.Parallel()

	var p ProxyProfile
	require.NoError(t, json.Unmarshal([]byte(`{"URL":"http://x"}`), &p))
	assert.Equal(t, "http://x", p.URL, "encoding/json is case-insensitive: mixed-case known field must decode")
}

func TestResolveProfilesJSON(t *testing.T) {
	t.Parallel()

	t.Run("omitted profiles not present", func(t *testing.T) {
		t.Parallel()
		profiles, present, err := resolveProfilesJSON([]byte(`{"enabled":true}`))
		require.NoError(t, err)
		assert.False(t, present)
		assert.Nil(t, profiles)
	})
	t.Run("single mapping", func(t *testing.T) {
		t.Parallel()
		profiles, present, err := resolveProfilesJSON([]byte(`{"profiles":{"main":{"url":"http://main"}}}`))
		require.NoError(t, err)
		assert.True(t, present)
		assert.Equal(t, "http://main", profiles["main"].URL)
	})
	t.Run("null clears", func(t *testing.T) {
		t.Parallel()
		profiles, present, err := resolveProfilesJSON([]byte(`{"profiles":null}`))
		require.NoError(t, err)
		assert.True(t, present)
		assert.Nil(t, profiles)
	})
	t.Run("non-object array returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := resolveProfilesJSON([]byte(`[1,2]`))
		require.Error(t, err)
	})
	t.Run("non-object scalar returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := resolveProfilesJSON([]byte(`"x"`))
		require.Error(t, err)
	})
	t.Run("bareword returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := resolveProfilesJSON([]byte(`malformed`))
		require.Error(t, err)
	})
	t.Run("malformed object returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := resolveProfilesJSON([]byte(`{invalid`))
		require.Error(t, err)
	})
	t.Run("profiles array returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := resolveProfilesJSON([]byte(`{"profiles":[1,2]}`))
		require.Error(t, err)
	})
	t.Run("profiles malformed value returns error", func(t *testing.T) {
		t.Parallel()
		_, _, err := resolveProfilesJSON([]byte(`{"profiles":{malformed}}`))
		require.Error(t, err)
	})
}

func TestProxyConfig_UnmarshalJSON_ProfilesOrderedSemantics(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		json     string
		seed     map[string]ProxyProfile
		wantKeys []string
		notKeys  []string
		wantURL  map[string]string
	}{
		{
			name:     "exact duplicate mapping merge",
			json:     `{"profiles":{"a":{"url":"http://a"}},"profiles":{"b":{"url":"http://b"}}}`,
			wantKeys: []string{"a", "b"},
			wantURL:  map[string]string{"a": "http://a", "b": "http://b"},
		},
		{
			name:     "case variants merge in source order",
			json:     `{"Profiles":{"a":{"url":"http://a"}},"profiles":{"b":{"url":"http://b"}}}`,
			wantKeys: []string{"a", "b"},
		},
		{
			name:     "three case-insensitive occurrences merge",
			json:     `{"profiles":{"a":{"url":"http://a"}},"Profiles":{"b":{"url":"http://b"}},"PROFILES":{"c":{"url":"http://c"}}}`,
			wantKeys: []string{"a", "b", "c"},
		},
		{
			name:     "duplicate key last mapping wins",
			json:     `{"profiles":{"a":{"url":"http://first"}},"profiles":{"a":{"url":"http://second"}}}`,
			wantKeys: []string{"a"},
			wantURL:  map[string]string{"a": "http://second"},
		},
		{
			name:     "null then mapping",
			json:     `{"profiles":null,"profiles":{"a":{"url":"http://a"}}}`,
			wantKeys: []string{"a"},
		},
		{
			name:    "mapping then null",
			json:    `{"profiles":{"a":{"url":"http://a"}},"profiles":null}`,
			notKeys: []string{"a"},
		},
		{
			name:     "tombstone then re-add",
			json:     `{"profiles":{"a":null},"profiles":{"a":{"url":"http://a"}}}`,
			wantKeys: []string{"a"},
			wantURL:  map[string]string{"a": "http://a"},
		},
		{
			name:    "re-add then tombstone",
			json:    `{"profiles":{"a":{"url":"http://a"}},"profiles":{"a":null}}`,
			notKeys: []string{"a"},
		},
		{
			name:     "tombstone in first occurrence, sibling survives",
			json:     `{"profiles":{"a":null,"b":{"url":"http://b"}}}`,
			wantKeys: []string{"b"},
			notKeys:  []string{"a"},
		},
		{
			name:     "tombstone in second occurrence, sibling from first survives",
			json:     `{"profiles":{"a":{"url":"http://a"}},"profiles":{"b":null}}`,
			wantKeys: []string{"a"},
			notKeys:  []string{"b"},
		},
		{
			name:     "within-mapping duplicate null then value",
			json:     `{"profiles":{"a":null,"a":{"url":"http://a"}}}`,
			wantKeys: []string{"a"},
			wantURL:  map[string]string{"a": "http://a"},
		},
		{
			name:    "within-mapping duplicate value then null",
			json:    `{"profiles":{"a":{"url":"http://a"},"a":null}}`,
			notKeys: []string{"a"},
		},
		{
			name:     "first occurrence replaces pre-seeded",
			json:     `{"profiles":{"a":{"url":"http://a"}}}`,
			seed:     map[string]ProxyProfile{"seed": {URL: "http://seed"}},
			wantKeys: []string{"a"},
			notKeys:  []string{"seed"},
		},
		{
			name:     "omitted retains pre-seeded",
			json:     `{"enabled":true}`,
			seed:     map[string]ProxyProfile{"seed": {URL: "http://seed"}},
			wantKeys: []string{"seed"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := ProxyConfig{Profiles: tc.seed}
			require.NoError(t, json.Unmarshal([]byte(tc.json), &p))
			for _, k := range tc.wantKeys {
				_, ok := p.Profiles[k]
				assert.Truef(t, ok, "expected profile %q", k)
			}
			for _, k := range tc.notKeys {
				_, ok := p.Profiles[k]
				assert.Falsef(t, ok, "profile %q must be absent", k)
			}
			for k, v := range tc.wantURL {
				assert.Equalf(t, v, p.Profiles[k].URL, "profile %q url", k)
			}
		})
	}
}

func TestProxyConfig_UnmarshalJSON_MalformedProfilesReceiverUnchanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		json string
	}{
		{"profiles array", `{"profiles":[1,2]}`},
		{"profiles scalar", `{"profiles":"x"}`},
		{"profiles malformed entry", `{"profiles":{"a":{malformed}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := ProxyConfig{
				Enabled:  true,
				Profile:  "seed",
				Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}},
			}
			err := json.Unmarshal([]byte(tc.json), &p)
			require.Error(t, err)
			assert.True(t, p.Enabled, "receiver must be unchanged on error")
			assert.Equal(t, "seed", p.Profile)
			_, hasKeep := p.Profiles["keep"]
			assert.True(t, hasKeep, "pre-seeded profiles must survive error")
		})
	}
}
