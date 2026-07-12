import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import type { ComponentProps } from 'svelte';
import FieldRow from './FieldRow.svelte';

type FieldRowProps = ComponentProps<typeof FieldRow>;

function make_props(overrides: Partial<FieldRowProps> = {}): FieldRowProps {
	return {
		fieldName: 'series',
		fieldLabel: 'Series',
		priority: ['r18dev', 'dmm'],
		globalPriority: ['r18dev', 'dmm'],
		status: 'inherited',
		onEdit: () => {},
		onReset: () => {},
		...overrides,
	};
}

describe('FieldRow', () => {
	it('shows the inherited (green) state with no reset button', () => {
		const { container } = render(FieldRow, { props: make_props() });

		expect(container.textContent).toContain('Inherited');
		const dot = container.querySelector('[role="img"]');
		expect(dot?.className).toContain('bg-green-500');

		// inherited rows offer no reset action
		expect(container.querySelector('[aria-label="Reset to global priority"]')).toBeNull();
	});

	it('shows the custom (orange) state with a reset button', () => {
		const { container } = render(FieldRow, {
			props: make_props({ status: 'custom', priority: ['tokyohot'] }),
		});

		expect(container.textContent).toContain('Custom');
		const dot = container.querySelector('[role="img"]');
		expect(dot?.className).toContain('bg-orange-500');

		// the override scraper is rendered by name
		expect(container.textContent).toContain('Tokyo-Hot');

		// custom rows offer a reset action
		expect(container.querySelector('[aria-label="Reset to global priority"]')).toBeTruthy();
	});

	it('shows the skipped (slate/red) state for the skip sentinel and suppresses the scraper chain', () => {
		// A stored ["__skip__"] override means suppression — the field is left
		// empty (no scrapers consulted). The row shows the Skipped badge and
		// "Suppressed (no scrapers consulted)" copy instead of the scraper → chain.
		const { container } = render(FieldRow, {
			props: make_props({ status: 'skipped', priority: ['__skip__'] }),
		});

		expect(container.textContent).toContain('Skipped');
		expect(container.textContent).toContain('Suppressed (no scrapers consulted)');
		// No scraper chain / arrow for a skipped field.
		expect(container.textContent).not.toContain('→');
		expect(container.textContent).not.toContain('R18.dev');

		// Skipped rows offer a reset action (override → inherited).
		expect(container.querySelector('[aria-label="Reset to global priority"]')).toBeTruthy();
	});

	it('renders the scraper chain for inherited/custom fields', () => {
		const inherited = render(FieldRow, { props: make_props() });
		expect(inherited.container.textContent).toContain('R18.dev');
		expect(inherited.container.textContent).toContain('→');

		const custom = render(FieldRow, {
			props: make_props({ status: 'custom', priority: ['tokyohot'] }),
		});
		expect(custom.container.textContent).toContain('Tokyo-Hot');
	});

	// Regression guard for issue #105: mgstage (and fc2/javstash) fell through
	// the scraper-name table and rendered as the raw config key. A field whose
	// priority resolves to mgstage must show the proper "MGStage" label.
	it('renders the MGStage label for the mgstage config key', () => {
		const { container } = render(FieldRow, {
			props: make_props({ status: 'custom', priority: ['mgstage'] }),
		});
		expect(container.textContent).toContain('MGStage');
		expect(container.textContent).not.toContain('mgstage');
	});

	it('shows a disabled-scrapers warning for a custom + empty (all disabled) field', () => {
		// When all scrapers in a custom override are disabled, the filtered
		// priority is empty. The row should show a warning that the field
		// will be empty, NOT the global chain (which would be misleading).
		const { container } = render(FieldRow, {
			props: make_props({ status: 'custom', priority: [], globalPriority: ['r18dev', 'dmm'] }),
		});

		expect(container.textContent).toContain('Custom');
		expect(container.textContent).toContain('All scrapers disabled');
		// Should NOT show the global chain — that would be misleading.
		expect(container.textContent).not.toContain('R18.dev');
		// Custom rows offer a reset action.
		expect(container.querySelector('[aria-label="Reset to global priority"]')).toBeTruthy();
	});
});
