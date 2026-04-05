# Template System

Javinizer Go uses a flexible template system for customizing folder and file names. This guide covers all available tags, modifiers, and examples.

## Table of Contents

- [Template Syntax](#template-syntax)
- [Available Tags](#available-tags)
- [Modifiers](#modifiers)
- [Conditional Logic](#conditional-logic)
- [Examples](#examples)
- [Advanced Usage](#advanced-usage)
- [Special Characters](#special-characters)

## Template Syntax

Templates use angle brackets `<TAG>` to insert dynamic data:

```
<ID> - <TITLE> (<YEAR>)
```

Result:
```
IPX-535 - Beautiful Day (2020)
```

### With Modifiers

Add modifiers after a colon:

```
<TITLE:upper>
```

Result:
```
BEAUTIFUL DAY
```

## Available Tags

### Basic Information

| Tag | Description | Example |
|-----|-------------|---------|
| `<ID>` | JAV ID | `IPX-535` |
| `<CONTENTID>` | Content ID (lowercase, no hyphen) | `ipx00535` |
| `<TITLE>` | Movie title | `Beautiful Day` |
| `<ORIGINALTITLE>` | Japanese/alternate title | `美しい日` |

### Production Information

| Tag | Description | Example |
|-----|-------------|---------|
| `<STUDIO>` or `<MAKER>` | Studio/maker name | `Idea Pocket` |
| `<LABEL>` | Label name | `IP Label` |
| `<SERIES>` | Series name | `Tsubomi Series` |
| `<DIRECTOR>` | Director name | `John Director` |

### Date and Time

| Tag | Description | Example |
|-----|-------------|---------|
| `<YEAR>` | Release year (4 digits) | `2020` |
| `<RELEASEDATE>` | Full release date | `2020-09-13` |
| `<RELEASEDATE:format>` | Custom date format | See [Date Modifiers](#date-modifiers) |
| `<RUNTIME>` | Runtime in minutes | `120` |

### People

| Tag | Description | Example |
|-----|-------------|---------|
| `<ACTRESSES>` or `<ACTORS>` | All actresses (comma-separated) | `Momo Sakura, Yua Mikami` |
| `<ACTRESSES:delimiter>` | Custom delimiter | See [Modifiers](#modifiers) |
| `<FIRSTNAME>` | First actress first name | `Momo` |
| `<LASTNAME>` | First actress last name | `Sakura` |

### Categories

| Tag | Description | Example |
|-----|-------------|---------|
| `<GENRES>` | All genres (comma-separated) | `Solowork, Beautiful Girl` |
| `<GENRES:delimiter>` | Custom delimiter | `Solowork & Beautiful Girl` |

### Metadata

| Tag | Description | Example |
|-----|-------------|---------|
| `<DESCRIPTION>` | Description/plot | `Long description text...` |

### Indexing

| Tag | Description | Example |
|-----|-------------|---------|
| `<INDEX>` | Index number (for multi-part files) | `1`, `2`, `3` |

## Modifiers

Modifiers change how tag values are displayed. Add them after a colon:

```
<TAG:modifier>
```

### Case Modifiers

Not yet implemented - coming soon!

Planned modifiers:
- `:upper` - Convert to UPPERCASE
- `:lower` - Convert to lowercase
- `:title` - Convert To Title Case

### Date Modifiers

Customize date formatting for `<RELEASEDATE>`:

| Modifier | Description | Example |
|----------|-------------|---------|
| (none) | Default format | `2020-09-13` |
| `:YYYY-MM-DD` | ISO format | `2020-09-13` |
| `:YYYY/MM/DD` | Slash separator | `2020/09/13` |
| `:MM-DD-YYYY` | US format | `09-13-2020` |
| `:DD.MM.YYYY` | European format | `13.09.2020` |
| `:YYYYMMDD` | Compact format | `20200913` |

**Custom format examples:**

```yaml
# In config.yaml
output:
  folder_format: "<ID> - <TITLE> (<RELEASEDATE:YYYY/MM/DD>)"
```

Result:
```
IPX-535 - Beautiful Day (2020/09/13)
```

### Delimiter Modifiers

Change how multiple values are joined:

**Actresses with custom delimiter:**

```yaml
output:
  folder_format: "<ACTRESSES: & >"
```

Result:
```
Momo Sakura & Yua Mikami & Rara Anzai
```

**Genres with custom delimiter:**

```yaml
output:
  file_format: "<ID> [<GENRES:, >]"
```

Result:
```
IPX-535 [Solowork, Beautiful Girl, Slender]
```

## Conditional Logic

Conditional blocks allow you to show or hide content based on whether a tag has a value.

### Basic Syntax

```
<IF:TAG>content</IF>
```

Shows `content` only if `TAG` has a value.

### With ELSE Clause

```
<IF:TAG>true_content<ELSE>false_content</IF>
```

Shows `true_content` if `TAG` has a value, otherwise shows `false_content`.

### Examples

**Show series only if it exists:**

```yaml
output:
  folder_format: "<ID> - <TITLE><IF:SERIES> [<SERIES>]</IF>"
```

Results:
- With series: `IPX-535 - Beautiful Day [Tsubomi Series]`
- Without series: `IPX-535 - Beautiful Day`

**Show director or studio:**

```yaml
output:
  folder_format: "<IF:DIRECTOR>Director: <DIRECTOR><ELSE>Studio: <STUDIO></IF>"
```

Results:
- With director: `Director: John Smith`
- Without director: `Studio: Idea Pocket`

**Multiple conditionals:**

```yaml
output:
  folder_format: "<ID> - <TITLE><IF:YEAR> (<YEAR>)</IF><IF:LABEL> [<LABEL>]</IF>"
```

Results:
- All fields: `IPX-535 - Beautiful Day (2020) [Premium]`
- No year: `IPX-535 - Beautiful Day [Premium]`
- No label: `IPX-535 - Beautiful Day (2020)`
- Neither: `IPX-535 - Beautiful Day`

**Check for actresses:**

```yaml
output:
  folder_format: "<ID><IF:ACTRESSES> starring <ACTRESSES></IF>"
```

Results:
- With actresses: `IPX-535 starring Momo Sakura, Yua Mikami`
- Without actresses: `IPX-535`

### Use Cases

1. **Optional metadata**: Show fields only when available
2. **Fallback values**: Use ELSE for default text
3. **Clean formatting**: Avoid empty brackets or parentheses
4. **Dynamic structure**: Adjust format based on data availability

## Examples

### Folder Formats

**Default (Recommended):**
```yaml
output:
  folder_format: "<ID> [<STUDIO>] - <TITLE> (<YEAR>)"
```
Result: `IPX-535 [Idea Pocket] - Beautiful Day (2020)/`

**Simple:**
```yaml
output:
  folder_format: "<ID> - <TITLE>"
```
Result: `IPX-535 - Beautiful Day/`

**Studio/Year Organization:**
```yaml
output:
  folder_format: "<STUDIO>/<YEAR>/<ID> - <TITLE>"
```
Result: `Idea Pocket/2020/IPX-535 - Beautiful Day/`

**Actress-based:**
```yaml
output:
  folder_format: "<ACTRESSES>/<ID> - <TITLE>"
```
Result: `Momo Sakura/IPX-535 - Beautiful Day/`

**Date-based:**
```yaml
output:
  folder_format: "<YEAR>/<MONTH>/<ID> - <TITLE>"
```
Result: `2020/09/IPX-535 - Beautiful Day/`

**Content ID:**
```yaml
output:
  folder_format: "<CONTENTID> - <TITLE>"
```
Result: `ipx00535 - Beautiful Day/`

### File Formats

**ID Only (Default, Recommended):**
```yaml
output:
  file_format: "<ID>"
```
Result: `IPX-535.mp4`

**ID with Title:**
```yaml
output:
  file_format: "<ID> - <TITLE>"
```
Result: `IPX-535 - Beautiful Day.mp4`

**With Actresses:**
```yaml
output:
  file_format: "<ID> - <ACTRESSES> - <TITLE>"
```
Result: `IPX-535 - Momo Sakura - Beautiful Day.mp4`

**With Date:**
```yaml
output:
  file_format: "<ID> (<YEAR>-<MONTH>-<DAY>)"
```
Result: `IPX-535 (2020-09-13).mp4`

**Studio and ID:**
```yaml
output:
  file_format: "[<STUDIO>] <ID>"
```
Result: `[Idea Pocket] IPX-535.mp4`

### Complete Examples

**Plex-style:**
```yaml
output:
  folder_format: "<TITLE> (<YEAR>)"
  file_format: "<TITLE> (<YEAR>)"
```
Result:
```
Beautiful Day (2020)/
  Beautiful Day (2020).mp4
```

**Kodi-style:**
```yaml
output:
  folder_format: "<ID> - <TITLE>"
  file_format: "<ID>"
```
Result:
```
IPX-535 - Beautiful Day/
  IPX-535.mp4
  IPX-535.nfo
```

**Studio Organization:**
```yaml
output:
  folder_format: "<STUDIO>/<YEAR>/<ID> - <TITLE> (<ACTRESSES>)"
  file_format: "<ID> - <TITLE>"
```
Result:
```
Idea Pocket/
  2020/
    IPX-535 - Beautiful Day (Momo Sakura)/
      IPX-535 - Beautiful Day.mp4
```

**Multi-part Files:**
```yaml
output:
  file_format: "<ID>-part<INDEX>"
```
Result:
```
IPX-535-part1.mp4
IPX-535-part2.mp4
```

## Advanced Usage

### Handling Missing Data

If a tag has no data, it's replaced with an empty string:

Template:
```
<ID> [<STUDIO>] - <TITLE> (<YEAR>)
```

With missing studio:
```
IPX-535 - Beautiful Day (2020)
```

Note the extra spaces are **not** automatically removed. The template system preserves your exact formatting.

### Multiple Actresses

When multiple actresses are present:

Template:
```
<ID> - <ACTRESSES>
```

Result:
```
IPX-535 - Momo Sakura, Yua Mikami, Rara Anzai
```

**First actress only:**

Use `<FIRSTNAME>` and `<LASTNAME>`:

```
<ID> - <FIRSTNAME> <LASTNAME>
```

Result:
```
IPX-535 - Momo Sakura
```

### Combining Tags

You can use multiple tags in creative ways:

**Year in multiple places:**
```
<YEAR>/<STUDIO> [<YEAR>]/<ID> - <TITLE>
```

Result:
```
2020/Idea Pocket [2020]/IPX-535 - Beautiful Day
```

**Date components:**
```
<YEAR>/<MONTH> - <DAY>/<ID>
```

Result:
```
2020/09 - 13/IPX-535
```

### NFO Templates

NFO filename template (in metadata.nfo section):

**Default:**
```yaml
metadata:
  nfo:
    filename_template: "<ID>.nfo"
```
Result: `IPX-535.nfo`

**With title:**
```yaml
metadata:
  nfo:
    filename_template: "<ID> - <TITLE>.nfo"
```
Result: `IPX-535 - Beautiful Day.nfo`

**Display name in NFO:**
```yaml
metadata:
  nfo:
    display_name: "<ID> - <TITLE> (<YEAR>)"
```

This appears as the `<title>` field inside the NFO file.

## Special Characters

### Automatic Sanitization

Javinizer automatically removes or replaces characters that are invalid in filenames:

| Character | Replacement | Reason |
|-----------|-------------|--------|
| `/` | `-` | Directory separator |
| `\` | `-` | Windows path separator |
| `:` | ` -` | Drive letter separator (Windows) |
| `*` | (removed) | Wildcard |
| `?` | (removed) | Wildcard |
| `"` | `'` | Quote |
| `<` | `(` | Redirect operator |
| `>` | `)` | Redirect operator |
| `|` | `-` | Pipe operator |

**Example:**

Title from scraper: `Love & Peace: The Movie?`

After sanitization: `Love & Peace - The Movie`

### Manual Escaping

You don't need to manually escape characters - Javinizer handles it automatically.

## Testing Templates

Before applying templates to your library, test them:

### Method 1: Dry Run

```bash
javinizer sort ~/test --dry-run
```

This shows what the final filenames and folders will look like without making changes.

### Method 2: Info Command

```bash
javinizer info
```

Shows your current template configuration.

### Method 3: Small Test Set

Process a few files first:

```bash
# Create test directory with 2-3 files
mkdir ~/template-test
cp ~/Videos/IPX-535.mp4 ~/template-test/

# Test your template
javinizer sort ~/template-test --dry-run

# If satisfied, apply
javinizer sort ~/template-test
```

## Template Best Practices

1. **Keep it simple**: Simpler templates are easier to manage
2. **Include ID**: Always include `<ID>` for easy lookups
3. **Avoid redundancy**: Don't repeat the same info in folder and file
4. **Test first**: Always use `--dry-run` before applying new templates
5. **Consider Kodi/Plex**: Match your media server's preferred format
6. **Be consistent**: Use the same template across your library
7. **Backup first**: Test on copies before modifying originals
8. **Check lengths**: Very long templates may exceed OS path limits

### Recommended Templates

**For Kodi:**
```yaml
output:
  folder_format: "<ID> - <TITLE> (<YEAR>)"
  file_format: "<ID>"
```

**For Plex:**
```yaml
output:
  folder_format: "<TITLE> (<YEAR>)"
  file_format: "<TITLE> (<YEAR>)"
```

**For Browsing:**
```yaml
output:
  folder_format: "<ID> [<STUDIO>] - <TITLE> (<ACTRESSES>)"
  file_format: "<ID>"
```

## Troubleshooting

### Template Not Working

1. Check syntax: Tags must be in `<ANGLE_BRACKETS>`
2. Verify tag names: Use exact case (e.g., `<TITLE>` not `<Title>`)
3. Check for typos: `<ACTRESSES>` not `<ACTRESS>`
4. Run dry-run to preview

### Missing Data

If a tag shows empty:
1. Check scraper returned that field: `javinizer scrape <ID>`
2. Verify field priority in config
3. Try different scraper

### Path Too Long

If folder paths are too long (>255 characters on Windows):
1. Simplify template
2. Remove `<TITLE>` or long fields
3. Use shorter studio names
4. Organize by year/studio in parent folders

### Special Characters Issues

If you see weird characters in filenames:
1. This is automatic - Javinizer sanitizes unsafe characters
2. Check the [Special Characters](#special-characters) section
3. Titles with many special chars will be cleaned

### SMB/NAS Mangled Names (`ABC123~1`)

If folder names appear as short aliases like `ABC123~1` over SMB/NAS:
1. Upgrade to a build that trims trailing dots/spaces from generated folder names
2. Truncated titles now use a trailing `~` marker instead of `...` for SMB compatibility
3. Keep `output.max_title_length` reasonable for your share (for example, `100`)
4. Avoid extremely long nested paths (`subfolder_format` + long title-heavy folder templates)
5. If your NAS still mangles names, use a shorter folder format (for example, `<ID> - <TITLE>`)

---

**Next**: [Genre Management](./05-genre-management.md)
