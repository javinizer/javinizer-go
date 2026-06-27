# Metadata Priority Components

This directory contains components for managing granular metadata field priorities in the Javinizer web UI.

## Semantics (post-#50: exclusive per-field priority)

A per-field `metadata.priority` override is **exclusive**: only the scrapers listed in the override are consulted for that field — there is **no fallback to the global scraper list**. This restores v1 (PowerShell) Javinizer behavior.

Each metadata field has one of **three states**:

| State | Config shape | Meaning | Row indicator |
|-------|--------------|---------|---------------|
| **Inherited** | `undefined` or `[]` | Use the global scraper priority list (unchanged "inherit" path). | 🟢 green |
| **Custom** | `[scraper1, scraper2, …]` (non-empty, not the sentinel) | EXCLUSIVE: consult only these scrapers, in order. If none of them provide the field, it is left empty — there is no global fallback. | 🟠 orange |
| **Skipped** | `["__skip__"]` | Suppress the field entirely — it will be left empty. | ⚪ grey |

### The `__skip__` sentinel

`SKIP_FIELD_SENTINEL` (`'__skip__'`) is a reserved scraper name. Setting a field's priority to `["__skip__"]` is a first-class way to **skip/suppress** that field:

- The backend aggregator consults only the listed scraper (`__skip__`), and since no scraper of that name is ever registered or run, no result matches and the field is left empty.
- `PriorityConfig.Fields` stores raw strings with no registry validation, so the sentinel round-trips through config load/save cleanly.
- **No backend change was required** — this works with the existing exclusive-semantics backend (PR #51). Verified empirically: `series: ["__skip__"]` leaves `Series` empty even when `r18dev`/`dmm` provide it.

`[]` is intentionally **not** the skip sentinel — it means "inherit global" (matching the backend's `GetFieldPriority`, which falls back to global for empty slices). `["__skip__"]` is the explicit skip.

> **Design note:** An earlier option considered reusing a real scraper that lacks a field (e.g. `series: [tokyohot]`) as the skip marker. That was rejected because it is scraper-specific (tokyohot lacks Series but may provide other fields), and because detecting the "skipped" visual state would require a scraper→field capability map that the frontend does not have. The explicit `__skip__` sentinel is unconditionally detectable and field-agnostic.

## Components

### priority.ts

Pure, unit-tested helpers — the single source of truth for field-state logic. Imported by both `MetadataPriority.svelte` and `FieldRow.svelte`.

- `SKIP_FIELD_SENTINEL` — the reserved `'__skip__'` string.
- `getGlobalPriority(config)` — global scraper execution order.
- `getFieldPriority(config, fieldKey)` — resolves a field's effective priority (`[]`/`undefined` ⇒ global; non-empty ⇒ returned as-is, including the sentinel).
- `isSkipField(priority)` — `true` when the priority is exactly `["__skip__"]`.
- `isFieldOverridden(config, fieldKey)` — `false` for `[]`/`undefined`; `true` for any non-empty override that differs from global (the sentinel counts).
- `getFieldStatus(config, fieldKey)` — coarse `'inherited' | 'custom' | 'skipped'`, driving the row's visual indicator. `"skipped"` takes precedence over `"custom"`.
- `buildFieldPriorityOverride(config, fieldKey, priority)` — pure helper returning a new `metadata.priority` record (collapses to `[]` when equal to global).

### DraggableList.svelte

Reusable drag-and-drop list component with keyboard accessibility.

**Features:**

- Native HTML5 drag and drop
- Keyboard controls (up/down arrow buttons)
- Touch-friendly for mobile devices
- Disabled state support
- Custom render via snippets

**Props:**

- `items` - Array of strings to display
- `onReorder` - Callback fired when order changes
- `disabled` - Disable drag/reordering
- `children` - Optional snippet for custom item rendering

### FieldRow.svelte

Displays a single metadata field with its priority configuration.

**Features:**

- Visual status indicators: 🟢 green (inherited), 🟠 orange (custom), ⚪ grey (skipped)
- Compact priority preview (the scraper `→` chain), or a "Field will be left empty (suppressed)" note when skipped
- Skipped fields render the field label struck-through for quick scanning
- Edit and reset buttons (reset is shown for any non-inherited state, including skipped)
- Color + text indicators (not color alone); ARIA labels on all interactive controls

**Props:**

- `fieldName` - Internal field key (e.g. `'series'`)
- `fieldLabel` - Display label
- `priority` - Current (resolved) priority array for this field
- `globalPriority` - Global priority for comparison
- `status` - `'inherited' | 'custom' | 'skipped'` (computed by the parent via `getFieldStatus`)
- `onEdit` - Callback to open editor
- `onReset` - Callback to reset to global (clears overrides and skips)

### MetadataPriority.svelte

Main orchestrator component for the entire priority management system.

**Features:**

- Simple/Advanced mode toggle
- Global priority management
- Per-field override system (exclusive semantics)
- 18 metadata fields across 3 categories (Primary, Metadata, Media)
- Modal field editor with reorder **and** a first-class "Skip field" action
- "Re-enable field" action to undo a skip
- Override count tracking (skips count as overrides)
- "Show only overridden" filter

**Props:**

- `config` - Full application config object
- `onUpdate` - Callback when config changes

## Usage

```svelte
<script lang="ts">
  import MetadataPriority from '$lib/components/priority/MetadataPriority.svelte';
  import type { SettingsConfig } from '$lib/api/types';

  let config = $state<SettingsConfig>({
    scrapers: { priority: ['r18dev', 'dmm'] },
    metadata: {
      // Per-field overrides (optional). Each is EXCLUSIVE.
      priority: {
        genre: ['dmm', 'r18dev'],   // custom: consult only dmm then r18dev
        series: ['__skip__']        // skipped: leave Series empty
      }
    }
  } as SettingsConfig);

  function handleUpdate(updatedConfig: SettingsConfig) {
    config = updatedConfig;
    // Save to backend...
  }
</script>

<MetadataPriority {config} onUpdate={handleUpdate} />
```

## Data Flow

1. **Simple Mode**: User drags scrapers in the global priority list.
2. **Config Update**: `onUpdate` callback fires with a deep-cloned config.
3. **Advanced Mode**: User clicks "Advanced" to reveal per-field controls.
4. **Field Override**: Click "Edit" on a field → modal opens → reorder scrapers → Save. The override is stored only if it differs from global (otherwise `[]` ⇒ inherited).
5. **Skip Field**: In the editor, click "Skip field" → confirm → the field is staged as `["__skip__"]` → Save persists it. The row turns grey with a "Skipped" badge.
6. **Re-enable**: In the editor for a skipped field, click "Re-enable field" to restore the inherited scraper list.
7. **Inheritance**: If a saved field priority matches global, the override is collapsed to `[]`.
8. **Persistence**: Parent component saves via the `/api/v1/config` PUT endpoint.

## Field Definitions

The component manages priority for these metadata fields (snake_case keys match the API):

### Primary

- `id`, `title`, `original_title`, `description`, `release_date`, `runtime`, `content_id`

### Metadata

- `actress`, `genre`, `director`, `maker`, `label`, `series`, `rating`

### Media

- `cover_url`, `poster_url`, `screenshot_url`, `trailer_url`

## Backend Integration

The config structure matches the Go backend:

```yaml
scrapers:
  priority: [r18dev, dmm] # Global priority

metadata:
  priority:
    # Per-field overrides (optional; each is EXCLUSIVE — no global fallback)
    genre: [dmm, r18dev]      # custom override
    series: [__skip__]        # skip: leave Series empty
    # actress is absent ⇒ inherits global priority
```

## Testing

- `priority.test.ts` — unit tests for the pure state logic (`getFieldPriority`, `isFieldOverridden`, `isSkipField`, `getFieldStatus`, `buildFieldPriorityOverride`, and the skip-action config shape).
- `FieldRow.test.ts` — render tests (`@testing-library/svelte`) asserting the three visual states (inherited/custom/skipped).

Run with `npx vitest run` from `web/frontend/`.

## Accessibility

- Keyboard navigation with Tab/Shift+Tab
- Up/Down arrow buttons for reordering
- ARIA labels on all interactive controls (edit, reset, skip, re-enable, close)
- `role="img"` + `aria-label` on the status dot (color is never the only signal)
- Screen-reader-friendly status text ("Inherited" / "Custom" / "Skipped")
- Color + text indicators (not color alone)

## Mobile Considerations

- Touch-friendly drag and drop
- Full-screen modals on small screens
- Explicit up/down buttons as fallback
- Vertical stacking of elements
- Minimum touch target sizes (44x44px)

## Future Enhancements

- [ ] Scraper strength indicators (which scraper is best for which field) — would also let a skipped field be detected from real-scraper overrides
- [ ] Preset configurations ("DMM Preferred", "R18Dev Preferred")
- [ ] Export/import priority configurations
- [ ] Visual diff view showing changes from default
- [ ] Bulk operations (reset all overrides)
- [ ] Search/filter fields by name
