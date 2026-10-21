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
	b := []byte(id)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// ContentIDsEqualFold compares lookup aliases using SQLite's ASCII-only NOCASE
// rules. It does not establish uniqueness or replace either raw source identity.
func ContentIDsEqualFold(a, b string) bool {
	return foldContentID(a) == foldContentID(b)
}

func contentIDBuckets(rows []dumpDVDRow) map[string][]dumpDVDRow {
	buckets := make(map[string][]dumpDVDRow, len(rows))
	for _, row := range rows {
		key := foldContentID(row.contentID)
		buckets[key] = append(buckets[key], row)
	}
	return buckets
}

// selectContentID prefers an explicitly supplied raw identity only when it
// identifies one service row. Generated candidates are aliases, so their
// bucket must contain exactly one row across all raw spellings and services.
func selectContentID(rows []dumpDVDRow, id string, allowExact bool) (dumpDVDRow, bool) {
	if allowExact {
		var exact dumpDVDRow
		count := 0
		for _, row := range rows {
			if row.contentID == id {
				exact = row
				count++
			}
		}
		if count == 1 && exact.variantCount <= 1 {
			return exact, true
		}
		if count > 1 {
			return dumpDVDRow{}, false
		}
	}
	if len(rows) == 1 && rows[0].variantCount <= 1 {
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
	if folded {
		// SQLite NOCASE treats embedded NUL specially. Preserve its comparison
		// semantics for malformed/control-containing requests rather than trying
		// to enumerate their ordinary byte-wise aliases.
		for _, id := range ids {
			if strings.IndexByte(id, 0) >= 0 {
				return s.queryContentIDsSQL(ctx, ids, true)
			}
		}
		indexed, err := s.hasContentFoldIndex(ctx)
		if err != nil {
			return nil, err
		}
		if !indexed {
			return s.queryContentIDAliases(ctx, ids)
		}
	}
	return s.queryContentIDsSQL(ctx, ids, folded)
}

func (s *Store) queryContentIDsSQL(ctx context.Context, ids []string, folded bool) ([]dumpDVDRow, error) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := "SELECT content_id, dvd_id, release_date, service_code FROM videos WHERE content_id COLLATE BINARY IN (" + placeholders + ")"
	if folded {
		query = "SELECT content_id, dvd_id, release_date, service_code FROM videos WHERE content_id COLLATE NOCASE IN (" + placeholders + ")"
	}
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
