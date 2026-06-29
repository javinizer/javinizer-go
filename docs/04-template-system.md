# Template System

Javinizer Go uses a flexible template system for customizing folder and file names. This guide covers all available tags, modifiers, and examples.

## Table of Contents

- [Template Syntax](#template-syntax)
- [Available Tags](#available-tags)
- [Modifiers](#modifiers)
  - [Case Modifiers](#case-modifiers)
  - [Date Modifiers](#date-modifiers)
  - [Delimiter Modifiers](#delimiter-modifiers)
  - [Language Modifiers](#language-modifiers)
  - [Actress Tag Modifiers](#actress-tag-modifiers)
- [Conditional Logic](#conditional-logic)
- [Examples](#examples)
- [Advanced Usage](#advanced-usage)
  - [Handling Missing Data](#handling-missing-data)
  - [Multiple Actresses](#multiple-actresses)
  - [Actress Name Ordering](#actress-name-ordering)
  - [Actress Language (Japanese Names)](#actress-language-japanese-names)
  - [Group Actress Organization](#group-actress-organization)
  - [Combining Tags](#combining-tags)
  - [NFO Templates](#nfo-templates)
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
<ID:lower>
```

Result:
```
ipx-535
```

The modifiers available depend on the tag (see [Modifiers](#modifiers) below). For example, case modifiers only work on `<ID>` and `<CONTENTID>`.

## Available Tags

### Basic Information

| Tag | Description | Example |
|-----|-------------|---------|
| `<ID>` | JAV ID | `IPX-535` |
| `<CONTENTID>` | Content ID (lowercase, no hyphen) | `ipx00535` |
| `<TITLE>` | Movie title (supports [language modifiers](#language-modifiers)) | `Beautiful Day` |
| `<ORIGINALTITLE>` | Japanese/alternate title (supports [language modifiers](#language-modifiers)) | `美しい日` |

### Production Information

| Tag | Description | Example |
|-----|-------------|---------|
| `<STUDIO>` or `<MAKER>` | Studio/maker name (supports [language modifiers](#language-modifiers)) | `Idea Pocket` |
| `<LABEL>` | Label name (supports [language modifiers](#language-modifiers)) | `IP Label` |
| `<SERIES>` or `<SET>` | Series name (supports [language modifiers](#language-modifiers)) | `Tsubomi Series` |
| `<DIRECTOR>` | Director name (supports [language modifiers](#language-modifiers)) | `John Director` |

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
| `<ACTRESSES>` or `<ACTORS>` | All actresses (joined by `actress_delimiter`, or group name when `group_actress` is enabled) | `Sakura Momo, Mikami Yua` |
| `<ACTRESSES:modifiers>` | Actress tag with modifiers (language, name order, delimiter) | See [Actress Tag Modifiers](#actress-tag-modifiers) |
| `<ACTRESS>` | First actress name | `Sakura Momo` |
| `<ACTRESSNAME>` or `<ACTORNAME>` | First actress name (same as `<ACTRESS>`, used for `.actors` image filenames) | `Sakura Momo` |
| `<FIRSTNAME>` | First actress first name | `Momo` |
| `<LASTNAME>` | First actress last name | `Sakura` |

> **Name ordering:** By default, actress names are displayed in Japanese naming convention (LastName FirstName, e.g., `Sakura Momo`). Set `output.first_name_order: true` to use Western ordering (FirstName LastName, e.g., `Momo Sakura`). You can also use the tag-level `:FIRST` modifier (e.g., `<ACTORS:FIRST>`) to override per-tag. See [Actress Name Ordering](#actress-name-ordering).

> **Japanese names:** Set `output.actress_language_ja: true` to prefer Japanese names (e.g., `波多野結衣`) over Latin names (e.g., `Hatano Yui`) for all actress tags. You can also use the tag-level `:JA` modifier (e.g., `<ACTORS:JA>`) to override per-tag. See [Actress Language (Japanese Names)](#actress-language-japanese-names).

> **Group actress:** When `output.group_actress` is enabled and a movie has multiple actresses, `<ACTRESSES>` returns the group name (default: `@Group`) instead of listing individual names. When the actress list is empty or unknown, `<ACTRESSES>` returns the `group_unknown_actress_name` value (default: `@Unknown`). See [Group Actress Organization](#group-actress-organization).

### Categories

| Tag | Description | Example |
|-----|-------------|---------|
| `<GENRES>` | All genres (comma-separated) | `Solowork, Beautiful Girl` |
| `<GENRES:delimiter>` | Custom delimiter | `Solowork & Beautiful Girl` |

### Metadata

| Tag | Description | Example |
|-----|-------------|---------|
| `<DESCRIPTION>` | Description/plot (supports [language modifiers](#language-modifiers)) | `Long description text...` |
| `<RATING>` | Rating score (one decimal) | `7.5` |
| `<RESOLUTION>` | Video resolution (e.g., 1080p, 720p) | `1080p` |
| `<FILENAME>` | Original filename (without extension) | `IPX-535` |
| `<FILENAME_EXT>` (alias `<FILENAMEEXT>`) | Original filename (with extension) | `IPX-535.mp4` |

### Multipart

| Tag | Description | Example |
|-----|-------------|---------|
| `<PART>` or `<DISC>` | Part/disc number for multi-part files | `1`, `2` |
| `<PARTSUFFIX>` | Part suffix (e.g., `-cd1`, `-pt1`) | `-cd1` |
| `<INDEX>` | Index number (for screenshots) | `1`, `2`, `3` |
| `<MULTIPART>` | Whether file is multi-part (for conditional logic) | `true`/empty |

## Modifiers

Modifiers change how tag values are displayed. Add them after a colon:

```
<TAG:modifier>
```

### Case Modifiers

Convert tag values to a specific case. **Case modifiers are only supported on the `<ID>` and `<CONTENTID>` tags.** Other tags (such as `<TITLE>`, `<MAKER>`, etc.) do not apply case conversion — `:upper`/`:lower` on those tags is treated as an unrecognized modifier and the original value is returned unchanged.

| Modifier | Description | Example |
|----------|-------------|--------|
| `:upper` or `:uppercase` | Convert to UPPERCASE | `<CONTENTID:upper>` → `IPX00535` |
| `:lower` or `:lowercase` | Convert to lowercase | `<ID:lower>` → `ipx-535` |

Title-case conversion is not supported. An unrecognized modifier is ignored and the original value is returned unchanged. To change a non-ID field's case, preprocess the value in your scraper/config or use the `<ACTORS>` tag's modifier syntax for actress names.

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
  folder_format: "<ID> - <TITLE> (<RELEASEDATE:YYYY-MM-DD>)"
```

Result:
```
IPX-535 - Beautiful Day (2020-09-13)
```

> **Slash separators don't survive path sanitization.** A format like `<RELEASEDATE:YYYY/MM/DD>` renders `2020/09/13`, but when used in `folder_format` the `/` characters are sanitized to `_` (yielding `2020_09_13`), and in `file_format` they become `-` (yielding `2020-09-13`). Use `-` or `.` separators if you want the date to appear literally in a name, or split the date across `subfolder_format` entries (for example, `["<YEAR>", "<RELEASEDATE:MM>", "<RELEASEDATE:DD>"]`) to turn it into nested folders.

### Delimiter Modifiers

Change how multiple values are joined.

**Actress delimiter (config-level):**

The `output.actress_delimiter` setting controls how actress names are joined when using `<ACTORS>` or `<ACTRESSES>` without a tag-level delimiter modifier:

```yaml
output:
  actress_delimiter: " | "
```

Result:
```
Sakura Momo | Mikami Yua | Anzai Rara
```

Default: `", "` (comma + space).

> **Legacy `delimiter` key:** If your config uses the old `output.delimiter` key, it will be automatically migrated to `actress_delimiter` on load. Update your config to use `actress_delimiter` to avoid the migration shim.

**Actress delimiter (tag-level `DELIM=` modifier):**

Use `DELIM=<value>` in the actress tag to override the joiner per-tag. This takes precedence over `actress_delimiter`:

```yaml
output:
  actress_delimiter: ", "
  folder_format: "<ACTORS:DELIM= | >"
```

Result (on-disk folder name):
```
Sakura Momo - Mikami Yua - Anzai Rara
```

> **Note:** `DELIM= | ` overrides `actress_delimiter` to join with ` | `, but `|` is sanitized to `-` in folder names (see [Special Characters](#special-characters)). The raw, pre-sanitization join is `Sakura Momo | Mikami Yua | Anzai Rara`.

The `DELIM=` value is the literal string after the `=` sign. It can contain spaces, commas, or any other characters:

| Template | Joiner | Result |
|----------|--------|--------|
| `<ACTORS:DELIM=\|>` | `\|` | `Sakura Momo\|Mikami Yua` |
| `<ACTORS:DELIM= & >` | ` & ` | `Sakura Momo & Mikami Yua` |
| `<ACTORS:DELIM=,>` | `,` | `Sakura Momo,Mikami Yua` |
| `<ACTORS:DELIM=>` | *(empty)* | `Sakura MomoMikami Yua` |

> **Hard break on legacy delimiter syntax:** The older form `<ACTORS:|>` (where the character after the colon was treated as a delimiter) is no longer supported. Characters after the colon are now interpreted as keywords only. If no recognized keyword or `DELIM=` prefix is found, the modifier is ignored and the configured `actress_delimiter` is used instead. Migrate to `<ACTORS:DELIM=|>` for explicit delimiter control.

**Genres with custom delimiter:**

The `<GENRES>` tag still uses the legacy inline-delimiter syntax:

```yaml
output:
  file_format: "<ID> [<GENRES:, >]"
```

Result:
```
IPX-535 [Solowork, Beautiful Girl, Slender]
```

### Language Modifiers

Some fields support multiple language translations. Use language modifiers to specify which language version to display:

**Syntax:**
```
<TAG:XX>
```

Where `XX` is a 2-letter ISO 639-1 language code (e.g., `en`, `ja`, `zh`, `ko`).

**Supported translatable fields:**

| Tag | Languages Available |
|-----|---------------------|
| `<TITLE:XX>` | Movie title in specified language |
| `<ORIGINALTITLE:XX>` | Original title in specified language |
| `<DESCRIPTION:XX>` | Description in specified language |
| `<DIRECTOR:XX>` | Director name in specified language |
| `<MAKER:XX>` or `<STUDIO:XX>` | Studio name in specified language |
| `<LABEL:XX>` | Label name in specified language |
| `<SERIES:XX>` or `<SET:XX>` | Series name in specified language |
| `<ACTORS:JA>` | Actress names in Japanese (see [Actress Tag Modifiers](#actress-tag-modifiers)) |
| `<ACTORS:EN>` | Actress names in Latin script (see [Actress Tag Modifiers](#actress-tag-modifiers)) |

**Examples:**

```yaml
output:
  folder_format: "<ID> - <TITLE:ja> (<TITLE:en>)"
```

Result:
```
IPX-535 - 美しい日 (Beautiful Day)
```

**Bilingual folder names:**
```yaml
output:
  folder_format: "<ID> [<TITLE:ja>] - <TITLE:en>"
```

Result:
```
IPX-535 [美しい日] - Beautiful Day
```

**Japanese director and studio:**
```yaml
output:
  folder_format: "<ID> by <DIRECTOR:ja> - <MAKER:ja>"
```

Result:
```
IPX-535 by 田中太郎 - アイデアポケット
```

**Fallback behavior:**

If a translation in the requested language is not available:
1. Falls back to the base field (no language specified)
2. If base field is also empty, returns empty string

**Note:** Language data availability depends on the scraper. Currently, only R18.dev provides both English (`en`) and Japanese (`ja`) translations in a single response. Other scrapers would need multiple requests to fetch different languages.

### Actress Tag Modifiers

The `<ACTORS>` and `<ACTRESSES>` tags support a rich modifier syntax that combines language, name order, and delimiter controls in a single tag. This is more powerful than the general language/case modifiers because it handles the unique multi-value nature of actress lists.

**Syntax:**
```
<ACTORS:modifier1,modifier2,modifier3>
```

Multiple modifiers are comma-separated and can appear in any order (though `DELIM=` must come last if the delimiter value contains commas).

**Available modifiers:**

| Modifier | Description | Example |
|----------|-------------|--------|
| `JA` | Prefer Japanese names (e.g., `波多野結衣` instead of `Hatano Yui`) | `<ACTORS:JA>` |
| `EN` | Prefer Latin-script names (e.g., `Hatano Yui`) | `<ACTORS:EN>` |
| `JA\|EN` | Japanese with English fallback when Japanese name is unavailable | `<ACTORS:JA\|EN>` |
| `FIRST` or `FIRSTNAMEORDER` | Force FirstName LastName order | `<ACTORS:FIRST>` |
| `LAST` or `LASTNAMEORDER` | Force LastName FirstName order (default) | `<ACTORS:LAST>` |
| `DELIM=<value>` | Override the joiner between actress names | `<ACTORS:DELIM=\|>` |

**Combining modifiers:**

| Template | Result | Explanation |
|----------|--------|-------------|
| `<ACTORS>` | `Hatano Yui, Uehara Ai` | Uses `actress_delimiter` config (default `", "`) |
| `<ACTORS:JA>` | `波多野結衣, 上原亜衣` | Japanese names, default delimiter |
| `<ACTORS:FIRST>` | `Yui Hatano, Ai Uehara` | First-name order, default delimiter |
| `<ACTORS:DELIM=\|>` | `Hatano Yui\|Uehara Ai` | Custom delimiter, Latin names |
| `<ACTORS:JA,DELIM=\|>` | `波多野結衣\|上原亜衣` | Japanese + custom delimiter |
| `<ACTORS:JA,FIRST>` | `結衣 波多野, 亜衣 上原` | Japanese + first-name order |
| `<ACTORS:JA,FIRST,DELIM=\|>` | `結衣 波多野\|亜衣 上原` | All three combined |
| `<ACTORS:DELIM=>` | `Hatano YuiUehara Ai` | Empty delimiter joins with nothing |

> **Tag-level vs config-level:** Tag-level modifiers override the config-level settings (`actress_language_ja`, `first_name_order`, `actress_delimiter`) for that specific tag only. Other tags in the same template are unaffected.

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
- With actresses: `IPX-535 starring Sakura Momo, Mikami Yua`
- Without actresses: `IPX-535`

### Use Cases

1. **Optional metadata**: Show fields only when available
2. **Fallback values**: Use ELSE for default text
3. **Clean formatting**: Avoid empty brackets or parentheses
4. **Dynamic structure**: Adjust format based on data availability

## Examples

### Folder Formats

> **Single folder vs. nested paths:** `folder_format` renders **one** folder name. Any `/` or `\` in the rendered value is replaced with `_` — it does **not** create a nested folder. To build a multi-level hierarchy (for example, Studio → Year → Movie), use `subfolder_format`, a list where each entry becomes one folder level (empty results are skipped). `folder_format` is always the deepest folder, the one that holds the files.

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

**Studio/Year Organization (nested via `subfolder_format`):**
```yaml
output:
  subfolder_format: ["<STUDIO>", "<YEAR>"]
  folder_format: "<ID> - <TITLE>"
```
Result: `Idea Pocket/2020/IPX-535 - Beautiful Day/`

> **Don't put `/` in `folder_format` to nest.** `folder_format: "<STUDIO>/<YEAR>/<ID> - <TITLE>"` renders `Idea Pocket/2020/IPX-535 - Beautiful Day` and then sanitizes the slashes to underscores, producing a single folder named `Idea Pocket_2020_IPX-535 - Beautiful Day` — almost never what you want. Use `subfolder_format` for nesting.

**Actress-based (nested via `subfolder_format`):**
```yaml
output:
  subfolder_format: ["<ACTRESSES>"]
  folder_format: "<ID> - <TITLE>"
```
Result: `Sakura Momo/IPX-535 - Beautiful Day/`

> **Note:** Actress names use LastName FirstName order by default. Set `first_name_order: true` for FirstName LastName order, or use the tag-level `:FIRST` modifier.

**Date-based (nested via `subfolder_format`):**
```yaml
output:
  subfolder_format: ["<YEAR>", "<RELEASEDATE:MM>"]
  folder_format: "<ID> - <TITLE>"
```
Result: `2020/09/IPX-535 - Beautiful Day/`

> There is no `<MONTH>` or `<DAY>` tag. Derive month/day components from `<RELEASEDATE:format>` (for example, `<RELEASEDATE:MM>` → `09`, `<RELEASEDATE:DD>` → `13`; see [Date Modifiers](#date-modifiers)). Put each path level in its own `subfolder_format` list entry — embedding `/` inside a single entry (for example, `<RELEASEDATE:YYYY/MM>`) turns the slash into `_` and yields `2020_09` instead of `2020/09`.

**Content ID:**
```yaml
output:
  folder_format: "<CONTENTID> - <TITLE>"
```
Result: `ipx00535 - Beautiful Day/`

**Bilingual (Japanese/English):**
```yaml
output:
  folder_format: "<ID> - <TITLE:ja> (<TITLE:en>)"
```
Result: `IPX-535 - 美しい日 (Beautiful Day)/`

**Japanese actress names (nested via `subfolder_format`):**
```yaml
output:
  actress_language_ja: true
  subfolder_format: ["<ACTRESSES>"]
  folder_format: "<ID> - <TITLE>"
```
Result: `波多野結衣, 上原亜衣/IPX-535 - Beautiful Day/`

**Mixed: Japanese folder, Latin file (nested via `subfolder_format`):**
```yaml
output:
  subfolder_format: ["<ACTORS:JA>"]
  folder_format: "<ID> - <TITLE>"
  file_format: "<ID> - <ACTORS>"
```
Result:
```
波多野結衣, 上原亜衣/IPX-535 - Beautiful Day/
  IPX-535 - Hatano Yui, Uehara Ai.mp4
```

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
Result: `IPX-535 - Sakura Momo, Mikami Yua - Beautiful Day.mp4`

**With pipe-delimited actresses:**
```yaml
output:
  file_format: "<ID> - <ACTORS:DELIM=|>"
```
Result: `IPX-535 - Sakura Momo-Mikami Yua.mp4`

> **Note:** The `|` character is sanitized to `-` in file and folder names (see [Special Characters](#special-characters)), so the requested join `Sakura Momo|Mikami Yua` is written to disk as `Sakura Momo-Mikami Yua`. Use `DELIM=&` or `DELIM=, ` for a separator that survives sanitization.

**With Date:**
```yaml
output:
  file_format: "<ID> (<RELEASEDATE:YYYY-MM-DD>)"
```
Result: `IPX-535 (2020-09-13).mp4`

`<RELEASEDATE>` (no modifier) defaults to the same `YYYY-MM-DD` form, so `<ID> (<RELEASEDATE>)` produces the identical result. There are no `<MONTH>` or `<DAY>` tags.

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
  subfolder_format: ["<STUDIO>", "<YEAR>"]
  folder_format: "<ID> - <TITLE> (<ACTORS:DELIM= & >)"
  file_format: "<ID> - <TITLE>"
```
Result:
```
Idea Pocket/
  2020/
    IPX-535 - Beautiful Day (Sakura Momo & Mikami Yua)/
      IPX-535 - Beautiful Day.mp4
```

**Multi-part Files:**
```yaml
output:
  file_format: "<ID>-pt<PART>"
```
Result:
```
IPX-535-pt1.mp4
IPX-535-pt2.mp4
```

Use `<PART>` (or its alias `<DISC>`) for the part/disc number. The default `file_format` is `<ID><IF:MULTIPART>-pt<PART></IF>`, which only appends the `-pt<N>` suffix for multi-part files (see [Conditional Logic](#conditional-logic)). Note that `<INDEX>` is the screenshot index, not the part number.

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

Result (with default `actress_delimiter: ", "`):
```
IPX-535 - Sakura Momo, Mikami Yua, Anzai Rara
```

**Changing the delimiter globally:**

```yaml
output:
  actress_delimiter: " & "
```

Result:
```
IPX-535 - Sakura Momo & Mikami Yua & Anzai Rara
```

**Changing the delimiter per-tag:**

```yaml
output:
  subfolder_format: ["<ACTORS:DELIM= & >"]
  folder_format: "<ID> - <TITLE>"
  file_format: "<ID> - <ACTORS:DELIM=_>"
```

Result:
```
Sakura Momo & Mikami Yua/IPX-535 - Beautiful Day/
  IPX-535 - Sakura Momo_Mikami Yua.mp4
```

The same `<ACTORS>` list is joined with ` & ` in the folder path and with `_` in the file name — the `DELIM=` override applies per tag.

**First actress only:**

Use `<ACTRESS>` or `<ACTRESSNAME>`:

```
<ID> - <ACTRESS>
```

Result:
```
IPX-535 - Sakura Momo
```

Or use `<FIRSTNAME>` and `<LASTNAME>` for individual name components:

```
<ID> - <FIRSTNAME> <LASTNAME>
```

Result:
```
IPX-535 - Momo Sakura
```

### Actress Name Ordering

By default, actress names in templates follow the Japanese naming convention (**LastName FirstName**):

```
Sakura Momo, Hatano Yui
```

**Config-level:** Set `first_name_order` in your config to change the default globally:

```yaml
output:
  first_name_order: true
```

Result:
```
Momo Sakura, Yui Hatano
```

**Tag-level:** Use the `:FIRST` or `:LAST` modifier to override per-tag:

```yaml
output:
  first_name_order: false
  subfolder_format: ["<ACTORS:FIRST>"]
  folder_format: "<ID> - <TITLE>"
```

Result (even though config says LastName FirstName, the actress subfolder uses FirstName LastName):
```
Momo Sakura, Yui Hatano/IPX-535 - Beautiful Day/
```

| Modifier | Effect |
|----------|--------|
| `:FIRST` or `:FIRSTNAMEORDER` | Force FirstName LastName order |
| `:LAST` or `:LASTNAMEORDER` | Force LastName FirstName order |

This affects all actress-related tags:

| Tag | `first_name_order: false` (default) | `first_name_order: true` |
|-----|--------------------------------------|--------------------------|
| `<ACTRESSES>` | `Sakura Momo, Hatano Yui` | `Momo Sakura, Yui Hatano` |
| `<ACTRESS>` | `Sakura Momo` | `Momo Sakura` |
| `<ACTRESSNAME>` | `Sakura Momo` | `Momo Sakura` |
| `<ACTRESSES:FIRST>` | `Momo Sakura, Yui Hatano` | `Momo Sakura, Yui Hatano` |

> **Note:** `<FIRSTNAME>` and `<LASTNAME>` always return the raw name components regardless of `first_name_order`. They are not affected by this setting.

> **NFO names are separate:** The `nfo.first_name_order` setting controls actress name formatting inside NFO files independently. It defaults to `true` (FirstName LastName) following the Kodi/Plex convention, while `output.first_name_order` defaults to `false` (LastName FirstName) following the Japanese naming convention.

### Actress Language (Japanese Names)

By default, actress names are displayed in Latin script (e.g., `Hatano Yui`). You can switch to Japanese names (e.g., `波多野結衣`) using the `actress_language_ja` setting or the `:JA` tag modifier.

**Config-level:** Set `actress_language_ja` in your config to prefer Japanese names everywhere:

```yaml
output:
  actress_language_ja: true
```

Result:
```
波多野結衣, 上原亜衣
```

**Tag-level:** Use `:JA` or `:EN` to override per-tag:

```yaml
output:
  actress_language_ja: false
  subfolder_format: ["<ACTORS:JA>"]
  folder_format: "<ID> - <TITLE>"
```

Result:
```
波多野結衣, 上原亜衣/IPX-535 - Beautiful Day/
```

**Fallback:** Use `JA|EN` to prefer Japanese but fall back to Latin when the Japanese name is unavailable:

```yaml
output:
  subfolder_format: ["<ACTORS:JA|EN>"]
  folder_format: "<ID> - <TITLE>"
```

| Modifier | Effect |
|----------|--------|
| `:JA` | Prefer Japanese names |
| `:EN` | Prefer Latin-script names |
| `:JA\|EN` | Japanese with Latin fallback |

**Combining with other modifiers:**

```yaml
output:
  subfolder_format: ["<ACTORS:JA,FIRST,DELIM= & >"]
  folder_format: "<ID>"
```

Result:
```
結衣 波多野 & 亜衣 上原/IPX-535
```

> **Note:** A `|` delimiter would be sanitized to `-` in folder and file names, so prefer `&`, `-`, or `_` for delimiters that end up in paths.

> **Note:** `actress_language_ja` only affects the `<ACTORS>`/`<ACTRESSES>`/`<ACTRESS>`/`<ACTRESSNAME>` tags. Other fields like `<TITLE>` use the general [language modifiers](#language-modifiers) (e.g., `<TITLE:ja>`).

### Group Actress Organization

When a movie has multiple actresses, you can organize them into a shared group folder instead of listing all names. This is controlled by `output.group_actress`:

```yaml
output:
  group_actress: true
  # group_actress_name: "@Group"  # Custom group folder name (default: @Group)
  # group_unknown_actress_name: "@Unknown"  # Folder name when actress is unknown (default: @Unknown)
```

**How it works:**

When `group_actress` is enabled and `<ACTRESSES>` appears in your folder template:
- **Multiple actresses** → `<ACTRESSES>` resolves to the group name (default: `@Group`)
- **Single actress** → `<ACTRESSES>` resolves to the actress name as normal
- **No actress / unknown actress** → `<ACTRESSES>` resolves to the unknown actress name (default: `@Unknown`)

**Example with group_actress enabled:**

```yaml
output:
  group_actress: true
  subfolder_format: ["<ACTRESSES>"]
  folder_format: "<ID> - <TITLE>"
```

Results:
```
# Movie with multiple actresses:
@Group/IPX-535 - Beautiful Day/

# Movie with single actress:
Sakura Momo/IPX-535 - Solo Title/

# Movie with no known actress:
@Unknown/ABP-123 - Unknown Actress Title/
```

**Custom group name:**

```yaml
output:
  group_actress: true
  group_actress_name: "Multi"
```

Result:
```
Multi/IPX-535 - Beautiful Day/
```

**Custom unknown actress name:**

```yaml
output:
  group_actress: true
  group_unknown_actress_name: "Various"
```

Result:
```
Various/ABP-123 - Unknown Actress Title/
```

> **Important:** `group_actress` only affects the `<ACTRESSES>` tag behavior. If your folder template does not contain `<ACTRESSES>`, the group organization will not apply. Files are organized into the destination folder directly.

**Combining with `first_name_order` and `actress_language_ja`:**

```yaml
output:
  group_actress: true
  first_name_order: true
  actress_language_ja: true
  subfolder_format: ["<ACTRESSES>"]
  folder_format: "<ID> - <TITLE>"
```

Results:
```
# Multiple actresses: group name is used (unaffected by first_name_order / actress_language_ja)
@Group/IPX-535 - Beautiful Day/

# Single actress: name follows first_name_order + actress_language_ja
結衣 波多野/IPX-535 - Solo Title/
```

### Combining Tags

You can use multiple tags in creative ways. Remember that `folder_format` is a single folder (slashes become `_`); use `subfolder_format` for each nesting level.

**Year in multiple places:**
```yaml
output:
  subfolder_format: ["<YEAR>", "<STUDIO> [<YEAR>]"]
  folder_format: "<ID> - <TITLE>"
```

Result:
```
2020/Idea Pocket [2020]/IPX-535 - Beautiful Day
```

**Date components:**
```yaml
output:
  subfolder_format: ["<YEAR>", "<RELEASEDATE:MM - DD>"]
  folder_format: "<ID>"
```

Result:
```
2020/09 - 13/IPX-535
```

There are no `<MONTH>` or `<DAY>` tags — derive them from `<RELEASEDATE:format>` (for example, `<RELEASEDATE:MM - DD>` → `09 - 13`).

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

**Display title in NFO:**
```yaml
metadata:
  nfo:
    display_title: "<ID> - <TITLE> (<YEAR>)"
```

This appears as the `<title>` field inside the NFO file. The key is `display_title` (not `display_name`, which was removed in an earlier rename and is now ignored).

## Special Characters

### Automatic Sanitization

Javinizer automatically removes or replaces characters that are invalid in file and folder names. **File names and folder names are sanitized separately, and they differ for path separators** (`/` and `\`).

**File names** (`file_format`, NFO filenames, image filenames):

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
| `\|` | `-` | Pipe operator |

File names are also trimmed of leading/trailing spaces and dots, and runs of multiple spaces are collapsed to one.

**Folder names** (each `subfolder_format` entry and the `folder_format` result):

| Character | Replacement | Reason |
|-----------|-------------|--------|
| `/` | `_` | Directory separator (kept out of a single folder name) |
| `\` | `_` | Windows path separator |
| `:` | ` -` | Drive letter separator (Windows) |
| `*` | (removed) | Wildcard |
| `?` | (removed) | Wildcard |
| `"` | `'` | Quote |
| `<` | `(` | Redirect operator |
| `>` | `)` | Redirect operator |
| `\|` | `-` | Pipe operator |

> **Why `/` and `\` become `_` in folders:** each folder is a single path component, so a literal slash inside a rendered folder name would be ambiguous. This is why embedding `/` in `folder_format` produces a single folder with underscores instead of nested folders — use `subfolder_format` (one entry per level) to build a hierarchy.

**Trailing dots and spaces (folder names):**

Windows does not allow folder names to end with a space or dot. When a rendered folder name ends with `.` or ` .`, Javinizer trims the trailing dots/spaces and appends a `~` marker. For example, a title truncated to `My Title...` becomes the folder `My Title~`. This also prevents SMB/NAS shares from mangling such names into short 8.3 aliases like `MYTITL~1`.

**Example:**

Title from scraper: `Love & Peace: The Movie?`

After sanitization (file name): `Love & Peace - The Movie`

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
  folder_format: "<ID> [<STUDIO>] - <TITLE> (<ACTORS:DELIM= & >)"
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
