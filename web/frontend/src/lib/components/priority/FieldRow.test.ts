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

	it('renders the scraper chain for inherited/custom fields', () => {
		const inherited = render(FieldRow, { props: makeProps() });
		expect(inherited.container.textContent).toContain('R18.dev');
		expect(inherited.container.textContent).toContain('→');

		const custom = render(
			FieldRow,
			{ props: makeProps({ status: 'custom', priority: ['tokyohot'] }) }
		);
		expect(custom.container.textContent).toContain('Tokyo-Hot');
	});
});
