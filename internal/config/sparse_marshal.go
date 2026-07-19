package config

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/models"
	"gopkg.in/yaml.v3"
)

func boolPtr(b bool) *bool { return &b }

// SparseSaveContext carries the defaults, schema, and known scraper names for sparse persistence.
type SparseSaveContext struct {
	Defaults          *Config
	Schema            *yaml.Node
	KnownScraperNames map[string]bool
	ScraperDefaults   map[string]models.ScraperSettings
}

var alwaysEmitPaths = map[string]bool{
	"config_version": true,
}

type configDocumentFunc func(*Config) (*yaml.Node, error)

//nolint:unused // public API wrapper for diffYAMLDocumentsWith
func diffYAMLDocuments(actual, defaults *Config) (*yaml.Node, error) {
	return diffYAMLDocumentsWith(actual, defaults, configToYAMLDocument)
}

func diffYAMLDocumentsWith(actual, defaults *Config, toDocument configDocumentFunc) (*yaml.Node, error) {
	if actual == nil {
		return nil, nil
	}
	if defaults == nil {
		defaults = DefaultConfig(nil, nil)
	}
	actualDoc, err := toDocument(actual)
	if err != nil {
		return nil, fmt.Errorf("marshal actual: %w", err)
	}
	defaultDoc, err := toDocument(defaults)
	if err != nil {
		return nil, fmt.Errorf("marshal defaults: %w", err)
	}
	aRoot := mappingRoot(actualDoc)
	dRoot := mappingRoot(defaultDoc)
	if aRoot == nil || dRoot == nil {
		return nil, fmt.Errorf("expected mapping document roots")
	}
	out := &yaml.Node{Kind: yaml.MappingNode}
	diffMappings(aRoot, dRoot, out, "")
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{out}}, nil
}

func resolveScraperOverridesForDiff(cfg, defaults *Config, scraperDefaults map[string]models.ScraperSettings) (*Config, *Config) {
	if len(scraperDefaults) == 0 || cfg == nil {
		return cfg, defaults
	}
	diffCfg := cfg.Clone()
	seeded := defaults.Clone()
	if seeded.Scrapers.Overrides == nil {
		seeded.Scrapers.Overrides = make(map[string]*models.ScraperSettings, len(diffCfg.Scrapers.Overrides))
	}
	for name, override := range diffCfg.Scrapers.Overrides {
		def, ok := scraperDefaults[name]
		if !ok {
			continue
		}
		resolved := override.Clone()
		resolved.MergeDefaultsFrom(def)
		resolved.MergeEnabledDefault(def)
		diffCfg.Scrapers.Overrides[name] = &resolved
		d := def.Clone()
		seeded.Scrapers.Overrides[name] = &d
	}
	return diffCfg, seeded
}

func mappingRoot(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return n.Content[0]
	}
	if n.Kind == yaml.MappingNode {
		return n
	}
	return nil
}

func diffMappings(actual, defaults, out *yaml.Node, path string) {
	if actual == nil || actual.Kind != yaml.MappingNode {
		return
	}
	defaultByKey := indexMapping(defaults)
	emitted := make(map[string]bool, len(actual.Content)/2)
	for i := 0; i+1 < len(actual.Content); i += 2 {
		keyNode := actual.Content[i]
		valNode := actual.Content[i+1]
		key := keyNode.Value
		if emitted[key] {
			continue
		}
		emitted[key] = true
		childPath := joinPath(path, key)
		defVal := defaultByKey[key]
		if alwaysEmitPaths[childPath] {
			appendKV(out, keyNode, valNode)
			continue
		}
		if defVal == nil {
			appendKV(out, keyNode, valNode)
			continue
		}
		if nodesEqual(valNode, defVal) {
			continue
		}
		if valNode.Kind == yaml.MappingNode && defVal.Kind == yaml.MappingNode {
			if len(valNode.Content) == 0 {
				continue
			}
			childOut := &yaml.Node{Kind: yaml.MappingNode}
			diffMappings(valNode, defVal, childOut, childPath)
			if len(childOut.Content) > 0 {
				appendKV(out, keyNode, childOut)
			}
			continue
		}
		appendKV(out, keyNode, valNode)
	}
	actualKeys := indexKeys(actual)
	for i := 0; i+1 < len(defaults.Content); i += 2 {
		defKey := defaults.Content[i]
		defVal := defaults.Content[i+1]
		if actualKeys[defKey.Value] {
			continue
		}
		if alwaysEmitPaths[joinPath(path, defKey.Value)] {
			continue
		}
		if defVal.Kind != yaml.MappingNode || len(defVal.Content) == 0 {
			continue
		}
		nullVal := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
		appendKV(out, defKey, nullVal)
	}
}

