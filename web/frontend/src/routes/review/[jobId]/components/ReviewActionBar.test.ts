import { describe, expect, it, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import ReviewActionBar from './ReviewActionBar.svelte';

function renderBar(operationMode: string, destinationPath = '', applyInvalid = false) {
	return render(ReviewActionBar, {
		isUpdateMode: false,
		organizing: false,
		destinationPath,
		operationMode,
		applyInvalid,
		movieResultsLength: 2,
		onCancel: vi.fn(),
		onOrganizeAll: vi.fn()
	});
}

describe('ReviewActionBar', () => {
	it.each(['in-place', 'in-place-norenamefolder'])('allows %s without a destination', (mode) => {
		const { getByRole } = renderBar(mode);
		expect((getByRole('button', { name: /Organize 2 Files/ }) as HTMLButtonElement).disabled).toBe(false);
	});

	it('requires a destination only for organize', () => {
		const { getByRole } = renderBar('organize');
		expect((getByRole('button', { name: /Organize 2 Files/ }) as HTMLButtonElement).disabled).toBe(true);
	});

	it('blocks an invalid effective plan', () => {
		const { getByRole } = renderBar('in-place', '', true);
		expect((getByRole('button', { name: /Organize 2 Files/ }) as HTMLButtonElement).disabled).toBe(true);
	});
});