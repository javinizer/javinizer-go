#!/usr/bin/env python3
"""Generate content_id_prefixes.go from an r18.dev database dump.

Usage:
    python3 generate_content_id_prefixes.py <dump.sql> <output.go>

Download the dump from: https://r18.dev/dumps/latest
It redirects to an S3 URL like: https://r18dotdev.s3.eu-west-1.wasabisys.com/dumps/r18dotdev_dump_YYYY-MM-DD.sql.gz

Steps:
    1. curl -Lo r18dev_dump.sql.gz "https://r18.dev/dumps/latest"
    2. gunzip r18dev_dump.sql.gz
    3. python3 generate_content_id_prefixes.py r18dev_dump.sql ../content_id_prefixes.go
"""

import re
import sys
from collections import defaultdict


def extract_videos(sql_path):
    """Extract (content_id, dvd_id) pairs from the COPY block for derived_video."""
    rows = []
    in_copy = False
    with open(sql_path) as f:
        for line in f:
            line = line.rstrip('\n')
            if line.startswith('COPY public.derived_video '):
                in_copy = True
                continue
            if in_copy:
                if line == '\\.':
                    break
                parts = line.split('\t')
                if len(parts) >= 2:
                    rows.append((parts[0], parts[1]))
    return rows


def main():
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <dump.sql> <output.go>")
        sys.exit(1)

    sql_path = sys.argv[1]
    output_path = sys.argv[2]

    rows = extract_videos(sql_path)
    print(f"Extracted {len(rows)} video entries")

    # Capture both DMM numeric prefixes (e.g. "118san00457" -> "118") and
    # PPV underscore prefixes (e.g. "h_796san00457" -> "h_796"). Digital-only
    # releases only have a PPV content_id with a null dvd_id, so without
    # capturing the underscore form the prefix table silently drops them.
    content_id_pattern = re.compile(r'^(?:(\d+)|([a-z])_(\d+))?([a-z]+)(\d+)$')

    def prefix_from_match(m):
        if m.group(1) is not None:
            return m.group(1)
        if m.group(2) is not None:
            return m.group(2) + '_' + m.group(3)
        return ''

    # Build: series -> set of prefixes from ALL rows
    series_prefixes = defaultdict(set)
    for cid, did in rows:
        m = content_id_pattern.match(cid)
        if not m:
            continue
        prefix = prefix_from_match(m)
        series = m.group(4)
        series_prefixes[series].add(prefix)

    # Sort prefixes: empty string first, then numeric prefixes (by length then
    # value), then PPV underscore prefixes last so standard DMM ids are probed
    # before PPV-only content_ids at runtime.
    def prefix_sort_key(p):
        if p == '':
            return (0, 0, '')
        if p.isdigit():
            return (1, len(p), int(p))
        return (2, len(p), p)

    # Generate Go source file
    lines = []
    lines.append('package r18dev')
    lines.append('')
    lines.append('//go:generate python3 scripts/generate_content_id_prefixes.py /tmp/r18dev_dump.sql content_id_prefixes.go')
    lines.append('')
    lines.append('// contentIDPrefixLookup maps series names (lowercase) to their known DMM content_id prefixes.')
    lines.append('// Built from r18.dev database dump. Regenerate with: go generate ./internal/scraper/r18dev/...')
    lines.append('var contentIDPrefixLookup = map[string][]string{')

    for series in sorted(series_prefixes.keys()):
        prefixes = sorted(series_prefixes[series], key=prefix_sort_key)
        prefix_strs = ', '.join(f'"{p}"' for p in prefixes)
