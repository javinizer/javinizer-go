import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import OperationRow from './OperationRow.svelte';
import type { OperationItem } from '$lib/api/types';

vi.mock('$lib/paraglide/messages', () => ({
	history_from_label: () => 'From:',
	history_to_label: () => 'To:',
	history_in_dir: ({ dir }: { dir: string }) => `in ${dir}`,
	history_in_place_rename: () => '(in-place rename)',
	history_revert_file: () => 'Revert File',
	history_reverting: () => 'Reverting...',
	history_reverted: () => 'Reverted ✓',
	history_nothing_to_revert: () => 'Nothing to revert',
	status_success: () => 'Success',
	status_failed: () => 'Failed',
	status_reverted: () => 'Reverted',
	status_noop: () => 'No changes',
	status_organized: () => 'Organized',
	status_running: () => 'Running',
	status_cancelled: () => 'Cancelled',
	status_partial: () => 'Partial',
}));

function makeOp(revert_status: string, overrides: Partial<OperationItem> = {}): OperationItem {
	return {
		id: 1,
		movie_id: 'ABC-123',
		original_path: '/src/ABC-123.mp4',
		new_path: '/dest/ABC-123/ABC-123.mp4',
		operation_type: 'move',
		revert_status,
		in_place_renamed: false,
		created_at: '2026-01-01T00:00:00Z',
		...overrides,
	};
}

function findButton(container: HTMLElement, text: string): HTMLButtonElement | null {
	const buttons = Array.from(container.querySelectorAll('button'));
	return (buttons.find((b) => b.textContent?.includes(text)) as HTMLButtonElement | undefined) ?? null;
}

describe('OperationRow revert action gating (codex P2, PR #241 F2)', () => {
	it('offers a working revert action for an applied row', async () => {
		const onRevert = vi.fn();
		const { container } = render(OperationRow, {
			operation: makeOp('applied'),
			onRevert,
			revertible: true,
		});

		const btn = findButton(container, 'Revert File');
		expect(btn).toBeTruthy();
		expect(btn?.disabled).toBe(false);
		expect(container.textContent).toContain('Success');

		await fireEvent.click(btn!);
		expect(onRevert).toHaveBeenCalledWith('ABC-123');
	});

	it('classifies a noop row as terminal + non-revertible — no revert action, even when the job allows revert', () => {
		const onRevert = vi.fn();
		const { container } = render(OperationRow, {
			operation: makeOp('noop'),
			onRevert,
			revertible: true,
		});

		// The pre-F2 bug: noop rows rendered as success WITH a live revert
		// button whose click reported success while the server returned an
		// empty result.
		expect(findButton(container, 'Revert File')).toBeNull();

		const noopBtn = findButton(container, 'Nothing to revert');
		expect(noopBtn).toBeTruthy();
		expect(noopBtn?.disabled).toBe(true);
		expect(container.textContent).toContain('No changes');
	});

	it('keeps noop terminal when the row was one of several in the batch (noop stays noop after refresh)', () => {
		const onRevert = vi.fn();
		const { container } = render(OperationRow, {
			// Server leaves revoked noop rows at 'noop' forever (revertLog
			// finalize) — the row must not flip back to a success + action.
			operation: makeOp('noop', { new_path: '' }),
			onRevert,
			revertible: true,
		});
		expect(findButton(container, 'Revert File')).toBeNull();
		expect(findButton(container, 'Nothing to revert')?.disabled).toBe(true);
		expect(onRevert).not.toHaveBeenCalled();
	});

	it('renders reverted rows disabled as before', () => {
		const onRevert = vi.fn();
		const { container } = render(OperationRow, {
			operation: makeOp('reverted'),
			onRevert,
			revertible: true,
		});

		expect(findButton(container, 'Revert File')).toBeNull();
		const revertedBtn = findButton(container, 'Reverted ✓');
		expect(revertedBtn).toBeTruthy();
		expect(revertedBtn?.disabled).toBe(true);
	});

	it('shows a spinner while a revert is in flight', () => {
		const { container } = render(OperationRow, {
			operation: makeOp('applied'),
			onRevert: vi.fn(),
			reverting: true,
			revertible: true,
		});

		const btn = findButton(container, 'Reverting...');
		expect(btn).toBeTruthy();
		expect(btn?.disabled).toBe(true);
		expect(findButton(container, 'Revert File')).toBeNull();
	});
});
