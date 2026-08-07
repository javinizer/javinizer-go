import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import type { ComponentProps } from 'svelte';
import FormTemplateInput from './FormTemplateInput.svelte';

type Props = ComponentProps<typeof FormTemplateInput>;

function makeProps(overrides: Partial<Props> = {}): Props {
	return {
		label: 'Folder Naming Template',
		value: '',
		onchange: vi.fn(),
		showTagList: true,
		layout: 'stacked',
		...overrides,
	};
}

function buttonByText(container: HTMLElement, text: string): HTMLButtonElement {
	const btn = Array.from(container.querySelectorAll('button')).find(
		(b) => b.textContent?.trim() === text,
	);
	if (!btn) throw new Error(`button with text "${text}" not found`);
	return btn as HTMLButtonElement;
}

describe('FormTemplateInput', () => {
	it('renders the label and the current value', () => {
		const { container } = render(FormTemplateInput, { props: makeProps({ value: '<ID>' }) });
		expect(container.textContent).toContain('Folder Naming Template');
		const input = container.querySelector('input') as HTMLInputElement;
		expect(input.value).toBe('<ID>');
	});

	it('inserts a clicked tag into an empty template and notifies the parent', async () => {
		const onchange = vi.fn();
		const { container } = render(FormTemplateInput, { props: makeProps({ value: '', onchange }) });
		await fireEvent.click(buttonByText(container, 'Show available tags'));
		await waitFor(() => expect(buttonByText(container, '<RELEASEDATE>')).toBeTruthy());
		await fireEvent.click(buttonByText(container, '<RELEASEDATE>'));
		expect(onchange).toHaveBeenCalledWith('<RELEASEDATE>');
		const input = container.querySelector('input') as HTMLInputElement;
		expect(input.value).toBe('<RELEASEDATE>');
	});

	it('preserves existing template content when inserting a tag', async () => {
		const onchange = vi.fn();
		const { container } = render(FormTemplateInput, {
			props: makeProps({ value: 'IPX-123', onchange }),
		});
		const input = container.querySelector('input') as HTMLInputElement;
		input.focus();
		input.setSelectionRange(input.value.length, input.value.length);
		await fireEvent.click(buttonByText(container, 'Show available tags'));
		await fireEvent.click(buttonByText(container, '<RELEASEDATE>'));
		expect(onchange).toHaveBeenCalled();
		const result = onchange.mock.calls.at(-1)?.[0] as string;
		expect(result).toContain('IPX-123');
		expect(result).toContain('<RELEASEDATE>');
		expect(input.value).toContain('<RELEASEDATE>');
	});

	it('hides the tag picker when showTagList is false', () => {
		const { container } = render(FormTemplateInput, { props: makeProps({ showTagList: false }) });
		expect(container.textContent).not.toContain('Show available tags');
	});
});
