package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}

	cloned := *node
	cloned.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		cloned.Content[i] = cloneYAMLNode(child)
	}
	return &cloned
}

func applyNodeMetadataPreservingComments(dst, src *yaml.Node) {
	if src.HeadComment == "" {
		src.HeadComment = dst.HeadComment
	}
	if src.LineComment == "" {
		src.LineComment = dst.LineComment
	}
	if src.FootComment == "" {
		src.FootComment = dst.FootComment
	}
	if src.Style == 0 {
		src.Style = dst.Style
	}
}

func findMappingValueIndex(node *yaml.Node, key string) int {
	if node == nil || node.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

func mergeYAMLNode(dst, src *yaml.Node) {
	if dst == nil || src == nil {
		return
	}

	if dst.Kind == yaml.MappingNode && src.Kind == yaml.MappingNode {
		for i := 0; i < len(src.Content)-1; i += 2 {
			srcKey := src.Content[i]
			srcValue := src.Content[i+1]

			dstValueIdx := findMappingValueIndex(dst, srcKey.Value)
			if dstValueIdx == -1 {
				dst.Content = append(dst.Content, cloneYAMLNode(srcKey), cloneYAMLNode(srcValue))
				continue
			}

			mergeYAMLNode(dst.Content[dstValueIdx], srcValue)
		}
		return
	}

	if dst.Kind == yaml.DocumentNode && src.Kind == yaml.DocumentNode {
		if len(dst.Content) == 0 {
			dst.Content = append(dst.Content, cloneYAMLNode(src.Content[0]))
			return
		}
		if len(src.Content) == 0 {
			return
		}
		mergeYAMLNode(dst.Content[0], src.Content[0])
		return
	}

	replacement := cloneYAMLNode(src)
	applyNodeMetadataPreservingComments(dst, replacement)
	*dst = *replacement
}

func configToYAMLDocument(cfg *Config) (*yaml.Node, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse marshaled config: %w", err)
	}

	if doc.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("invalid marshaled YAML document")
	}

	return &doc, nil
}

func parseYAMLDocument(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse YAML document: %w", err)
	}
	if doc.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("invalid YAML document")
	}
	return &doc, nil
}

func encodeYAMLDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("failed to encode YAML document: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize YAML encoding: %w", err)
	}
	return buf.Bytes(), nil
}
