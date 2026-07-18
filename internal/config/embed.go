package config

import (
	_ "embed"
)

//go:embed config.yaml.example
//nolint:unused
var embeddedConfig []byte

// embeddedConfigBytes returns the raw embedded config bytes.
// Use this when you need the byte slice directly (e.g., for YAML parsing).
//
//nolint:unused
func embeddedConfigBytes() []byte {
	// Return a copy to prevent mutation of the embedded data
	result := make([]byte, len(embeddedConfig))
	copy(result, embeddedConfig)
	return result
}
