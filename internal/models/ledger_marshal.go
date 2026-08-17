package models

import "encoding/json"

// MarshalLedgerJSON is the single place a GeneratedFilesJSON blob is compacted.
// It is branchless because GeneratedFilesJSON contains only JSON-safe value
// types. If a future field can fail encoding, handle that failure here.
func MarshalLedgerJSON(gf GeneratedFilesJSON) string {
	data, _ := json.Marshal(gf)
	return string(data)
}
