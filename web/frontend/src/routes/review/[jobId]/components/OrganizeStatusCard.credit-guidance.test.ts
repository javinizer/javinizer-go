import { cleanup, fireEvent, render } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ComponentProps } from 'svelte';
import OrganizeStatusCard from './OrganizeStatusCard.svelte';

afterEach(cleanup);

function mount(
	kind: 'conflict' | 'unavailable' | null,
	movieIndex: number | null = 1,
	historicalJob = false,
	overrides: Partial<ComponentProps<typeof OrganizeStatusCard>> = {},
) {
	const path = '/synthetic/failed-part2.mp4';
	const onReviewCredits = vi.fn();
	const onRetryFailed = vi.fn();
	const view = render(OrganizeStatusCard, {
		organizeStatus: 'failed',
		organizeProgress: 100,
		fileStatuses: new Map([[path, { status: 'failed', error: 'Original backend error detail' }]]),
		expectedOrganizeFilePaths: [path],
		isUpdateMode: false,
		onRetryFailed,
		onContinue: vi.fn(),
		retryAvailable: false,
		historicalJob,
		creditGuidance: new Map(kind ? [[path, { kind, movieIndex }]] : []),
		onReviewCredits,
		...overrides,
	});
	return { ...view, path, onReviewCredits, onRetryFailed };
}

describe('OrganizeStatusCard credit failure guidance', () => {
	it.each([
		['conflict', 'Review actress-credit conflicts'],
		['unavailable', 'Check actress-credit status'],
	] as const)(
		'offers the correct %s recovery action while preserving the raw error',
		async (kind, label) => {
			const { getByText, getByRole, path, onReviewCredits, onRetryFailed } = mount(kind);
			expect(getByText('Original backend error detail')).toBeTruthy();
			const retry = getByRole('button', { name: /Retry Failed/i }) as HTMLButtonElement;
			expect(retry.disabled).toBe(true);
			await fireEvent.click(getByRole('button', { name: label }));
			expect(onReviewCredits).toHaveBeenCalledExactlyOnceWith(path);
			expect(onRetryFailed).not.toHaveBeenCalled();
			expect(retry.disabled).toBe(true);
		},
	);

	it('does not invent credit guidance for unrelated failures', () => {
		const { queryByRole, getByText } = mount(null, null, false, { creditGuidance: undefined });
		expect(getByText('Original backend error detail')).toBeTruthy();
		expect(queryByRole('button', { name: /actress-credit/i })).toBeNull();
	});

	it('does not offer navigation when the failed file cannot map to a reviewable movie', () => {
		const { queryByRole, getByText, onReviewCredits } = mount('conflict', null);
		expect(getByText('Original backend error detail')).toBeTruthy();
		expect(queryByRole('button', { name: 'Review actress-credit conflicts' })).toBeNull();
		expect(onReviewCredits).not.toHaveBeenCalled();
	});
});

it('explains that resolving conflicts does not reopen a historical job', () => {
	const { getByText, getByRole } = mount('conflict', 1, true);
	expect(getByText(/This saved job cannot be retried here/)).toBeTruthy();
	expect(getByText(/Resolving conflicts does not reopen this job/)).toBeTruthy();
	expect((getByRole('button', { name: /Retry Failed/i }) as HTMLButtonElement).disabled).toBe(true);
});

it.each([false, true])(
	'preserves the live retry gate while showing guidance (disabled: %s)',
	async (retryDisabled) => {
		const { getByRole, getByText, onRetryFailed } = mount('conflict', 1, false, {
			retryAvailable: true,
			retryDisabled,
		});
		expect(getByText(/Resolve any current conflicts, wait for the review to refresh/)).toBeTruthy();
		const retry = getByRole('button', { name: /Retry Failed/i }) as HTMLButtonElement;
		expect(retry.disabled).toBe(retryDisabled);
		await fireEvent.click(getByRole('button', { name: 'Review actress-credit conflicts' }));
		expect(onRetryFailed).not.toHaveBeenCalled();
		if (!retryDisabled) {
			await fireEvent.click(retry);
			expect(onRetryFailed).toHaveBeenCalledOnce();
		}
	},
);

it('does not present recovery actions while an attempt is in progress', () => {
	const { queryByRole, getByText } = mount('conflict', 1, false, {
		organizeStatus: 'organizing',
		organizeProgress: 42,
	});
	expect(getByText('42%')).toBeTruthy();
	expect(getByText('Original backend error detail')).toBeTruthy();
	expect(queryByRole('button', { name: /actress-credit/i })).toBeNull();
	expect(queryByRole('button', { name: /Retry Failed/i })).toBeNull();
});

it('keeps guidance explanatory when a navigation callback is unavailable', () => {
	const { queryByRole, getByText } = mount('unavailable', 1, false, { onReviewCredits: undefined });
	expect(getByText('Original backend error detail')).toBeTruthy();
	expect(queryByRole('button', { name: /actress-credit/i })).toBeNull();
});

it('does not show recovery actions when there are no failed files', () => {
	const { queryByRole, container } = mount(null, null, false, { fileStatuses: new Map() });
	expect(queryByRole('button')).toBeNull();
	expect(container.textContent?.trim()).toBe('');
});

it('disables credit navigation while a guarded write is pending', () => {
	const { getByRole, onReviewCredits } = mount('conflict', 1, false, {
		reviewCreditsDisabled: true,
	});
	const action = getByRole('button', {
		name: 'Review actress-credit conflicts',
	}) as HTMLButtonElement;
	expect(action.disabled).toBe(true);
	expect(onReviewCredits).not.toHaveBeenCalled();
});
