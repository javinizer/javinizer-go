package scraperconfig

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// ActressNameOverride is an explicit field split, not an inferred identity or
// nationality. SourceName guards against applying an old rule to a changed label.
type ActressNameOverride struct {
	SourceName string `yaml:"source_name" json:"source_name"`
	FirstName  string `yaml:"first_name" json:"first_name"`
	LastName   string `yaml:"last_name" json:"last_name"`
}

// ActressNameOverrides is keyed by JavLibrary's opaque vl_star.php?s= ID.
// There are no built-in entries and these mappings never inherit module defaults.
type ActressNameOverrides map[string]ActressNameOverride

// UnmarshalJSON decodes a replacement map, not a merge. Explicit {} or
// null clears existing overrides; an omitted field leaves them untouched.
func (m *ActressNameOverrides) UnmarshalJSON(data []byte) error {
	type plain ActressNameOverrides
	var next plain
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	*m = ActressNameOverrides(next)
	return nil
}

// UnmarshalYAML decodes a replacement map rather than merging existing rules.
func (m *ActressNameOverrides) UnmarshalYAML(node *yaml.Node) error {
	type plain ActressNameOverrides
	var next plain
	if err := node.Decode(&next); err != nil {
		return err
	}
	*m = ActressNameOverrides(next)
	return nil
}

var performerIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

// Valid reports whether a rule only explicitly splits/reorders the supplied
// multi-part non-native-script label. Native and single-stage names retain their
// existing routing. Whitespace is insignificant; spelling and case are not.
func (o ActressNameOverride) Valid() bool {
	source := strings.Join(strings.Fields(o.SourceName), " ")
	first := strings.Join(strings.Fields(o.FirstName), " ")
	last := strings.Join(strings.Fields(o.LastName), " ")
	if first == "" || last == "" || strings.ContainsFunc(source, func(r rune) bool {
		return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana)
	}) {
		return false
	}
	return source == first+" "+last || source == last+" "+first
}

// Validate checks the source scope, performer IDs, and explicit field splits.
func (m ActressNameOverrides) Validate(scraperName string) error {
	if len(m) > 0 && scraperName != "javlibrary" {
		return fmt.Errorf("%s: actress_name_overrides is supported only by javlibrary", scraperName)
	}
	for id, rule := range m {
		if !performerIDPattern.MatchString(id) || !rule.Valid() {
			return fmt.Errorf("%s: invalid actress_name_overrides entry %q: require an alphanumeric performer ID and source_name matching the nonempty first_name/last_name in either order", scraperName, id)
		}
	}
	return nil
}
