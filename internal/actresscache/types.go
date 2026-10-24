package actresscache

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const formatName = "javinizer.actress-evidence"
const schemaVersion = 1

// Limits bounds encoded input, each line and string, aggregate strings, and collections.
// Every budget must be positive; byte limits must leave room for overflow detection.
type Limits struct {
	MaxBytes            int64
	MaxRecordBytes      int
	MaxRecords          int
	MaxStringBytes      int
	MaxTotalStringBytes int64
	MaxAliases          int
	MaxExternalIDs      int
	MaxTokensPerRecord  int
}

// Provenance retains importer assertions, not a source authenticity guarantee.
type Provenance struct {
	SourceURL      string    `json:"source_url"`
	DatasetSHA256  string    `json:"dataset_sha256"`
	ParserRevision string    `json:"parser_revision"`
	ObservedAt     time.Time `json:"observed_at"`
}

// ExternalID is an exact, unnormalized namespace/value claim.
type ExternalID struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

// Metadata contains source claims with no canonical identity or credit authority.
type Metadata struct {
	FirstName    string   `json:"first_name,omitempty"`
	LastName     string   `json:"last_name,omitempty"`
	JapaneseName string   `json:"japanese_name,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	ThumbnailURL string   `json:"thumbnail_url,omitempty"`
}

// Evidence is one source-owned record, retained independently of other sources.
type Evidence struct {
	Source      string       `json:"source"`
	SourceID    string       `json:"source_id"`
	Provenance  Provenance   `json:"provenance"`
	Metadata    Metadata     `json:"metadata"`
	ExternalIDs []ExternalID `json:"external_ids,omitempty"`
}

// SourceKey encodes a canonical source namespace and opaque stable source ID.
// Mutable names, metadata, external-ID claims and input positions do not affect it.
func SourceKey(source, sourceID string) (string, error) {
	if !validNamespace(source) || !validID(sourceID) {
		return "", fmt.Errorf("invalid source namespace or stable source ID")
	}
	return "v1:" + source + ":" + base64.RawURLEncoding.EncodeToString([]byte(sourceID)), nil
}

func validNamespace(value string) bool {
	if len(value) < 1 || len(value) > 32 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validID(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func cloneEvidence(value Evidence) Evidence {
	value.Metadata.Aliases = append([]string(nil), value.Metadata.Aliases...)
	value.ExternalIDs = append([]ExternalID(nil), value.ExternalIDs...)
	return value
}

func ownEvidence(value Evidence) Evidence {
	value = cloneEvidence(value)
	value.Source = strings.Clone(value.Source)
	value.SourceID = strings.Clone(value.SourceID)
	value.Provenance.SourceURL = strings.Clone(value.Provenance.SourceURL)
	value.Provenance.DatasetSHA256 = strings.Clone(value.Provenance.DatasetSHA256)
	value.Provenance.ParserRevision = strings.Clone(value.Provenance.ParserRevision)
	value.Metadata.FirstName = strings.Clone(value.Metadata.FirstName)
	value.Metadata.LastName = strings.Clone(value.Metadata.LastName)
	value.Metadata.JapaneseName = strings.Clone(value.Metadata.JapaneseName)
	value.Metadata.ThumbnailURL = strings.Clone(value.Metadata.ThumbnailURL)
	for i, alias := range value.Metadata.Aliases {
		value.Metadata.Aliases[i] = strings.Clone(alias)
	}
	for i, id := range value.ExternalIDs {
		value.ExternalIDs[i] = ExternalID{strings.Clone(id.Namespace), strings.Clone(id.Value)}
	}
	return value
}
