import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render } from '@testing-library/svelte';
import VideoCollisionPanel from './VideoCollisionPanel.svelte';
import type { VideoCollision } from '$lib/api/types';

const item: VideoCollision = {
	source: '/incoming/IPX-277.mp4',
	destination: '/library/IPX-277.mp4',
	source_size: 1048576,
	destination_size: 2097152,
	kind: 'file',
	review_token: 'server-receipt',
};

describe('video collision review', () => {
	it('shows paths, sizes and explicit choices with the server receipt', async () => {
		const onDecide = vi.fn();
		const view = render(VideoCollisionPanel, {
			report: { items: [item], new: 0, replaced: 0, skipped: 0, unresolved: 1, same_file: 0 },
			pending: false,
			error: null,
			blocked: true,
			decisions: {},
			onReview: vi.fn(),
			onDecide,
		});
		expect(view.getByText(/incoming\/IPX-277/)).toBeTruthy();
		expect(view.getByText(/2 MiB/)).toBeTruthy();
		await fireEvent.change(view.getByRole('combobox'), { target: { value: 'replace' } });
		expect(onDecide).toHaveBeenCalledWith(item, 'replace');
	});
	it('keeps a skipped new destination actionable after refresh and reload', async () => {
		const skipped = { ...item, kind: 'new', review_token: undefined };
		const competitor = { ...skipped, source: '/incoming/other.mp4' };
		const onDecide = vi.fn();
		const props = {
			report: {
				items: [skipped, competitor],
				new: 1,
				replaced: 0,
				skipped: 1,
				unresolved: 0,
				same_file: 0,
			},
			pending: false,
			error: null,
			blocked: false,
			decisions: {
				[skipped.source]: {
					source: skipped.source,
					destination: skipped.destination,
					policy: 'skip' as const,
					review_token: '',
				},
			},
			onReview: vi.fn(),
			onDecide,
		};
		const first = render(VideoCollisionPanel, props);
		expect((first.getByRole('combobox') as HTMLSelectElement).value).toBe('skip');
		expect(first.getByText(/Skip 1/)).toBeTruthy();
		await first.rerender({ ...props, report: structuredClone(props.report) });
		expect(first.getAllByRole('combobox')).toHaveLength(1);
		first.unmount();
		const reloaded = render(VideoCollisionPanel, {
			...props,
			decisions: JSON.parse(JSON.stringify(props.decisions)),
		});
		await fireEvent.change(reloaded.getByRole('combobox'), { target: { value: 'fail' } });
		expect(onDecide).toHaveBeenLastCalledWith(skipped, 'fail');
		await reloaded.rerender({
			...props,
			decisions: {},
			blocked: true,
			report: {
				...props.report,
				items: [skipped, { ...competitor, kind: 'duplicate' }],
				skipped: 0,
				unresolved: 1,
			},
		});
		await fireEvent.change(reloaded.getByRole('combobox'), { target: { value: 'skip' } });
		expect(onDecide).toHaveBeenLastCalledWith({ ...competitor, kind: 'duplicate' }, 'skip');
	});

	it.each(['symlink', 'directory', 'multipart', 'duplicate', 'unknown'])(
		'never offers replacement for %s',
		(kind) => {
			const view = render(VideoCollisionPanel, {
				report: {
					items: [{ ...item, kind, review_token: undefined }],
					new: 0,
					replaced: 0,
					skipped: 0,
					unresolved: 1,
					same_file: 0,
				},
				pending: false,
				error: null,
				blocked: true,
				decisions: {},
				onReview: vi.fn(),
				onDecide: vi.fn(),
			});
			expect(view.queryByRole('option', { name: 'Replace reviewed video' })).toBeNull();
		},
	);
});