func indexMapping(n *yaml.Node) map[string]*yaml.Node {
	m := make(map[string]*yaml.Node)
	if n == nil || n.Kind != yaml.MappingNode {
		return m
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if _, exists := m[k]; !exists {
			m[k] = n.Content[i+1]
		}
	}
	return m
}

func indexKeys(n *yaml.Node) map[string]bool {
	m := make(map[string]bool)
	if n == nil || n.Kind != yaml.MappingNode {
		return m
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		m[n.Content[i].Value] = true
	}
	return m
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func appendKV(out, key, val *yaml.Node) {
	out.Content = append(out.Content, cloneYAMLNode(key), cloneYAMLNode(val))
}

func nodesEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var ab, bb bytes.Buffer
	encA, encB := yaml.NewEncoder(&ab), yaml.NewEncoder(&bb)
	encA.SetIndent(4)
	encB.SetIndent(4)
	if err := encA.Encode(a); err != nil {
		return false
	}
	if err := encB.Encode(b); err != nil {
		return false
	}
	_ = encA.Close()
	_ = encB.Close()
	return bytes.Equal(ab.Bytes(), bb.Bytes())
}

func reconcileSparse(dst, sparseTarget, schemaDoc *yaml.Node, knownScraperNames map[string]bool) {
	dRoot := mappingRoot(dst)
	sRoot := mappingRoot(sparseTarget)
	kRoot := mappingRoot(schemaDoc)
	if dRoot == nil || sRoot == nil {
		return
	}
	reconcileMappings(dRoot, sRoot, kRoot, knownScraperNames, "")
}

func reconcileMappings(dst, src, known *yaml.Node, knownScraperNames map[string]bool, path string) {
	if dst == nil || src == nil {
		return
	}
	if dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {
		replacement := cloneYAMLNode(src)
		applyNodeMetadataPreservingComments(dst, replacement)
		*dst = *replacement
		return
	}
	dstByKey := indexMapping(dst)
	knownByKey := indexMapping(known)
	emittedDst := make(map[string]bool, len(src.Content)/2)
	result := make([]*yaml.Node, 0, len(src.Content)+len(dst.Content))
	for i := 0; i+1 < len(src.Content); i += 2 {
		srcKey := src.Content[i]
		srcVal := src.Content[i+1]
		if dstVal, ok := dstByKey[srcKey.Value]; ok {
			emittedDst[srcKey.Value] = true
			dstKeyIdx := findMappingValueIndex(dst, srcKey.Value) - 1
			dstKey := dst.Content[dstKeyIdx]
			knownVal := knownByKey[srcKey.Value]
			if dstVal.Kind == yaml.MappingNode && srcVal.Kind == yaml.MappingNode {
				if path == "scrapers" && staticScraperKeys[srcKey.Value] {
					reconcileMappings(dstVal, srcVal, knownVal, knownScraperNames, joinPath(path, srcKey.Value))
				} else if path == "scrapers" && knownScraperNames[srcKey.Value] {
					scraperSchema := buildScraperSettingsSchema()
					reconcileMappings(dstVal, srcVal, scraperSchema, knownScraperNames, joinPath(path, srcKey.Value))
				} else if knownVal != nil && knownVal.Kind == yaml.MappingNode {
					reconcileMappings(dstVal, srcVal, knownVal, knownScraperNames, joinPath(path, srcKey.Value))
				} else {
					replacement := cloneYAMLNode(srcVal)
					applyNodeMetadataPreservingComments(dstVal, replacement)
					*dstVal = *replacement
				}
				result = append(result, dstKey, dstVal)
			} else {
				replacement := cloneYAMLNode(srcVal)
				applyNodeMetadataPreservingComments(dstVal, replacement)
				*dstVal = *replacement
				result = append(result, dstKey, dstVal)
			}
		} else {
			result = append(result, cloneYAMLNode(srcKey), cloneYAMLNode(srcVal))
		}
	}
	for i := 0; i+1 < len(dst.Content); i += 2 {
		dstKey := dst.Content[i]
		dstVal := dst.Content[i+1]
		if emittedDst[dstKey.Value] {
			continue
		}
		if isSourceAuthoritativeFreeFormMap(path) {
			continue
		}
		knownVal, isKnown := knownByKey[dstKey.Value]
		isKnownScraper := path == "scrapers" && knownScraperNames[dstKey.Value]
		if isKnown || isKnownScraper {
			if dstVal.Kind == yaml.MappingNode && knownVal != nil && knownVal.Kind == yaml.MappingNode {
				emptySrc := &yaml.Node{Kind: yaml.MappingNode}
				reconcileMappings(dstVal, emptySrc, knownVal, knownScraperNames, joinPath(path, dstKey.Value))
				if len(dstVal.Content) > 0 {
					result = append(result, dstKey, dstVal)
				}
			}
			continue
		}
		result = append(result, cloneYAMLNode(dstKey), cloneYAMLNode(dstVal))
	}
	dst.Content = result
}

