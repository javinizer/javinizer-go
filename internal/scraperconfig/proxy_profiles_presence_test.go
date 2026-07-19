package scraperconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestProxyConfig_UnmarshalYAML_ProfilesOmittedRetainsPreSeeded(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"main":   {URL: "seed-main"},
		"backup": {URL: "seed-backup"},
	}}
	require.NoError(t, yaml.Unmarshal([]byte("enabled: true\n"), &p))
	assert.True(t, p.Enabled)
	assert.Len(t, p.Profiles, 2, "omitted profiles must retain pre-seeded entries")
	_, hasBackup := p.Profiles["backup"]
	assert.True(t, hasBackup)
}

func TestProxyConfig_UnmarshalYAML_ProfilesMappingReplacesPreSeeded(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"main":   {URL: "seed-main"},
		"backup": {URL: "seed-backup"},
	}}
	require.NoError(t, yaml.Unmarshal([]byte("profiles:\n  main:\n    url: http://main\n  custom:\n    url: http://custom\n"), &p))
	assert.Len(t, p.Profiles, 2)
	_, hasMain := p.Profiles["main"]
	_, hasCustom := p.Profiles["custom"]
	_, hasBackup := p.Profiles["backup"]
	assert.True(t, hasMain)
	assert.True(t, hasCustom)
	assert.False(t, hasBackup, "explicit mapping must replace pre-seeded defaults")
}

func TestProxyConfig_UnmarshalYAML_ProfilesNullClearsPreSeeded(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"main":   {URL: "seed-main"},
		"backup": {URL: "seed-backup"},
	}}
	require.NoError(t, yaml.Unmarshal([]byte("profiles: null\n"), &p))
	assert.Empty(t, p.Profiles, "explicit null must clear pre-seeded defaults")
}

func TestProxyConfig_UnmarshalYAML_NullTombstoneEntriesStripped(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"main":   {URL: "seed-main"},
		"backup": {URL: "seed-backup"},
	}}
	require.NoError(t, yaml.Unmarshal([]byte("profiles:\n  main:\n    url: http://main\n  backup: null\n"), &p))
	_, hasMain := p.Profiles["main"]
	_, hasBackup := p.Profiles["backup"]
	assert.True(t, hasMain)
	assert.False(t, hasBackup, "null tombstone entry must be dropped, not materialised as zero-value profile")
}

func TestProxyConfig_UnmarshalYAML_AliasedEntriesPreserved(t *testing.T) {
	t.Parallel()

	input := []byte("profiles:\n  seed: &seed\n    url: http://seed\n  extra: *seed\n")
	var p ProxyConfig
	require.NoError(t, yaml.Unmarshal(input, &p))
	_, hasSeed := p.Profiles["seed"]
	_, hasExtra := p.Profiles["extra"]
	assert.True(t, hasSeed, "anchored profile must survive")
	assert.True(t, hasExtra, "aliased profile entry must resolve and survive")
	assert.Equal(t, "http://seed", p.Profiles["extra"].URL)
}

func TestProxyConfig_UnmarshalYAML_DecodeErrorPropagated(t *testing.T) {
	t.Parallel()

	var p ProxyConfig
	err := yaml.Unmarshal([]byte("enabled: [1, 2, 3]\n"), &p)
	assert.Error(t, err, "a type mismatch surviving field validation must surface the decode error")
}

