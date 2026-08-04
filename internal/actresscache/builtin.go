package actresscache

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"sync"
)

// builtinData ...
//
//go:embed data/actresses.json.gz
var builtinData []byte

// builtinIndex ...
var builtinIndex struct {
	sync.Once
	records    []RuntimeRecord
	err        error
	byDMM      map[int]int
	byJP       map[string]int
	byName     map[string]int
	ambiguousD map[int]struct{}
	ambiguousP map[string]struct{}
	ambiguousN map[string]struct{}
}

// Builtin ...
func Builtin() (Cache, error) {
	loadBuiltin()
	cache := Cache{SchemaVersion: RuntimeSchemaVersion, Records: make([]Record, len(builtinIndex.records))}
	for i, record := range builtinIndex.records {
		cache.Records[i] = runtimeRecordToRecord(record)
	}
	return cache, builtinIndex.err
}

var marshalBuiltinCache = json.Marshal

// BuiltinData ...
func BuiltinData() []byte {
	loadBuiltin()
	if builtinIndex.err != nil {
		return nil
	}
	data, err := marshalBuiltinCache(RuntimeCache{SchemaVersion: RuntimeSchemaVersion, Records: builtinIndex.records})
	if err != nil {
		return nil
	}
	return data
}

// Lookup ...
func Lookup(dmmID int, japaneseName, firstName, lastName string) (Record, bool) {
	loadBuiltin()
	if builtinIndex.err != nil {
		return Record{}, false
	}
	if dmmID > 0 {
		if index, ok := builtinIndex.byDMM[dmmID]; ok {
			return runtimeRecordToRecord(builtinIndex.records[index]), true
		}
	}
	if name := normalizeIdentity(japaneseName); name != "" {
		if index, ok := builtinIndex.byJP[name]; ok {
			return runtimeRecordToRecord(builtinIndex.records[index]), true
		}
		// A jp-name miss is not authoritative-absent: fall through to the
		// romanized index so romanized-only records stay reachable.
	}
	// Consult the romanized index only with both name parts present: a
	// single-part key can collide with mononymous/stage-name records.
	if firstName != "" && lastName != "" {
		if index, ok := builtinIndex.byName[normalizeIdentity(firstName+" "+lastName)]; ok {
			return runtimeRecordToRecord(builtinIndex.records[index]), true
		}
	}
	return Record{}, false
}

// loadBuiltin ...
func loadBuiltin() {
	builtinIndex.Do(func() {
		cache, err := decodeRuntimeCache(bytes.NewReader(builtinData))
		if err != nil {
			builtinIndex.err = err
			return
		}
		builtinIndex.records = cache.Records
		builtinIndex.byDMM = make(map[int]int)
		builtinIndex.byJP = make(map[string]int)
		builtinIndex.byName = make(map[string]int)
		builtinIndex.ambiguousD = make(map[int]struct{})
		builtinIndex.ambiguousP = make(map[string]struct{})
		builtinIndex.ambiguousN = make(map[string]struct{})
		for index, record := range builtinIndex.records {
			addDMMIndex(builtinIndex.byDMM, builtinIndex.ambiguousD, record.DMMID, index)
			addStringIndex(builtinIndex.byJP, builtinIndex.ambiguousP, normalizeIdentity(record.JapaneseName), index)
			for _, alias := range record.Aliases {
				addStringIndex(builtinIndex.byJP, builtinIndex.ambiguousP, normalizeIdentity(alias), index)
			}
			addStringIndex(builtinIndex.byName, builtinIndex.ambiguousN, normalizeIdentity(record.FirstName+" "+record.LastName), index)
		}
	})
}

// runtimeRecordToRecord ...
func runtimeRecordToRecord(record RuntimeRecord) Record {
	return Record{
		BuiltinKey:   record.BuiltinKey,
		DMMID:        record.DMMID,
		FirstName:    record.FirstName,
		LastName:     record.LastName,
		JapaneseName: record.JapaneseName,
		Aliases:      append([]string(nil), record.Aliases...),
		ThumbURL:     record.ThumbURL,
	}
}

// addDMMIndex ...
func addDMMIndex(index map[int]int, ambiguous map[int]struct{}, dmmID, recordIndex int) {
	if dmmID <= 0 {
		return
	}
	if _, ok := ambiguous[dmmID]; ok {
		return
	}
	if existing, ok := index[dmmID]; ok && existing != recordIndex {
		delete(index, dmmID)
		ambiguous[dmmID] = struct{}{}
		return
	}
	index[dmmID] = recordIndex
}

// addStringIndex ...
func addStringIndex(index map[string]int, ambiguous map[string]struct{}, key string, recordIndex int) {
	if key == "" {
		return
	}
	if _, ok := ambiguous[key]; ok {
		return
	}
	if existing, ok := index[key]; ok && existing != recordIndex {
		delete(index, key)
		ambiguous[key] = struct{}{}
		return
	}
	index[key] = recordIndex
}
