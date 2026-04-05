# Troubleshooting

Common issues and solutions for Javinizer Go.

## Installation Issues

### "javinizer: command not found"

**Problem**: Binary not in PATH

**Solutions**:
```bash
# Option 1: Move to PATH location
sudo mv javinizer /usr/local/bin/

# Option 2: Run with full path
/path/to/javinizer --help

# Option 3: Add directory to PATH
export PATH=$PATH:/path/to/directory
```

### "Permission denied"

**Problem**: Binary not executable

**Solution**:
```bash
chmod +x javinizer
```

## Configuration Issues

### "Config file not found"

**Problem**: `config.yaml` missing

**Solution**:
```bash
# Initialize configuration
javinizer init

# Or specify custom location
javinizer --config /path/to/config.yaml scrape IPX-535
```

### "Invalid configuration"

**Problem**: Malformed YAML

**Solutions**:
- Check YAML syntax (indentation, colons, quotes)
- Validate with online YAML validator
- Compare with default config
- Run `javinizer info` to see parsed config

## Database Issues

### "Database locked"

**Problem**: Multiple instances accessing database simultaneously

**Solutions**:
```bash
# Close other Javinizer processes
killall javinizer

# Or wait for operation to complete
# Or delete lock file
rm data/javinizer.db-wal
rm data/javinizer.db-shm
```

### "No such table"

**Problem**: Database not initialized or migrated

**Solution**:
```bash
# Re-initialize database
rm data/javinizer.db
javinizer init
```

### "Database is corrupt"

**Problem**: Corrupted SQLite database

**Solutions**:
```bash
# Option 1: Delete and re-initialize
rm data/javinizer.db
javinizer init

# Option 2: Attempt repair
sqlite3 data/javinizer.db "PRAGMA integrity_check;"
sqlite3 data/javinizer.db ".recover" | sqlite3 data/javinizer-recovered.db
```

## Scraping Issues

### "Failed to scrape: timeout"

**Problem**: Network timeout or slow connection

**Solutions**:
- Check internet connection
- Try again later (site may be down)
- Use different scraper: `--scrapers dmm`
- Increase timeout in code (requires rebuild)

### "404 Not Found"

**Problem**: Movie ID doesn't exist on scraper

**Solutions**:
- Verify ID is correct
- Try different scraper
- Check if movie exists on website manually
- Some IDs only available on specific scrapers

### "No scrapers returned data"

**Problem**: All scrapers failed

**Solutions**:
```bash
# Check scraper configuration
javinizer info

# Enable scrapers in config.yaml
scrapers:
  r18dev:
    enabled: true
  dmm:
    enabled: true

# Test each scraper individually
javinizer scrape IPX-535 --scrapers r18dev
javinizer scrape IPX-535 --scrapers dmm
```

### "Rate limited"

**Problem**: Too many requests to scraper

**Solutions**:
- Wait a few minutes
- Reduce concurrent requests
- Spread out batch operations

## File Matching Issues

### "No files found"

**Problem**: Scanner didn't find any video files

**Solutions**:
```bash
# Check path exists
ls -la /path/to/videos

# Verify file extensions in config
file_matching:
  extensions: [.mp4, .mkv, .avi, .wmv, .flv]

# Check exclude patterns
file_matching:
  exclude_patterns: ["*-trailer*", "*-sample*"]

# Try recursive scan
javinizer sort /path --recursive
```

### "ID not detected"

**Problem**: Matcher couldn't extract JAV ID from filename

**Solutions**:
- Ensure filename contains JAV ID (e.g., `IPX-535`)
- Check custom regex if enabled
- Rename file to include ID clearly
- Disable custom regex to use builtin pattern

**Examples of Good Filenames**:
```
IPX-535.mp4
IPX-535 Beautiful Day.mkv
[Studio] IPX-535.avi
```

**Examples of Bad Filenames**:
```
movie.mp4
download (1).mkv
video_file.avi
```

## Organization Issues

### "File already exists"

**Problem**: Target file conflicts with existing file

**Solutions**:
- Use `--dry-run` to preview
- Manually resolve conflicts
- Use different destination directory
- Check if you've already processed this file

