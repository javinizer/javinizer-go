# Metadata Priority Components

This directory contains components for managing granular metadata field priorities in the Javinizer web UI.

## Semantics (post-#50: exclusive per-field priority)

A per-field `metadata.priority` override is **exclusive**: only the scrapers listed in the override are consulted for that field — there is **no fallback to the global scraper list**. This restores v1 (PowerShell) Javinizer behavior.

Each metadata field has one of **three states**:

| State | Config shape | Meaning | Row indicator |
|-------|--------------|---------|---------------|
| **Inherited** | `undefined` or `[]` | Use the global scraper priority list (unchanged "inherit" path). | 🟢 green |
| **Custom** | `[scraper1, scraper2, …]` (non-empty, differs from global) | EXCLUSIVE: consult only these scrapers, in order. If none of them provide the field, it is left empty — there is no global fallback. | 🟠 orange |

### Suppressing a field

There is no skip sentinel or skip button. A field is left empty by pointing it
(at a scraper that didn't run or lacks the field — the original (v1) Javinizer
behavior. For example `series: [tokyohot]` leaves Series empty when tokyohot
isn't scraped or doesn't provide Series, with no fallback to `r18dev`/`dmm`.

`[]` means "inherit global" (matching the backend's `GetFieldPriority`, which
falls back to global for empty slices), **not** skip.

## Components

### priority.ts

Pure, unit-tested helpers — the single source of truth for field-state logic. Imported by both `MetadataPriority.svelte` and `FieldRow.svelte`.

- `getGlobalPriority(config)` — global scraper execution order.
- `getFieldPriority(config, fieldKey)` — resolves a field's effective priority (`[]`/`undefined` ⇒ global; non-empty ⇒ returned as-is, exclusive).
- `isFieldOverridden(config, fieldKey)` — `false` for `[]`/`undefined`; `true` for any non-empty override that differs from global.
- `getFieldStatus(config, fieldKey)` — coarse `'inherited' | 'custom'`, driving the row's visual indicator.
- `buildFieldPriorityOverride(config, fieldKey, priority)` — pure helper returning a new `metadata.priority` record (collapses to `[]` when equal to global).
- `applyEnabledReorderToFull(fullPriority, newEnabledOrder)` — applies a reorder of the enabled scrapers back onto the full stored list, preserving disabled scrapers; filters stale ids.

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
- `onRemove` - Optional callback fired when an item's ✕ button is clicked
- `disabled` - Disable drag/reordering
- `children` - Optional snippet for custom item rendering

### FieldRow.svelte

Displays a single metadata field with its priority configuration.

**Features:**

- Visual status indicators: 🟢 green (inherited), 🟠 orange (custom)
- Compact priority preview (the scraper `→` chain)
- Edit and reset buttons (reset is shown for any non-inherited state)
- Color + text indicators (not color alone); ARIA labels on all interactive controls

**Props:**

- `fieldName` - Internal field key (e.g. `'series'`)
- `fieldLabel` - Display label
- `priority` - Current (resolved) priority array for this field
- `globalPriority` - Global priority for comparison
- `status` - `'inherited' | 'custom'` (computed by the parent via `getFieldStatus`)
- `onEdit` - Callback to open editor
- `onReset` - Callback to reset to global (clears overrides)

### MetadataPriority.svelte

Main orchestrator component for the entire priority management system.

**Features:**

- Simple/Advanced mode toggle
- Global priority management
- Per-field override system (exclusive semantics)
- 18 metadata fields across 3 categories (Primary, Metadata, Media)
- Modal field editor with reorder, per-scraper ✕ remove, and **Add all** / **Remove all** shortcuts
- "Available scrapers" chip row to add individual scrapers back after removing
- Override count tracking
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
        series: ['tokyohot']        // tokyohot lacks Series ⇒ left empty (no fallback)
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
4. **Field Override**: Click "Edit" on a field → modal opens → reorder scrapers, remove a scraper with its ✕, or use **Add all** / **Remove all**. Add individual scrapers back from the "Available scrapers" chip row. Save stores the override only if it differs from global (otherwise `[]` ⇒ inherited).
5. **Inheritance**: If a saved field priority matches global, the override is collapsed to `[]`.
6. **Persistence**: Parent component saves via the `/api/v1/config` PUT endpoint.

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
    series: [tokyohot]        # tokyohot lacks Series ⇒ left empty (no fallback)
    # actress is absent ⇒ inherits global priority
```

## Testing

- `priority.test.ts` — unit tests for the pure state logic (`getFieldPriority`, `isFieldOverridden`, `getFieldStatus`, `buildFieldPriorityOverride`, `applyEnabledReorderToFull`).
- `FieldRow.test.ts` — render tests (`@testing-library/svelte`) asserting the two visual states (inherited/custom).

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
