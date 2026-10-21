package r18devdump

import (
	"fmt"
	"strings"
)

// ImportBatchError describes rows rejected by the sidecar's required-key
// constraints. Its diagnostic contains only schema names, counts and source
// table row positions, never key values or other uploaded row contents.
// Import still rejects the entire upload and preserves the previous sidecar.
type ImportBatchError struct {
	table      string
	keyColumns []string
	firstRow   int64
	totalRows  int64
	storedRows int64
	nullRows   int64
}

func (e *ImportBatchError) Error() string {
	return fmt.Sprintf("%v: %s batch stored %d of %d rows (source table rows %d-%d; key columns: %s; %d duplicate-key rows, %d NULL-key rows)",
		ErrDumpInvalid, e.table, e.storedRows, e.totalRows,
		e.firstRow, e.firstRow+e.totalRows-1, strings.Join(e.keyColumns, "+"),
		e.totalRows-e.storedRows-e.nullRows, e.nullRows)
}

func (e *ImportBatchError) Unwrap() error { return ErrDumpInvalid }

func newImportBatchError(table string, batch []DumpRow, stored, firstRow int64) *ImportBatchError {
	e := &ImportBatchError{
		table: table, firstRow: firstRow, totalRows: int64(len(batch)), storedRows: stored,
	}
	for _, lk := range logicalKeys {
		if lk.table == sqliteTableName(table) {
			e.keyColumns = lk.keys
			break
		}
	}
	if table == derivedVideoTable {
		// The upstream key is composite. A missing service remains supported
		// for legacy small dumps; its uniqueness is enforced separately.
		e.keyColumns = []string{contentIDColumn, serviceCodeColumn}
	}
	var keyIndexes []int
	for i, column := range tableSchema[table].columns {
		for _, key := range e.keyColumns {
			if table == derivedVideoTable && key == serviceCodeColumn {
				continue // Nullable legacy service is not a rejected NULL key.
			}
			if column == key {
				keyIndexes = append(keyIndexes, i)
			}
		}
	}
	for _, row := range batch {
		for _, i := range keyIndexes {
			if row.Values[i] == nullSentinel {
				e.nullRows++
				break // A row with multiple NULL key components is rejected once.
			}
		}
	}
	// Import creates these tables itself: NOT NULL and the BINARY primary key
	// are their only rejecting constraints. All NULL-key rows are ignored;
	// the other ignored rows therefore collided with an already stored key,
	// including keys from earlier batches. Do not infer anything about which
	// source row should win, or whether the upstream logical key differs.
	return e
}