### "Permission denied" (during move/copy)

**Problem**: Insufficient permissions

**Solutions**:
```bash
# Check permissions
ls -la /destination/path

# Fix permissions
chmod 755 /destination/path

# Run with appropriate user
sudo javinizer sort /path
```

For Docker/Unraid deployments:
- Ensure the container runs with matching IDs (`PUID`/`PGID`, or legacy `USER_ID`/`GROUP_ID`)
- On Unraid, common values are `PUID=99` and `PGID=100`

### "Path too long"

**Problem**: File path exceeds OS limits (Windows: 260 chars)

**Solutions**:
- Simplify template format
- Remove long fields like `<TITLE>`
- Use shorter destination path
- On Windows: Enable long paths in registry

## NFO Generation Issues

### "Invalid XML"

**Problem**: Generated NFO isn't valid XML

**Solutions**:
- Check for special characters in metadata
- Report issue with movie ID
- Manually edit NFO if needed

### "NFO not recognized by Kodi/Plex"

**Problem**: Media server doesn't parse NFO

**Solutions**:
- Verify NFO filename matches video file
- Check NFO is in same directory as video
- Validate XML structure
- Check media server logs

## Download Issues

### "Failed to download cover"

**Problem**: Image download failed

**Solutions**:
- Check internet connection
- Verify URL is accessible
- Check disk space
- Retry operation

### "Downloaded file is corrupt"

**Problem**: Incomplete or corrupted download

**Solutions**:
```bash
# Delete partial download
rm /path/to/corrupt/file

# Retry download
javinizer sort /path
```

## Template Issues

### "Template not rendering"

**Problem**: Template tags not replaced with values

**Solutions**:
- Check tag syntax: `<TAG>` not `{TAG}` or `[TAG]`
- Verify tag name is correct (case-sensitive)
- Check if field has data: `javinizer scrape ID`
- Review template in config:

```yaml
output:
  folder_format: "<ID> - <TITLE> (<YEAR>)"
  file_format: "<ID>"
```

### "Special characters in filenames"

**Problem**: Unwanted characters in output

**Solutions**:
- This is automatic sanitization (expected)
- See [Template Guide](./04-template-system.md#special-characters)
- Characters like `:`, `?`, `*` are replaced automatically

## Performance Issues

### "Slow scraping"

**Problem**: Metadata fetching takes too long

**Solutions**:
- Use database cache (pre-scrape IDs)
- Enable only necessary scrapers
- Check network connection
- Consider scraper reliability (R18.dev is usually faster)

### "High memory usage"

**Problem**: Javinizer using too much RAM

**Solutions**:
- Process smaller batches
- Clear database cache if very large
- Report issue with details

## Genre Replacement Issues

### "Replacement not applied"

**Problem**: Genre still shows original name

**Solutions**:
```bash
# Verify replacement exists
javinizer genre list

# Check exact spelling (case-sensitive)
javinizer genre add "Exact Original" "Replacement"

# Re-scrape to apply
javinizer scrape IPX-535
```

## Debug Mode

Enable detailed logging:

```yaml
# config.yaml
logging:
  level: debug
  format: text
  output: stdout
```

Then run command:
```bash
javinizer sort /path --dry-run 2> debug.log
```

## Getting Help

1. **Check documentation**: Review relevant guide
2. **Search issues**: https://github.com/javinizer/javinizer-go/issues
3. **Enable debug logging**: Capture detailed output
4. **Create issue**: Provide:
   - Javinizer version
   - Operating system
   - Command used
   - Error message
   - Debug log (if applicable)

## Common Error Messages

### "no such file or directory"

- Check path exists
- Use absolute paths
- Verify permissions

### "invalid argument"

- Check command syntax
- Verify flag values
- Use quotes for paths with spaces

### "context deadline exceeded"

- Network timeout
- Increase timeout or retry
- Check internet connection

### "database schema mismatch"

- Delete database: `rm data/javinizer.db`
- Re-initialize: `javinizer init`

---

**Return to**: [Getting Started](./01-getting-started.md)
