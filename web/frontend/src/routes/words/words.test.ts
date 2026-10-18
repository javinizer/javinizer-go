import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import { toastStore } from '$lib/stores/toast';
import type { WordReplacement } from '$lib/api/types';
import Words from './+page.svelte';

vi.mock('$lib/api/client', () => ({
	apiClient: { listWordReplacements: vi.fn(), createWordReplacement: vi.fn() },
}));
vi.mock('$lib/stores/toast', () => ({
	toastStore: { success: vi.fn(), error: vi.fn() },
}));

let client: QueryClient;

beforeEach(() => {
	vi.resetAllMocks();
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
	client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	vi.mocked(apiClient.listWordReplacements).mockResolvedValue({
		replacements: [],
		total: 0,
		count: 0,
		limit: 100,
		offset: 0,
	});
});

afterEach(() => {
	cleanup();
	client.clear();
});

async function mountDraft(originalValue = 'First', replacementValue = 'One') {
	const view = render(Words, {}, { wrapper: QueryClientWrapper, wrapperProps: { client } });
	const original = view.container.querySelector('#word-original') as HTMLInputElement;
	const replacement = view.container.querySelector('#word-replacement') as HTMLInputElement;
	const mode = view.container.querySelector('#word-match-mode') as HTMLSelectElement;
	const add = view.getByRole('button', { name: 'Add' }) as HTMLButtonElement;
	await fireEvent.input(original, { target: { value: originalValue } });
	await fireEvent.input(replacement, { target: { value: replacementValue } });
	return { view, original, replacement, mode, add };
}

async function startPending() {
	let resolve!: (value: WordReplacement) => void;
	let reject!: (reason: Error) => void;
	vi.mocked(apiClient.createWordReplacement).mockImplementation(
		() =>
			new Promise((done, fail) => {
				resolve = done;
				reject = fail;
			}),
	);
	const draft = await mountDraft();
	await fireEvent.click(draft.add);
	await waitFor(() => expect(apiClient.createWordReplacement).toHaveBeenCalledTimes(1));
	return {
		...draft,
		complete: () =>
			resolve({
				id: 1,
				original: 'First',
				replacement: 'One',
				match_mode: 'literal',
			} as WordReplacement),
		fail: (message = 'Add failed') => reject(new Error(message)),
	};
}

it.each(['original', 'replacement', 'mode'] as const)(
	'preserves the entire editable draft when only %s changes during Add',
	async (changed) => {
		const { original, replacement, mode, add, complete } = await startPending();
		expect(original.disabled).toBe(false);
		expect(replacement.disabled).toBe(false);
		expect(mode.disabled).toBe(false);
		if (changed === 'original') await fireEvent.input(original, { target: { value: 'Second' } });
		if (changed === 'replacement') await fireEvent.input(replacement, { target: { value: 'Two' } });
		if (changed === 'mode') await fireEvent.change(mode, { target: { value: 'wildcard' } });
		complete();
		await waitFor(() => expect(toastStore.success).toHaveBeenCalledTimes(1));
		expect(original.value).toBe(changed === 'original' ? 'Second' : 'First');
		expect(replacement.value).toBe(changed === 'replacement' ? 'Two' : 'One');
		expect(mode.value).toBe(changed === 'mode' ? 'wildcard' : 'literal');
		await waitFor(() => expect(add.disabled).toBe(false));
	},
);

it('retains whitespace edits made while Add is pending', async () => {
	const { original, replacement, complete } = await startPending();
	await fireEvent.input(original, { target: { value: ' First ' } });
	complete();
	await waitFor(() => expect(toastStore.success).toHaveBeenCalledTimes(1));
	expect(original.value).toBe(' First ');
	expect(replacement.value).toBe('One');
});

