import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import type { ComponentProps } from 'svelte';
import FieldRow from './FieldRow.svelte';

type FieldRowProps = ComponentProps<typeof FieldRow>;

function makeProps(overrides: Partial<FieldRowProps> = {}): FieldRowProps {
	return {
		fieldName: 'series',
		fieldLabel: 'Series',
		priority: ['r18dev', 'dmm'],
		globalPriority: ['r18dev', 'dmm'],
		status: 'inherited',
		onEdit: () => {},
		onReset: () => {},
		...overrides
	};
}

describe('FieldRow', () => {
	it('shows the inherited (green) state with no reset button', () => {
		const { container } = render(FieldRow, { props: makeProps() });

		expect(container.textContent).toContain('Inherited');
		const dot = container.querySelector('[role="img"]');
		expect(dot?.className).toContain('bg-green-500');

		// inherited rows offer no reset action
		expect(container.querySelector('[aria-label="Reset to global priority"]')).toBeNull();
	});

	it('shows the custom (orange) state with a reset button', () => {
		const { container } = render(
			FieldRow,
			{ props: makeProps({ status: 'custom', priority: ['tokyohot'] }) }
		);

		expect(container.textContent).toContain('Custom');
		const dot = container.querySelector('[role="img"]');
		expect(dot?.className).toContain('bg-orange-500');

		// the override scraper is rendered by name
		expect(container.textContent).toContain('Tokyo-Hot');

		// custom rows offer a reset action
		expect(container.querySelector('[aria-label="Reset to global priority"]')).toBeTruthy();
	});

	it('shows the skipped (grey) state and hides the __skip__ scraper name', () => {
		const { container } = render(
			FieldRow,
			{ props: makeProps({ status: 'skipped', priority: ['__skip__'] }) }
		);

		expect(container.textContent).toContain('Skipped');
		const dot = container.querySelector('[role="img"]');
		expect(dot?.className).toContain('bg-slate-400');

		// skipped rows explain the suppression in plain language
		expect(container.textContent).toContain('left empty');

		// the raw sentinel must never leak into the UI as a scraper name
		expect(container.textContent).not.toContain('__skip__');

		// skipped rows offer a reset (re-enable via parent) action
		expect(container.querySelector('[aria-label="Reset to global priority"]')).toBeTruthy();
	});

	it('renders the scraper chain for inherited/custom and not for skipped', () => {
		const inherited = render(FieldRow, { props: makeProps() });
		expect(inherited.container.textContent).toContain('R18.dev');
		expect(inherited.container.textContent).toContain('→');

		const skipped = render(
			FieldRow,
			{ props: makeProps({ status: 'skipped', priority: ['__skip__'] }) }
		);
		expect(skipped.container.textContent).not.toContain('R18.dev');
	});
});
