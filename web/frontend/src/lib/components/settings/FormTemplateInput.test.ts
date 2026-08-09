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

	it('appends (not prepends) a tag when the input has not been focused', async () => {
		const onchange = vi.fn();
		const { container } = render(FormTemplateInput, {
			props: makeProps({ value: '<ID> - <TITLE>', onchange }),
		});
		// Do NOT focus the input first — selectionStart is 0, not null.
		await fireEvent.click(buttonByText(container, 'Show available tags'));
		await fireEvent.click(buttonByText(container, '<RELEASEDATE>'));
		expect(onchange).toHaveBeenCalledWith('<ID> - <TITLE><RELEASEDATE>');
		const input = container.querySelector('input') as HTMLInputElement;
		expect(input.value).toBe('<ID> - <TITLE><RELEASEDATE>');
	});

	it('hides the tag picker when showTagList is false', () => {
		const { container } = render(FormTemplateInput, { props: makeProps({ showTagList: false }) });
		expect(container.textContent).not.toContain('Show available tags');
	});

	it('does not offer <INDEX> in the default tag list (it only resolves for screenshots)', async () => {
		const { container } = render(FormTemplateInput, { props: makeProps({ value: '' }) });
		await fireEvent.click(buttonByText(container, 'Show available tags'));
		await waitFor(() => expect(container.querySelector('button.font-mono')).toBeTruthy());
		const chips = Array.from(container.querySelectorAll('button.font-mono')).map(
			(b) => b.textContent,
		);
		expect(chips).not.toContain('<INDEX>');
		expect(chips).toContain('<RELEASEDATE>');
		expect(chips).toContain('<RESOLUTION>');
		expect(chips).toContain('<SET>');
		expect(chips).not.toContain('<PART>');
		expect(chips).not.toContain('<PARTSUFFIX>');
	});

	it('renders a custom tag set when the tags prop is provided', async () => {
		const { container } = render(FormTemplateInput, {
			props: makeProps({ value: '', tags: ['<ID>', '<INDEX>'] }),
		});
		await fireEvent.click(buttonByText(container, 'Show available tags'));
		await waitFor(() => expect(buttonByText(container, '<INDEX>')).toBeTruthy());
		const chips = Array.from(container.querySelectorAll('button.font-mono')).map(
			(b) => b.textContent,
		);
		expect(chips).toEqual(['<ID>', '<INDEX>']);
	});

	it('renders non-clickable chips when clickableTags is false', async () => {
		const onchange = vi.fn();
		const { container } = render(FormTemplateInput, {
			props: makeProps({ value: '', onchange, clickableTags: false }),
		});
		await fireEvent.click(buttonByText(container, 'Show available tags'));
		await waitFor(() => expect(container.querySelector('code.font-mono')).toBeTruthy());
		expect(container.querySelectorAll('button.font-mono').length).toBe(0);
		expect(container.querySelectorAll('code.font-mono').length).toBeGreaterThan(0);
		const code = container.querySelector('code.font-mono') as HTMLElement;
		await fireEvent.click(code);
		expect(onchange).not.toHaveBeenCalled();
	});
});
