package nfo

import (
	"encoding/xml"
	"strings"
)

// SetString is the movie series/collection name. It marshals in the tiered
// Kodi 19+ format expected by Jellyfin, Plex, and Emby:
//
//	<set>
//	  <name>Collection Name</name>
//	</set>
//
// Unmarshaling accepts both the tiered form above and the legacy flat form
// <set>Collection Name</set> produced by older javinizer versions, so existing
// NFOs on disk continue to round-trip correctly.
type SetString string

// MarshalXML emits the tiered <set><name>...</name></set> structure. Empty
// values are omitted via the struct tag's omitempty.
func (s SetString) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if s == "" {
		return nil
	}
	return e.EncodeElement(struct {
		Name string `xml:"name"`
	}{Name: string(s)}, start)
}

// UnmarshalXML accepts both the tiered <set><name>...</name></set> form and
// the legacy flat <set>Name</set> form. When a <set> element contains both a
// <name> child and bare character data, the <name> child value wins regardless
// of document order. Other child elements (e.g. <overview>) are skipped.
// All direct character data is accumulated (so a legacy flat value split
// across multiple CharData tokens, e.g. via CDATA or comments, is preserved)
// and trimmed of surrounding whitespace only at the closing element.
func (s *SetString) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var flat strings.Builder
	var name string
	hasName := false
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.CharData:
			flat.Write(t)
		case xml.StartElement:
			if t.Name.Local == "name" {
				var n string
				if err := d.DecodeElement(&n, &t); err != nil {
					return err
				}
				name = strings.TrimSpace(n)
				hasName = true
			} else if err := d.Skip(); err != nil {
				return err
			}
		case xml.EndElement:
			if hasName {
				*s = SetString(name)
			} else {
				*s = SetString(strings.TrimSpace(flat.String()))
			}
			return nil
		}
	}
}
