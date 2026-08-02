import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import ReviewHeader from './ReviewHeader.svelte';

// Minimal valid prop set; only the fields under test vary.
function renderHeader(overrides: Record<string, unknown> = {}) {
	return render(ReviewHeader, {
		isUpdateMode: false,
		canOrganize: true,
		organizing: false,
		movieResultsLength: 2,
		destinationPath: '',
		operationMode: 'in-place',
		onClose: vi.fn(),
		onUpdateAll: vi.fn(),
		onOrganizeAll: vi.fn(),
		onSaveAll: vi.fn(),
		hasEdits: false,
		editCount: 0,
		savingEdits: false,
		...overrides,
	});
}

describe('ReviewHeader apply gating', () => {
	it('enables Organize for a valid in-place plan', () => {
		const { getByRole } = renderHeader();
		expect((getByRole('button', { name: /Organize 2 Files/ }) as HTMLButtonElement).disabled).toBe(
			false,
		);
	});

	it('blocks Organize when the effective plan is invalid (both NFO and media skipped)', () => {
		// Regression: the sticky action bar and update-mode button honored
		// applyInvalid, but this header button did not — it submitted a plan
		// the backend rejects.
		const { getByRole } = renderHeader({ operationMode: 'metadata-artwork', applyInvalid: true });
		expect((getByRole('button', { name: /Organize 2 Files/ }) as HTMLButtonElement).disabled).toBe(
			true,
		);
	});

it('clears the preset when a custom merge strategy is chosen', async () => {
		// Regression: with a preset active, editing a strategy by hand sent
		// contradictory preset + strategy to the backend (rejected). The pure
		// helper (withCustomReviewMergeStrategy) clears it — wire-level pin.
		const { container, getByRole } = renderHeader({
			isUpdateMode: true, // merge strategy controls are update-mode only
			applyPreset: 'aggressive',
		});
		await fireEvent.click(getByRole('button', { name: 'Options' }));

		let scalarSelect: HTMLSelectElement | undefined;
		let presetSelect: HTMLSelectElement | undefined;
		await waitFor(() => {
			const selects = Array.from(container.querySelectorAll('select'));
			scalarSelect = selects.find((s) =>
				Array.from(s.options).some((o) => o.value === 'prefer-nfo'),
			);
			presetSelect = selects.find((s) =>
				Array.from(s.options).some((o) => o.value === 'gap-fill'),
			);
			expect(scalarSelect).toBeTruthy();
			expect(presetSelect).toBeTruthy();
		});
		expect(presetSelect!.value).toBe('aggressive');

		await fireEvent.change(scalarSelect!, { target: { value: 'prefer-nfo' } });

		await waitFor(() => {
			expect(presetSelect!.value).toBe('');
		});
		expect(scalarSelect!.value).toBe('prefer-nfo');
	});

	it('clears replace-existing when downloads are skipped', async () => {
		// Regression: Skip-downloads left Replace-existing checked-but-disabled,
		// submitting skip_download + overwrite_existing_media (backend 400).
		const { container, getByRole } = renderHeader({ isUpdateMode: true, overwriteExistingMedia: true });
		await fireEvent.click(getByRole('button', { name: 'Options' }));

		let overwrite: HTMLInputElement | undefined;
		let skip: HTMLInputElement | undefined;
		await waitFor(() => {
			const boxes = Array.from(container.querySelectorAll<HTMLInputElement>('input[type="checkbox"]'));
			overwrite = boxes.find((i) => i.closest('label')?.textContent?.includes('Replace Existing'));
			skip = boxes.find((i) => i.closest('label')?.textContent?.includes('Skip Download'));
			expect(overwrite).toBeTruthy();
			expect(skip).toBeTruthy();
		});
		expect(overwrite!.checked).toBe(true);
		expect(skip!.checked).toBe(false);

		await fireEvent.click(skip!);
		expect(skip!.checked).toBe(true);

		await waitFor(() => {
			expect(overwrite!.checked).toBe(false);
		});
		expect(overwrite!.disabled).toBe(true);
	});

	it('blocks the update-mode button when the plan is invalid', () => {
		const { getByRole } = renderHeader({ isUpdateMode: true, applyInvalid: true });
		expect((getByRole('button', { name: /Update 2 Files/ }) as HTMLButtonElement).disabled).toBe(
			true,
		);
	});
});
