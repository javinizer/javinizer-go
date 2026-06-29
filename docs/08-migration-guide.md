# Migration Guide: PowerShell to Go

Guide for migrating from the original PowerShell Javinizer to Javinizer Go.

## Key Differences

| Feature | PowerShell | Go | Notes |
|---------|-----------|-----|-------|
| Config Format | JSON | YAML | Manual (see Step 2) |
| Actress Data | CSV (jvThumbs.csv) | SQLite Database | See Step 4 |
| Genre Mapping | CSV (jvGenres.csv) | SQLite Database | See Step 3 |
| Performance | Slower | Much faster | Native binary |
| Cross-platform | Windows-focused | All platforms | Single binary |
| Dependencies | PowerShell modules | None | Self-contained |

## Performance Snapshot

Approximate real-world behavior from project testing:

| Operation | PowerShell | Go | Typical Improvement |
|-----------|-----------|-----|---------------------|
| Scraping | ~5s per ID | ~1.5s per ID | ~3x faster |
| File operations | Slower | Faster | ~10x faster |
| Metadata cache lookups | CSV-based | SQLite | Large improvement |
| Startup | Slower module load | Native binary startup | Faster startup |

## Compatibility Notes

- NFO output is compatible with Kodi/Plex workflows.
- Javinizer Go can be run alongside PowerShell Javinizer during migration.
- Database/storage formats are different, so PowerShell and Go databases are not directly interchangeable.

## Migration Steps

### 1. Install Javinizer Go

```bash
# Download binary or build from source
javinizer init
```

`javinizer init` creates a default configuration file and initializes the SQLite database. The config path defaults to `configs/config.yaml`; override it with `--config <path>`.

### 2. Convert Configuration

The PowerShell version used `jvSettings.json`. Javinizer Go uses YAML.

**PowerShell (jvSettings.json):**
```json
{
  "sort.metadata.priority.actress": ["r18dev", "dmm"],
  "sort.metadata.priority.title": ["r18dev", "dmm"]
}
```

**Go (config.yaml):**
```yaml
metadata:
  priority:
    actress:
      - r18dev
      - dmm
    title:
      - r18dev
      - dmm
```

> **Note:** The priority order above is an illustrative conversion of the example PowerShell settings, not the Go defaults. Go's default per-field `metadata.priority` is `dmm`-first (e.g. `actress: [dmm, r18dev, libredmm]`), while the global `scrapers.priority` list defaults to `r18dev`-first. See [Configuration](./02-configuration.md) for the full default priority lists.

> **Config versioning:** Go config files carry a `config_version` field (currently `3`). On load, older Go configs (v0/v1/v2) are auto-migrated by backing up the existing file and writing a fresh config from defaults — **custom settings are not preserved**, so porting your PowerShell settings by hand (as above) is recommended. Preview with `javinizer config migrate --dry-run`. This applies to older Go YAML configs only, not to PowerShell `jvSettings.json`.

### 3. Migrate Genre Replacements

**PowerShell (jvGenres.csv):**
```csv
Original,Replacement
Blow,Blowjob
Creampie,Cream Pie
```

**Recommended: bulk import from JSON**

`javinizer genre import <input.json>` loads many replacements at once. The expected format is a JSON array of objects with `original` and `replacement` fields:

```json
[
  { "original": "Blow", "replacement": "Blowjob" },
  { "original": "Creampie", "replacement": "Cream Pie" }
]
```

Convert your CSV to this JSON, then import:

```bash
# Convert jvGenres.csv → genres.json (requires Python 3)
python3 -c "
import csv, json
with open('jvGenres.csv', newline='') as f:
    rows = list(csv.DictReader(f))
json.dump([{'original': r['Original'], 'replacement': r['Replacement']} for r in rows],
          open('genres.json', 'w'), indent=2)
"

# Import into the database
javinizer genre import genres.json
```

The import deduplicates: entries whose `original` already exists with the same `replacement` are skipped; all others are upserted. On completion it prints `Imported: N, Skipped: N, Errors: N`.

**Alternative: add entries one at a time**

```bash
javinizer genre add "Blow" "Blowjob"
javinizer genre add "Creampie" "Cream Pie"
```

**Alternative: batch script (CSV loop)**

```bash
#!/bin/bash
# migrate-genres.sh

# Parse CSV and add to database
tail -n +2 jvGenres.csv | while IFS=, read -r original replacement; do
  javinizer genre add "$original" "$replacement"
done
```

**Back up or transfer replacements:**

```bash
javinizer genre export replacements-backup.json   # write to a file
javinizer genre export                             # print to stdout
javinizer genre list                               # show a table
```

### 4. Migrate Actress Data

PowerShell stored actress thumbnails in `jvThumbs.csv`. Go stores actress records — name, Japanese name, thumbnail URL, aliases, and DMM ID — in a SQLite table and manages them through the `actress` command.

**Bulk import from JSON** (`javinizer actress import <input.json>`). The expected format is a JSON array of objects with these fields:

```json
[
  {
    "first_name": "Momo",
    "last_name": "Sakura",
    "japanese_name": "桜空もも",
    "thumb_url": "https://example.com/momo.jpg",
    "aliases": "もも|Sakura Momo",
    "dmm_id": 12345
  }
]
```

| Field | Description |
|-------|-------------|
| `first_name` / `last_name` | Romanized name parts |
| `japanese_name` | Japanese name (used for dedup when `id` is absent) |
| `thumb_url` | Thumbnail image URL |
| `aliases` | Pipe-separated alias list |
| `dmm_id` | DMM actress ID (optional; `0` if unknown) |

```bash
javinizer actress import actresses.json
```

The import deduplicates by `id` when present, otherwise by `japanese_name`: identical records are skipped, differing records are updated, and new records are created. It prints `Imported: N, Skipped: N, Errors: N`.

**Back up, transfer, or merge actresses:**

```bash
javinizer actress export actresses-backup.json     # back up to a file
javinizer actress export                            # print to stdout
javinizer actress merge --target 12 --source 34     # merge #34 into #12 (dedup)
javinizer actress merge --target 12 --source 34 --non-interactive --prefer target -y
```

Map your `jvThumbs.csv` columns to the JSON fields above, then import. Actress records are also populated automatically as you scrape, so manual import is only needed to preserve an existing thumbnail or name mapping.

## Workflow Comparison

### PowerShell Workflow

```powershell
# Import module
Import-Module Javinizer

# Set location
Set-JavinizerLocation -Input "C:\Videos"

# Run
Javinizer -Path "C:\Videos"
```

### Go Workflow

```bash
# Initialize (once)
javinizer init

# Run
javinizer sort ~/Videos
```

## Tips for Migration

1. **Keep PowerShell version**: Run both in parallel during migration
2. **Test on copies**: Don't process your main library immediately
3. **Compare results**: Scrape same IDs in both versions
4. **Dry run first**: Always use `--dry-run` in Go version
5. **Backup data**: Keep CSV files as backup reference

---

**Next**: [Development Guide](./09-development.md)