it('submits trimmed values and clears an unchanged successful draft including mode', async () => {
	vi.mocked(apiClient.createWordReplacement).mockResolvedValue({} as WordReplacement);
	const { original, replacement, mode } = await mountDraft(' First ', ' One ');
	await fireEvent.change(mode, { target: { value: 'wildcard' } });
	await fireEvent.keyDown(replacement, { key: 'Enter' });
	await waitFor(() => expect(toastStore.success).toHaveBeenCalledTimes(1));
	expect(apiClient.createWordReplacement).toHaveBeenCalledExactlyOnceWith({
		original: 'First',
		replacement: 'One',
		match_mode: 'wildcard',
	});
	expect(original.value).toBe('');
	expect(replacement.value).toBe('');
	expect(mode.value).toBe('literal');
});

it('blocks Enter and button submissions during the same event turn and pending request', async () => {
	let complete!: (value: WordReplacement) => void;
	vi.mocked(apiClient.createWordReplacement).mockImplementation(
		() =>
			new Promise((resolve) => {
				complete = resolve;
			}),
	);
	const { original, replacement, add } = await mountDraft();
	// Deliberately dispatch without an intervening render or awaited event.
	original.dispatchEvent(
		new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
	);
	add.dispatchEvent(new MouseEvent('click', { bubbles: true }));
	replacement.dispatchEvent(
		new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }),
	);
	await waitFor(() => expect(apiClient.createWordReplacement).toHaveBeenCalledTimes(1));
	await waitFor(() => expect(add.disabled).toBe(true));
	await fireEvent.keyDown(original, { key: 'Enter' });
	await fireEvent.keyDown(replacement, { key: 'Enter' });
	expect(apiClient.createWordReplacement).toHaveBeenCalledTimes(1);
	complete({} as WordReplacement);
	await waitFor(() => expect(toastStore.success).toHaveBeenCalledTimes(1));
});

it('preserves a failed draft and unlocks a successful retry', async () => {
	const { original, replacement, add, fail } = await startPending();
	fail();
	await waitFor(() => expect(toastStore.error).toHaveBeenCalledWith('Add failed', 4000));
	expect(original.value).toBe('First');
	expect(replacement.value).toBe('One');
	await waitFor(() => expect(add.disabled).toBe(false));
	vi.mocked(apiClient.createWordReplacement).mockResolvedValue({} as WordReplacement);
	await fireEvent.click(add);
	await waitFor(() => expect(toastStore.success).toHaveBeenCalledTimes(1));
	expect(apiClient.createWordReplacement).toHaveBeenCalledTimes(2);
	expect(original.value).toBe('');
	expect(replacement.value).toBe('');
});

it('preserves a newer draft when the earlier request fails', async () => {
	const { original, replacement, mode, add, fail } = await startPending();
	await fireEvent.input(original, { target: { value: 'Second' } });
	await fireEvent.input(replacement, { target: { value: 'Two' } });
	await fireEvent.change(mode, { target: { value: 'wildcard' } });
	fail('');
	await waitFor(() => expect(toastStore.error).toHaveBeenCalledTimes(1));
	expect(original.value).toBe('Second');
	expect(replacement.value).toBe('Two');
	expect(mode.value).toBe('wildcard');
	await waitFor(() => expect(add.disabled).toBe(false));
});

it.each([
	[' ', 'One'],
	['First', ' '],
])('does not submit empty trimmed fields (%s, %s)', async (first, second) => {
	const { original, add } = await mountDraft(first, second);
	expect(add.disabled).toBe(true);
	await fireEvent.keyDown(original, { key: 'Enter' });
	expect(apiClient.createWordReplacement).not.toHaveBeenCalled();
	expect(toastStore.error).toHaveBeenCalledTimes(1);
});

it('ignores other keys without locking the next valid submission', async () => {
	vi.mocked(apiClient.createWordReplacement).mockResolvedValue({} as WordReplacement);
	const { original, add } = await mountDraft();
	await fireEvent.keyDown(original, { key: 'a' });
	expect(apiClient.createWordReplacement).not.toHaveBeenCalled();
	await fireEvent.click(add);
	await waitFor(() => expect(toastStore.success).toHaveBeenCalledTimes(1));
});