func TestStripNullProfileEntries(t *testing.T) {
	t.Parallel()

	t.Run("nil resolved is no-op", func(t *testing.T) {
		t.Parallel()
		profiles := map[string]ProxyProfile{"main": {}}
		stripNullProfileEntries(nil, profiles)
		assert.Len(t, profiles, 1)
	})
	t.Run("nil profiles is no-op", func(t *testing.T) {
		t.Parallel()
		stripNullProfileEntries(map[string]*yaml.Node{"backup": {Kind: yaml.ScalarNode, Tag: "!!null"}}, nil)
	})
	t.Run("null scalar entry deleted", func(t *testing.T) {
		t.Parallel()
		resolved := map[string]*yaml.Node{
			"main":   {Kind: yaml.MappingNode},
			"backup": {Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"},
		}
		profiles := map[string]ProxyProfile{"main": {URL: "http://main"}, "backup": {}}
		stripNullProfileEntries(resolved, profiles)
		_, hasMain := profiles["main"]
		_, hasBackup := profiles["backup"]
		assert.True(t, hasMain)
		assert.False(t, hasBackup)
	})
	t.Run("mapping entry preserved", func(t *testing.T) {
		t.Parallel()
		resolved := map[string]*yaml.Node{
			"extra": {Kind: yaml.MappingNode},
		}
		profiles := map[string]ProxyProfile{"extra": {}}
		stripNullProfileEntries(resolved, profiles)
		_, hasExtra := profiles["extra"]
		assert.True(t, hasExtra, "non-null entry must not be stripped")
	})
}

func TestProxyConfig_UnmarshalYAML_NilNodeDirect(t *testing.T) {
	t.Parallel()

	p := ProxyConfig{Enabled: true, Profile: "seed", Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}}}
	require.NoError(t, (&p).UnmarshalYAML(nil))
	assert.True(t, p.Enabled, "nil node must leave receiver untouched")
	assert.Equal(t, "seed", p.Profile)
	_, hasKeep := p.Profiles["keep"]
	assert.True(t, hasKeep)
}

func TestProxyConfig_UnmarshalYAML_ReusedReceiverUnchangedOnError(t *testing.T) {
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

	t.Run("profiles type error", func(t *testing.T) {
		t.Parallel()
		p := seed()
		err := yaml.Unmarshal([]byte("profiles:\n  main: [1, 2]\n"), &p)
		require.Error(t, err)
		assert.True(t, p.Enabled, "enabled must be unchanged on decode error")
		assert.Equal(t, "seed", p.Profile)
		assert.Equal(t, "seed-default", p.DefaultProfile)
		assert.Equal(t, "seed-main", p.Profiles["main"].URL, "pre-seeded profiles must survive profiles decode error")
		_, hasBackup := p.Profiles["backup"]
		assert.True(t, hasBackup)
	})

	t.Run("enabled type error", func(t *testing.T) {
		t.Parallel()
		p := seed()
		err := yaml.Unmarshal([]byte("enabled: [1, 2, 3]\n"), &p)
		require.Error(t, err)
		assert.True(t, p.Enabled, "enabled must be unchanged on type error")
		assert.Equal(t, "seed", p.Profile)
		assert.Equal(t, "seed-main", p.Profiles["main"].URL)
	})

	t.Run("late field error after profiles decoded", func(t *testing.T) {
		t.Parallel()
		p := seed()
		err := yaml.Unmarshal([]byte("profiles:\n  main:\n    url: http://main\nenabled: [1, 2, 3]\n"), &p)
		require.Error(t, err)
		assert.True(t, p.Enabled, "enabled must be unchanged when late field errors")
		assert.Equal(t, "seed", p.Profile)
		_, hasBackup := p.Profiles["backup"]
		assert.True(t, hasBackup, "pre-seeded backup must survive late-field decode error")
		assert.Equal(t, "seed-main", p.Profiles["main"].URL, "decoded tmp profiles must not leak into receiver on error")
	})
}

func TestProxyConfig_UnmarshalYAML_MergeKeySupported(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		"<<: &base",
		"  enabled: true",
		"profiles:",
		"  main:",
		"    url: http://main",
	}, "\n"))
	var p ProxyConfig
	require.NoError(t, yaml.Unmarshal(input, &p))
	assert.True(t, p.Enabled, "merge key << must resolve without being rejected")
	_, hasMain := p.Profiles["main"]
	assert.True(t, hasMain)
	assert.Equal(t, "http://main", p.Profiles["main"].URL)
}

