package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Imported sidecars carry a NOCASE index, but older and copied sidecars need
// not. Cache only a successful capability probe; cancellation must not poison
// subsequent lookups. The Store is read-only, so its index set cannot change.
func (s *Store) hasContentFoldIndex(ctx context.Context) (bool, error) {
	s.contentIndexMu.Lock()
	known, indexed := s.contentIndexKnown, s.contentFoldIndexed
	s.contentIndexMu.Unlock()
	if known {
		return indexed, nil
	}
	// Do not hold the cache lock over database I/O: another caller may have
	// an already-canceled context while this probe waits for a connection.
	// Concurrent duplicate probes are harmless on the read-only Store.
	rows, err := s.db.QueryContext(ctx, `SELECT 1
 FROM pragma_index_list('videos') AS il
 JOIN pragma_index_xinfo(il.name) AS ix
 WHERE il.partial = 0 AND ix.seqno = 0 AND ix.key = 1
 AND ix.name = 'content_id' AND ix.coll COLLATE NOCASE = 'NOCASE'
 LIMIT 1`)
	if err != nil {
		return false, fmt.Errorf("dump content_id index probe failed: %w", err)
	}
	defer func() { _ = rows.Close() }()
	found := false
	for rows.Next() {
		found = true
		var one int
		if err := rows.Scan(&one); err != nil {
			return false, fmt.Errorf("dump content_id index probe failed: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("dump content_id index probe failed: %w", err)
	}
	s.contentIndexMu.Lock()
	s.contentIndexKnown, s.contentFoldIndexed = true, found
	s.contentIndexMu.Unlock()
	return found, nil
}

// Bound work linearly in distinct requested aliases, never in their number of
// possible case permutations. Each candidate gets a small baseline; one shared
// extra pool handles occasional busy ranges. Wide legitimate expansions (e.g.
// AVOP) are not forced to scan simply because they contain many candidates.
// Exhaustion falls back to the original SQL, never a partial identity bucket.
const contentIDAliasSeeksPerID = 8
const extraContentIDAliasSeeks = 64

const contentIDAliasSeekSQL = `SELECT content_id FROM videos
 WHERE content_id COLLATE BINARY >= ? AND content_id COLLATE BINARY <= ?
 ORDER BY content_id COLLATE BINARY LIMIT 1`

// queryContentIDAliases uses the existing BINARY primary-key prefix instead of
// scanning an old sidecar for NOCASE matches. Seek the next existing raw ID,
// then jump to the least possible ASCII-case alias after it. Absent subtrees
// of the case-permutation space are skipped in one indexed seek, not enumerated.
// No lowercase-only assumption, writable migration, or table-size cache is used.
func (s *Store) queryContentIDAliases(ctx context.Context, ids []string) ([]dumpDVDRow, error) {
	var rawIDs []string
	extraSeeks := extraContentIDAliasSeeks
	visited := make(map[string]bool)
	seen := make(map[string]bool)
	for _, id := range ids {
		high := foldContentID(id)
		if visited[high] {
			continue
		}
		visited[high] = true
		seeks := 0
		lowBytes := []byte(high)
		for i, c := range lowBytes {
			if c >= 'a' && c <= 'z' {
				lowBytes[i] = c - ('a' - 'A')
			}
		}
		low := string(lowBytes)
		probe := low
		for {
			if seeks >= contentIDAliasSeeksPerID {
				if extraSeeks == 0 {
					return s.queryContentIDsSQL(ctx, ids, true)
				}
				extraSeeks--
			}
			seeks++
			var raw string
			err := s.db.QueryRowContext(ctx, contentIDAliasSeekSQL, probe, high).Scan(&raw)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("dump content_id alias seek failed: %w", err)
			}
			if foldContentID(raw) == high && !seen[raw] {
				seen[raw] = true
				rawIDs = append(rawIDs, raw)
			}
			var more bool
			probe, more = nextContentIDAlias(low, high, raw)
			if !more {
				break
			}
		}
	}
	return s.queryContentIDs(ctx, rawIDs, false)
}

// nextContentIDAlias returns the lexicographically first spelling in the
// per-byte [low, high] ASCII-case choice set strictly after the supplied key.
// Non-ASCII bytes are fixed, matching SQLite NOCASE (including invalid UTF-8).
func nextContentIDAlias(low, high, after string) (string, bool) {
	next := []byte(low)
	for i := range next {
		if i >= len(after) || after[i] < low[i] {
			return string(next), true
		}
		if after[i] == low[i] {
			continue
		}
		if low[i] != high[i] && after[i] <= high[i] {
			next[i] = high[i]
			if after[i] < high[i] {
				return string(next), true
			}
			continue
		}
		return advanceContentIDAlias(next, low, high, i-1)
	}
	return advanceContentIDAlias(next, low, high, len(next)-1)
}

func advanceContentIDAlias(next []byte, low, high string, from int) (string, bool) {
	for i := from; i >= 0; i-- {
		if next[i] < high[i] {
			next[i] = high[i]
			copy(next[i+1:], low[i+1:])
			return string(next), true
		}
	}
	return "", false
}
