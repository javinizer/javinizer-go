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
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if err := e.EncodeElement(string(s), xml.StartElement{Name: xml.Name{Local: "name"}}); err != nil {
		return err
	}
	return e.EncodeToken(start.End())
}

// UnmarshalXML accepts both the tiered <set><name>...</name></set> form and
// the legacy flat <set>Name</set> form.
func (s *SetString) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.CharData:
			if v := strings.TrimSpace(string(t)); v != "" {
				*s = SetString(v)
			}
		case xml.StartElement:
			if t.Name.Local == "name" {
				var name string
				if err := d.DecodeElement(&name, &t); err != nil {
					return err
				}
				*s = SetString(strings.TrimSpace(name))
			} else if err := d.Skip(); err != nil {
				return err
			}
		case xml.EndElement:
			return nil
		}
	}
}