var (
	scraperSettingsSchemaOnce sync.Once
	scraperSettingsSchema     *yaml.Node
)

func isSourceAuthoritativeFreeFormMap(path string) bool {
	if path == "output.download_proxy.profiles" {
		return true
	}
	return strings.HasPrefix(path, "scrapers.") &&
		(strings.HasSuffix(path, ".proxy.profiles") || strings.HasSuffix(path, ".download_proxy.profiles"))
}

type yamlMarshalFunc func(any) ([]byte, error)
type yamlParseFunc func([]byte) (*yaml.Node, error)

func buildScraperSettingsSchema() *yaml.Node {
	scraperSettingsSchemaOnce.Do(func() {
		scraperSettingsSchema = buildScraperSettingsSchemaWith(yaml.Marshal, parseYAMLDocument)
	})
	return scraperSettingsSchema
}

func buildScraperSettingsSchemaWith(marshal yamlMarshalFunc, parse yamlParseFunc) *yaml.Node {
	s := models.ScraperSettings{
		Enabled:    true,
		Language:   "xx",
		Timeout:    1,
		RateLimit:  1,
		RetryCount: 1,
		UserAgent:  "xx",
		Proxy: &models.ProxyConfig{
			Enabled:        true,
			Profile:        "xx",
			DefaultProfile: "xx",
			Profiles:       map[string]models.ProxyProfile{"xx": {URL: "xx"}},
		},
		DownloadProxy: &models.ProxyConfig{
			Enabled:        true,
			Profile:        "xx",
			DefaultProfile: "xx",
			Profiles:       map[string]models.ProxyProfile{"xx": {URL: "xx"}},
		},
		BaseURL:                "xx",
		UseFlareSolverr:        true,
		UseBrowser:             true,
		ScrapeActress:          boolPtr(true),
		Cookies:                map[string]string{"k": "v"},
		PlaceholderThresholdKB: 1,
		ExtraPlaceholderHashes: []string{"xx"},
		ScrapeBonusScreens:     true,
		APIKey:                 "xx",
		RespectRetryAfter:      boolPtr(true),
	}
	data, err := marshal(&s)
	if err != nil {
		return nil
	}
	doc, err := parse(data)
	if err != nil {
		return nil
	}
	schema := mappingRoot(doc)
	schema.Content = append(schema.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "request_delay", Tag: "!!str"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "0"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: "max_retries", Tag: "!!str"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "0"},
	)
	return schema
}
