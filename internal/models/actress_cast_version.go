package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// ActressCastVersion hashes the cast in stable identity order for edit admission.
func ActressCastVersion(actresses []Actress) string {
	ordered := append([]Actress(nil), actresses...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ID != ordered[j].ID {
			return ordered[i].ID < ordered[j].ID
		}
		return fmt.Sprint(ordered[i].DMMID, ordered[i].FirstName, ordered[i].LastName, ordered[i].JapaneseName) < fmt.Sprint(ordered[j].DMMID, ordered[j].FirstName, ordered[j].LastName, ordered[j].JapaneseName)
	})
	h := sha256.New()
	for _, actress := range ordered {
		_, _ = fmt.Fprintf(h, "%v\x00%v\x00%v\x00%v\x00%v\x00%v\x00%v\x00%v\x00%v\x00%v\n", actress.ID, actress.DMMID, actress.FirstName, actress.LastName, actress.JapaneseName, actress.ThumbURL, actress.Aliases, actress.NameKey, actress.Verified, actress.Origin)
	}
	return hex.EncodeToString(h.Sum(nil))
}
