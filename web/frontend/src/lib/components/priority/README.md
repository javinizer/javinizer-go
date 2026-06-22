# Metadata Priority Components

This directory contains components for managing granular metadata field priorities in the Javinizer web UI.

## Components

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

- Visual status indicators (green = inherited, orange = custom)
- Compact priority preview
- Edit and reset buttons
- Responsive layout

**Props:**

- `fieldName` - Internal field key (e.g., 'Title')
- `fieldLabel` - Display label
- `priority` - Current priority array for this field
- `globalPriority` - Global priority for comparison
- `isOverridden` - Whether field has custom override
- `onEdit` - Callback to open editor
- `onReset` - Callback to reset to global

### MetadataPriority.svelte

Main orchestrator component for the entire priority management system.

**Features:**

- Simple/Advanced mode toggle
- Global priority management
- Per-field override system
- 18 metadata fields across 3 categories (Primary, Metadata, Media)
- Modal field editor
- Override count tracking
- "Show only overridden" filter

**Props:**

- `config` - Full application config object
- `onUpdate` - Callback when config changes

## Usage

```svelte
<script>
  import MetadataPriority from '$lib/components/priority/MetadataPriority.svelte';

  let config = $state({
    Scrapers: {
      Priority: ['r18dev', 'dmm']
    },
    Metadata: {
      Priority: {
        Genre: ['dmm', 'r18dev']  // Override for genres
      }
    }
  });

  function handleUpdate(updatedConfig) {
    config = updatedConfig;
    // Save to backend...
  }
</script>

<MetadataPriority {config} onUpdate={handleUpdate} />
```

## Data Flow

1. **Simple Mode**: User drags scrapers in global priority list
2. **Config Update**: `onUpdate` callback fires with modified config
3. **Advanced Mode**: User clicks "Advanced" to reveal per-field controls
4. **Field Override**: Click "Edit" on field → Modal opens → Reorder → Save
5. **Inheritance**: If field priority matches global, override is removed
6. **Persistence**: Parent component saves via `/api/v1/config` PUT endpoint

## Field Definitions

The component manages priority for these metadata fields:

### Primary

- ID, Title, OriginalTitle, Description, ReleaseDate, Runtime, ContentID

### Metadata

- Actress, Genre, Director, Maker, Label, Series, Rating

### Media

- CoverURL, PosterURL, ScreenshotURL, TrailerURL

## Backend Integration

The config structure matches the Go backend:

```yaml
scrapers:
  priority: [r18dev, dmm] # Global priority

metadata:
  priority:
    # Per-field overrides (optional)
    genre: [dmm, r18dev]
    actress: [r18dev, dmm]
```

## Accessibility

- Keyboard navigation with Tab/Shift+Tab
- Up/Down arrow buttons for reordering
- ARIA labels on interactive elements
- Screen reader announcements for state changes
- Color + text indicators (not color alone)

## Mobile Considerations

- Touch-friendly drag and drop
- Full-screen modals on small screens
- Explicit up/down buttons as fallback
- Vertical stacking of elements
- Minimum touch target sizes (44x44px)

## Future Enhancements

- [ ] Add scraper strength indicators (which scraper is best for which field)
- [ ] Preset configurations ("DMM Preferred", "R18Dev Preferred")
- [ ] Export/import priority configurations
- [ ] Visual diff view showing changes from default
- [ ] Bulk operations (reset all overrides)
- [ ] Search/filter fields by name