func TestProxyConfig_UnmarshalYAML_QuotedMergeLiteralIgnored(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		quote string
	}{
		{"double-quoted", `"<<"`},
		{"single-quoted", `'<<'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := []byte(strings.Join([]string{
				tc.quote + ": &base",
				"  enabled: true",
				"  profiles:",
				"    main:",
				"      url: http://merged",
				"  url: http://legacy",
			}, "\n"))
			p := ProxyConfig{
				Enabled:  false,
				Profile:  "seed",
				Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}},
			}
			require.NoError(t, yaml.Unmarshal(input, &p),
				"quoted << literal must be an ordinary ignored key, not a merge or legacy rejection")
			assert.False(t, p.Enabled, "quoted << must not merge nested enabled into receiver")
			assert.Equal(t, "seed", p.Profile)
			_, hasKeep := p.Profiles["keep"]
			assert.True(t, hasKeep, "quoted << must not clear or replace pre-seeded profiles")
			_, hasMain := p.Profiles["main"]
			assert.False(t, hasMain, "quoted << must not merge nested profiles into receiver")
		})
	}
}

func TestEffectiveMappingKeys_QuotedMergeLiteralIsOrdinaryKey(t *testing.T) {
	t.Parallel()

	target := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "enabled", Tag: "!!str"},
		{Kind: yaml.ScalarNode, Value: "true", Tag: "!!bool"},
	}}
	node := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "<<", Tag: "!!str"},
		{Kind: yaml.AliasNode, Alias: target},
	}}
	keys, err := effectiveMappingKeys(node)
	require.NoError(t, err)
	assert.NotContains(t, keys, "enabled", "!!str << key must not resolve as a merge")
	assert.Contains(t, keys, "<<", "!!str << key must be indexed as an ordinary key")
}

func TestProxyConfig_UnmarshalYAML_UnknownFieldIgnored(t *testing.T) {
	t.Parallel()

	var p ProxyConfig
	require.NoError(t, yaml.Unmarshal([]byte("enabled: true\nfuture_field: value\n"), &p))
	assert.True(t, p.Enabled)
}

func TestProxyProfile_UnmarshalYAML_UnknownFieldIgnored(t *testing.T) {
	t.Parallel()

	var p ProxyProfile
	require.NoError(t, yaml.Unmarshal([]byte("url: http://x\nfuture_field: value\n"), &p))
	assert.Equal(t, "http://x", p.URL)
}

func TestProxyConfig_UnmarshalYAML_MixedCaseLegacyVariantRejected(t *testing.T) {
	t.Parallel()

	cases := []string{"URL", "Url", "uRl", "Username", "Password", "Use_Main_Proxy", "USE_MAIN_PROXY"}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			p := ProxyConfig{Enabled: true, Profile: "seed", Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}}}
			err := yaml.Unmarshal([]byte(field+": http://x\n"), &p)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no longer supported")
			assert.True(t, p.Enabled, "receiver must be unchanged on rejection error")
			assert.Equal(t, "seed", p.Profile)
			_, hasKeep := p.Profiles["keep"]
			assert.True(t, hasKeep, "pre-seeded profile must survive rejection error")
		})
	}
}

func TestProxyConfig_UnmarshalYAML_MixedCaseKnownFieldNotDecoded(t *testing.T) {
	t.Parallel()

	var p ProxyConfig
	require.NoError(t, yaml.Unmarshal([]byte("Profiles:\n  main:\n    url: http://x\n"), &p))
	assert.Empty(t, p.Profiles, "mixed-case key must not be decoded or null-stripped")
}

func mustParseProxyYAMLNode(t *testing.T, data string) *yaml.Node {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(data), &root))
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		return root.Content[0]
	}
	return &root
}

func proxyMappingHasKey(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

func TestProxyConfig_UnmarshalYAML_MergeKeyProfilesReplacesPreSeeded(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		"<<: &base",
		"  profiles:",
		"    main:",
		"      url: http://main",
		"    backup: null",
	}, "\n"))
	p := ProxyConfig{Profiles: map[string]ProxyProfile{
		"keep":  {URL: "seed-keep"},
		"other": {URL: "seed-other"},
	}}
	require.NoError(t, yaml.Unmarshal(input, &p))
	_, hasMain := p.Profiles["main"]
	_, hasBackup := p.Profiles["backup"]
	_, hasKeep := p.Profiles["keep"]
	_, hasOther := p.Profiles["other"]
	assert.True(t, hasMain, "merge-supplied profile must survive")
	assert.Equal(t, "http://main", p.Profiles["main"].URL)
	assert.False(t, hasBackup, "merge-supplied null tombstone must be stripped")
	assert.False(t, hasKeep, "merge-supplied profiles must replace pre-seeded map")
	assert.False(t, hasOther, "merge-supplied profiles must replace pre-seeded map")
}

func TestProxyConfig_UnmarshalYAML_MergeKeySequenceProfilesReplacesPreSeeded(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		"a: &a",
		"  profiles:",
		"    main: {url: http://main}",
		"b: &b",
		"  profiles:",
		"    extra: {url: http://extra}",
		"<<: [*a, *b]",
	}, "\n"))
	p := ProxyConfig{Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}}}
	require.NoError(t, yaml.Unmarshal(input, &p))
	_, hasMain := p.Profiles["main"]
	_, hasExtra := p.Profiles["extra"]
	_, hasKeep := p.Profiles["keep"]
	assert.True(t, hasMain, "earlier sequence merge entry wins for shared profiles key")
	assert.False(t, hasExtra, "later sequence merge entry must not override earlier profiles")
	assert.False(t, hasKeep, "sequence-merged profiles must replace pre-seeded map")
	assert.Equal(t, "http://main", p.Profiles["main"].URL)
}

func TestProxyConfig_UnmarshalYAML_MergeKeyLateErrorUnchangedReceiver(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		"<<: &base",
		"  profiles:",
		"    main:",
		"      url: http://main",
		"    backup: null",
		"enabled: [1, 2, 3]",
	}, "\n"))
	p := ProxyConfig{
		Enabled: true,
		Profile: "seed",
		Profiles: map[string]ProxyProfile{
			"keep": {URL: "seed-keep"},
		},
	}
	err := yaml.Unmarshal(input, &p)
	require.Error(t, err)
	assert.True(t, p.Enabled, "enabled must be unchanged on late decode error")
	assert.Equal(t, "seed", p.Profile)
	_, hasKeep := p.Profiles["keep"]
	assert.True(t, hasKeep, "pre-seeded profile must survive merge + late-field decode error")
	assert.Equal(t, "seed-keep", p.Profiles["keep"].URL)
	_, hasMain := p.Profiles["main"]
	assert.False(t, hasMain, "merge-decoded profile must not leak into receiver on error")
	_, hasBackup := p.Profiles["backup"]
	assert.False(t, hasBackup, "merge-decoded null tombstone must not leak into receiver on error")
}

func TestProxyConfig_UnmarshalYAML_MergeKeyLegacyFieldRejected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "legacy url via merge",
			yaml: strings.Join([]string{
				"<<: &base",
				"  url: http://legacy",
				"  enabled: true",
			}, "\n"),
		},
		{
			name: "legacy password via merge",
			yaml: strings.Join([]string{
				"<<: &base",
				"  password: secret",
			}, "\n"),
		},
		{
			name: "legacy field via sequence merge",
			yaml: strings.Join([]string{
				"a: &a",
				"  username: u",
				"<<: [*a]",
			}, "\n"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var p ProxyConfig
			err := yaml.Unmarshal([]byte(tc.yaml), &p)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no longer supported")
		})
	}
}

func TestEffectiveMappingKeys(t *testing.T) {
	t.Parallel()

	mustKeys := func(t *testing.T, node *yaml.Node) map[string]*yaml.Node {
		t.Helper()
		keys, err := effectiveMappingKeys(node)
		require.NoError(t, err)
		return keys
	}

	t.Run("nil node returns nil", func(t *testing.T) {
		t.Parallel()
		keys, err := effectiveMappingKeys(nil)
		require.NoError(t, err)
		assert.Nil(t, keys)
	})
	t.Run("non-mapping returns nil", func(t *testing.T) {
		t.Parallel()
		keys, err := effectiveMappingKeys(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str"})
		require.NoError(t, err)
		assert.Nil(t, keys)
	})
	t.Run("direct keys only", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, "enabled: true\nprofile: p\n")
		keys := mustKeys(t, node)
		assert.Contains(t, keys, "enabled")
		assert.Contains(t, keys, "profile")
		assert.NotContains(t, keys, "profiles")
	})
	t.Run("single mapping merge", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, "<<: &base\n  enabled: true\n  profiles: {main: {url: http://main}}\n")
		keys := mustKeys(t, node)
		assert.Contains(t, keys, "enabled")
		assert.Contains(t, keys, "profiles")
	})
	t.Run("direct overrides merge", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, strings.Join([]string{
			"base: &base",
			"  profiles: {merged: {url: http://merged}}",
			"<<: *base",
			"profiles: {direct: {url: http://direct}}",
		}, "\n"))
		keys := mustKeys(t, node)
		require.Contains(t, keys, "profiles")
		assert.True(t, proxyMappingHasKey(keys["profiles"], "direct"))
		assert.False(t, proxyMappingHasKey(keys["profiles"], "merged"))
	})
	t.Run("sequence merge earlier wins", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, strings.Join([]string{
			"a: &a",
			"  shared: from-a",
			"b: &b",
			"  shared: from-b",
			"<<: [*a, *b]",
		}, "\n"))
		keys := mustKeys(t, node)
		require.Contains(t, keys, "shared")
		assert.Equal(t, "from-a", keys["shared"].Value)
	})
	t.Run("alias to mapping in sequence", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, strings.Join([]string{
			"a: &a",
			"  profiles: {main: {url: http://main}}",
			"<<: *a",
		}, "\n"))
		keys := mustKeys(t, node)
		require.Contains(t, keys, "profiles")
		assert.Equal(t, yaml.MappingNode, keys["profiles"].Kind)
	})
	t.Run("merge alias resolves to target", func(t *testing.T) {
		t.Parallel()
		target := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "enabled", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "true", Tag: "!!bool"},
		}}
		node := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "<<", Tag: "!!merge"},
			{Kind: yaml.AliasNode, Alias: target},
		}}
		keys := mustKeys(t, node)
		assert.Contains(t, keys, "enabled")
	})
	t.Run("nested merge within merged mapping", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, strings.Join([]string{
			"inner: &inner",
			"  profiles: {main: {url: http://main}}",
			"outer: &outer",
			"  <<: *inner",
			"  enabled: true",
			"<<: *outer",
		}, "\n"))
		keys := mustKeys(t, node)
		assert.Contains(t, keys, "enabled")
		assert.Contains(t, keys, "profiles")
	})
	t.Run("merge to non-mapping is ignored", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, "<<: scalar-value\nenabled: true\n")
		keys := mustKeys(t, node)
		assert.Contains(t, keys, "enabled")
		assert.NotContains(t, keys, "profiles")
	})
	t.Run("merge alias with nil target is ignored", func(t *testing.T) {
		t.Parallel()
		node := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "<<", Tag: "!!merge"},
			{Kind: yaml.AliasNode, Alias: nil},
			{Kind: yaml.ScalarNode, Value: "enabled", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "true", Tag: "!!bool"},
		}}
		keys := mustKeys(t, node)
		assert.Contains(t, keys, "enabled")
		assert.NotContains(t, keys, "profiles")
	})
	t.Run("nested direct key overrides inner merge", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, strings.Join([]string{
			"inner: &inner",
			"  x: from-inner",
			"outer: &outer",
			"  <<: *inner",
			"  x: from-outer-direct",
			"<<: *outer",
		}, "\n"))
		keys := mustKeys(t, node)
		require.Contains(t, keys, "x")
		assert.Equal(t, "from-outer-direct", keys["x"].Value, "direct key of a merged mapping wins over its nested merge")
	})
	t.Run("self-cycle returns error", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, strings.Join([]string{
			"<<: &a",
			"  enabled: true",
			"  <<: *a",
		}, "\n"))
		keys, err := effectiveMappingKeys(node)
		require.Error(t, err)
		assert.Nil(t, keys)
		assert.Contains(t, err.Error(), "cycle")
	})
	t.Run("sequence self-cycle returns error", func(t *testing.T) {
		t.Parallel()
		node := mustParseProxyYAMLNode(t, strings.Join([]string{
			"seq: &seq",
			"  - *seq",
			"<<: *seq",
		}, "\n"))
		_, err := effectiveMappingKeys(node)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cycle")
	})
}

func TestProxyConfig_UnmarshalYAML_MergeResolverProfilesTombstones(t *testing.T) {
	t.Parallel()

	t.Run("inherited null profile via merge is tombstone", func(t *testing.T) {
		t.Parallel()
		input := []byte(strings.Join([]string{
			"profiles:",
			"  <<: &base",
			"    main: null",
			"    backup: null",
			"  custom:",
			"    url: http://custom",
		}, "\n"))
		p := ProxyConfig{Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}}}
		require.NoError(t, yaml.Unmarshal(input, &p))
		_, hasMain := p.Profiles["main"]
		_, hasBackup := p.Profiles["backup"]
		_, hasCustom := p.Profiles["custom"]
		_, hasKeep := p.Profiles["keep"]
		assert.False(t, hasMain, "inherited null profile via << must be a tombstone")
		assert.False(t, hasBackup, "inherited null profile via << must be a tombstone")
		assert.True(t, hasCustom, "non-null profile must survive")
		assert.False(t, hasKeep, "explicit profiles mapping must replace pre-seeded map")
	})

	t.Run("direct profile overrides inherited null", func(t *testing.T) {
		t.Parallel()
		input := []byte(strings.Join([]string{
			"profiles:",
			"  <<: &base",
			"    main: null",
			"  main:",
			"    url: http://main",
		}, "\n"))
		p := ProxyConfig{Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}}}
		require.NoError(t, yaml.Unmarshal(input, &p))
		_, hasMain := p.Profiles["main"]
		assert.True(t, hasMain, "direct profile entry must override inherited null")
		assert.Equal(t, "http://main", p.Profiles["main"].URL)
		_, hasKeep := p.Profiles["keep"]
		assert.False(t, hasKeep, "explicit profiles mapping must replace pre-seeded map")
	})

	t.Run("direct profile mapping overrides inherited mapping", func(t *testing.T) {
		t.Parallel()
		input := []byte(strings.Join([]string{
			"profiles:",
			"  <<: &base",
			"    main: {url: http://inherited}",
			"  main: {url: http://direct}",
		}, "\n"))
		var p ProxyConfig
		require.NoError(t, yaml.Unmarshal(input, &p))
		_, hasMain := p.Profiles["main"]
		assert.True(t, hasMain, "direct profile mapping must survive")
		assert.Equal(t, "http://direct", p.Profiles["main"].URL, "direct profile mapping must override inherited mapping")
	})

	t.Run("self-cycle rejected transactionally", func(t *testing.T) {
		t.Parallel()
		input := []byte(strings.Join([]string{
			"<<: &a",
			"  enabled: true",
			"  <<: *a",
		}, "\n"))
		p := ProxyConfig{
			Enabled:  true,
			Profile:  "seed",
			Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}},
		}
		err := yaml.Unmarshal(input, &p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cycle")
		assert.True(t, p.Enabled, "receiver must be unchanged on cycle error")
		assert.Equal(t, "seed", p.Profile)
		_, hasKeep := p.Profiles["keep"]
		assert.True(t, hasKeep, "pre-seeded profile must survive cycle error")
		assert.Equal(t, "seed-keep", p.Profiles["keep"].URL)
	})
}

func TestResolveAlias(t *testing.T) {
	t.Parallel()

	t.Run("nil node", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, resolveAlias(nil))
	})
	t.Run("non-alias returns itself", func(t *testing.T) {
		t.Parallel()
		n := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		assert.Equal(t, n, resolveAlias(n))
	})
	t.Run("alias chain resolves to target", func(t *testing.T) {
		t.Parallel()
		target := &yaml.Node{Kind: yaml.MappingNode}
		mid := &yaml.Node{Kind: yaml.AliasNode, Alias: target}
		head := &yaml.Node{Kind: yaml.AliasNode, Alias: mid}
		assert.Equal(t, target, resolveAlias(head))
	})
	t.Run("alias with nil target returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, resolveAlias(&yaml.Node{Kind: yaml.AliasNode, Alias: nil}))
	})
}

func TestProxyConfig_UnmarshalYAML_ProfilesMappingMergeCycleRejected(t *testing.T) {
	t.Parallel()

	input := []byte(strings.Join([]string{
		"profiles:",
		"  <<: &a",
		"    main: null",
		"    <<: *a",
	}, "\n"))
	p := ProxyConfig{
		Enabled:  true,
		Profile:  "seed",
		Profiles: map[string]ProxyProfile{"keep": {URL: "seed-keep"}},
	}
	err := yaml.Unmarshal(input, &p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
	assert.True(t, p.Enabled, "receiver must be unchanged on profiles merge cycle error")
	assert.Equal(t, "seed", p.Profile)
	_, hasKeep := p.Profiles["keep"]
	assert.True(t, hasKeep, "pre-seeded profile must survive profiles merge cycle error")
	assert.Equal(t, "seed-keep", p.Profiles["keep"].URL)
}
