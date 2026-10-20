package r18devdump

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// foldContentID mirrors SQLite NOCASE: ASCII casing is a lookup alias only.
// Non-ASCII characters and the stored source identity are never rewritten.
func foldContentID(id string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, id)
}

func contentIDBuckets(rows []dumpDVDRow) map[string][]dumpDVDRow {
	buckets := make(map[string][]dumpDVDRow, len(rows))
	for _, row := range rows {
		key := foldContentID(row.contentID)
		buckets[key] = append(buckets[key], row)
	}
	return buckets
}

// selectContentID prefers an explicitly supplied raw identity. Generated
// candidates are aliases, so their bucket must contain exactly one raw ID.
func selectContentID(rows []dumpDVDRow, id string, allowExact bool) (dumpDVDRow, bool) {
	if allowExact {
		for _, row := range rows {
			if row.contentID == id {
				return row, true
			}
		}
	}
	if len(rows) == 1 {
		return rows[0], true
	}
	return dumpDVDRow{}, false
}

// queryContentIDs returns raw rows without filtering by published DVD identity.
// Ambiguity must be established before filtering, never hidden by it. Newly
// imported dumps have an additive NOCASE index; older sidecars remain readable.
func (s *Store) queryContentIDs(ctx context.Context, ids []string, folded bool) ([]dumpDVDRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	collation := "BINARY"
	if folded {
		collation = "NOCASE"
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := "SELECT content_id, dvd_id, release_date, service_code FROM videos WHERE content_id COLLATE " + collation + " IN (" + strings.Repeat("?,", len(ids)-1) + "?)"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("dump content_id lookup failed: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var matches []dumpDVDRow
	for rows.Next() {
		var row dumpDVDRow
		var dvdID, releaseDate, serviceCode sql.NullString
		if err := rows.Scan(&row.contentID, &dvdID, &releaseDate, &serviceCode); err != nil {
			return nil, fmt.Errorf("dump content_id lookup failed: scan: %w", err)
		}
		row.dvdID, row.releaseDate, row.serviceCode = dvdID.String, releaseDate.String, serviceCode.String
		matches = append(matches, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dump content_id lookup failed: %w", err)
	}
	return matches, nil
}
