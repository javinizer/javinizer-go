# Migration Guide: PowerShell to Go

Guide for migrating from the original PowerShell Javinizer to Javinizer Go.

## Key Differences

| Feature | PowerShell | Go | Notes |
|---------|-----------|-----|-------|
| Config Format | JSON | YAML | Auto-converted |
| Actress Data | CSV (jvThumbs.csv) | SQLite Database | See below |
| Genre Mapping | CSV (jvGenres.csv) | SQLite Database | Use CLI commands |
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

### 3. Migrate Genre Replacements

**PowerShell (jvGenres.csv):**
```csv
Original,Replacement
Blow,Blowjob
Creampie,Cream Pie
```

**Go (CLI commands):**
```bash
javinizer genre add "Blow" "Blowjob"
javinizer genre add "Creampie" "Cream Pie"
```

**Batch Migration Script:**
```bash
#!/bin/bash
# migrate-genres.sh

# Parse CSV and add to database
tail -n +2 jvGenres.csv | while IFS=, read -r original replacement; do
  javinizer genre add "$original" "$replacement"
done
```

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
