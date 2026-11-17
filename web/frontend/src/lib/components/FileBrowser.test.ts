import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { tick } from 'svelte';
import FileBrowser from './FileBrowser.svelte';
import { apiClient } from '$lib/api/client';
import type { BrowseResponse } from '$lib/api/types';

vi.mock('$lib/api/client', () => ({ apiClient: { browse: vi.fn() } }));

beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);

describe.each(['hydration', 'browse'] as const)(
	'FileBrowser selection pruning via %s',
	(source) => {
		it.each([
			['POSIX', '/library', '/library/'],
			['Windows', 'C:\\library', 'C:\\library\\'],
			['UNC', '\\\\server\\share', '\\\\server\\share\\'],
		])(
			'notifies exactly once for stale %s selections and not on an unchanged refresh',
			async (_name, directory, prefix) => {
				const kept = `${prefix}kept.mp4`;
				const other = '/other/kept.mp4';
				const nested = `${prefix}sub/deep.mp4`;
				const response: BrowseResponse = {
					current_path: directory,
					parent_path: '',
					items: [
						{
							name: 'kept.mp4',
							path: kept,
							is_dir: false,
							size: 0,
							mod_time: '2024-01-01T00:00:00Z',
						},
					],
				};
				vi.mocked(apiClient.browse).mockResolvedValue(response);
				const onFileSelect = vi.fn();
				const { getByTitle } = render(FileBrowser, {
					initialPath: directory,
					...(source === 'hydration' ? { initialData: response } : {}),
					selectedFiles: [`${prefix}stale.mp4`, kept, other, nested],
					onFileSelect,
				});
				await waitFor(() =>
					expect(onFileSelect).toHaveBeenCalledExactlyOnceWith([kept, other, nested]),
				);
				expect(apiClient.browse).toHaveBeenCalledTimes(source === 'hydration' ? 0 : 1);
				await fireEvent.click(getByTitle('Refresh'));
				await tick();
				expect(apiClient.browse).toHaveBeenCalledTimes(source === 'hydration' ? 1 : 2);
				expect(onFileSelect).toHaveBeenCalledTimes(1);
			},
		);

		it('does not notify when selections are unchanged', async () => {
			const response: BrowseResponse = { current_path: '/library', parent_path: '', items: [] };
			vi.mocked(apiClient.browse).mockResolvedValue(response);
			const onFileSelect = vi.fn();
			const { getByTitle } = render(FileBrowser, {
				initialPath: '/library',
				...(source === 'hydration' ? { initialData: response } : {}),
				selectedFiles: ['/other/kept.mp4', '/library/sub/deep.mp4'],
				onFileSelect,
			});
			await tick();
			await fireEvent.click(getByTitle('Refresh'));
			await tick();
			expect(onFileSelect).not.toHaveBeenCalled();
		});

		it('supports pruning without a callback', async () => {
			const response: BrowseResponse = { current_path: '/library', parent_path: '', items: [] };
			vi.mocked(apiClient.browse).mockResolvedValue(response);
			render(FileBrowser, {
				initialPath: '/library',
				...(source === 'hydration' ? { initialData: response } : {}),
				selectedFiles: ['/library/stale.mp4'],
			});
			await tick();
			expect(apiClient.browse).toHaveBeenCalledTimes(source === 'hydration' ? 0 : 1);
		});
	},
);

describe('FileBrowser obsolete requests', () => {
	function deferred<T>() {
		let resolve!: (value: T) => void;
		let reject!: (reason: Error) => void;
		const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
		return { promise, resolve, reject };
	}

	it.each(['success', 'error'])('ignores an older %s after a newer path has loaded', async (outcome) => {
		const old = deferred<BrowseResponse>();
		const newer = deferred<BrowseResponse>();
		vi.mocked(apiClient.browse).mockReturnValueOnce(old.promise).mockReturnValueOnce(newer.promise);
		const onPathChange = vi.fn();
		const view = render(FileBrowser, { initialPath: '/old', onPathChange });
		await waitFor(() => expect(apiClient.browse).toHaveBeenCalledTimes(1));
		await view.rerender({ initialPath: '/new', onPathChange });
		await waitFor(() => expect(apiClient.browse).toHaveBeenCalledTimes(2));
		newer.resolve({ current_path: '/new', parent_path: '/', items: [] });
		await waitFor(() => expect(onPathChange).toHaveBeenCalledExactlyOnceWith('/new'));
		if (outcome === 'success') old.resolve({ current_path: '/old', parent_path: '/', items: [] });
		else old.reject(new Error('Obsolete browse failure'));
		await old.promise.catch(() => {});
		await tick();
		expect(onPathChange).toHaveBeenCalledExactlyOnceWith('/new');
		expect(view.getByDisplayValue('/new')).toBeTruthy();
		expect(view.queryByText('Obsolete browse failure')).toBeNull();
	});

	it('keeps loading while a newer request is pending after an obsolete failure', async () => {
		const old = deferred<BrowseResponse>();
		const newer = deferred<BrowseResponse>();
		vi.mocked(apiClient.browse).mockReturnValueOnce(old.promise).mockReturnValueOnce(newer.promise);
		const onPathChange = vi.fn();
		const view = render(FileBrowser, { initialPath: '/old', onPathChange });
		await waitFor(() => expect(apiClient.browse).toHaveBeenCalledTimes(1));
		await view.rerender({ initialPath: '/new', onPathChange });
		await waitFor(() => expect(apiClient.browse).toHaveBeenCalledTimes(2));
		old.reject(new Error('Obsolete browse failure'));
		await old.promise.catch(() => {});
		await tick();
		expect(onPathChange).not.toHaveBeenCalled();
		expect(view.queryByText('Obsolete browse failure')).toBeNull();
		expect((view.getByTitle('Navigate to path') as HTMLButtonElement).disabled).toBe(true);
		newer.resolve({ current_path: '/new', parent_path: '/', items: [] });
		await waitFor(() => expect(onPathChange).toHaveBeenCalledExactlyOnceWith('/new'));
		expect((view.getByTitle('Navigate to path') as HTMLButtonElement).disabled).toBe(false);
	});

	it('does not publish a late response after the browser is unmounted', async () => {
		const response = deferred<BrowseResponse>();
		vi.mocked(apiClient.browse).mockReturnValue(response.promise);
		const onPathChange = vi.fn();
		const view = render(FileBrowser, { initialPath: '/old', onPathChange });
		await waitFor(() => expect(apiClient.browse).toHaveBeenCalledTimes(1));
		await view.unmount();
		response.resolve({ current_path: '/old', parent_path: '/', items: [] });
		await response.promise;
		await tick();
		expect(onPathChange).not.toHaveBeenCalled();
	});
});

describe('FileBrowser entry filters', () => {
	const items = [
		{ name: 'alpha', path: '/library/alpha', is_dir: true, size: 0, mod_time: '2024-01-01T00:00:00Z' },
		{ name: 'alpha.mp4', path: '/library/alpha.mp4', is_dir: false, size: 1, mod_time: '2024-01-01T00:00:00Z' },
		{ name: 'beta.txt', path: '/library/beta.txt', is_dir: false, size: 1, mod_time: '2024-01-01T00:00:00Z' },
	];
	const initialData = { current_path: '/library', parent_path: '/', items };

	it('combines type and text filters and can reset an empty result', async () => {
		const view = render(FileBrowser, { initialData, showEntryFilter: true });
		await fireEvent.change(view.getByRole('combobox'), { target: { value: 'files' } });
		expect(view.queryByText('alpha')).toBeNull();
		expect(view.getByText('alpha.mp4')).toBeTruthy();
		expect(view.getByText('beta.txt')).toBeTruthy();
		const search = view.container.querySelector('input[placeholder*="Filter"]')!;
		await fireEvent.input(search, { target: { value: 'beta' } });
		expect(view.queryByText('alpha.mp4')).toBeNull();
		await fireEvent.change(view.getByRole('combobox'), { target: { value: 'folders' } });
		expect(view.queryByText('beta.txt')).toBeNull();
		await fireEvent.click(view.getByText('Clear filter'));
		expect(view.getByText('alpha')).toBeTruthy();
		expect(view.getByText('alpha.mp4')).toBeTruthy();
	});

	it('keeps hidden folder selections explicit and lets users clear them', async () => {
		const onScan = vi.fn();
		const props = { initialData, showEntryFilter: true, selectedFolders: ['/library/alpha'], onScan };
		const view = render(FileBrowser, props);
		await fireEvent.change(view.getByRole('combobox'), { target: { value: 'files' } });
		expect(view.getByRole('status').textContent).toContain('remain selected for recursive scanning');
		await fireEvent.click(view.getByText('Clear folder selections'));
		expect(view.queryByRole('status')).toBeNull();
	});

	it('retains the type filter across refresh and navigation', async () => {
		vi.mocked(apiClient.browse).mockResolvedValue({ ...initialData, current_path: '/other' });
		const view = render(FileBrowser, { initialData, showEntryFilter: true });
		await fireEvent.change(view.getByRole('combobox'), { target: { value: 'files' } });
		await fireEvent.click(view.getByTitle('Refresh'));
		await tick();
		expect((view.getByRole('combobox') as HTMLSelectElement).value).toBe('files');
		expect(view.queryByText('alpha')).toBeNull();
	});

	it('does not add type controls to destination pickers', () => {
		const view = render(FileBrowser, { initialData, showEntryFilter: true, folderOnly: true });
		expect(view.queryByRole('combobox')).toBeNull();
		expect(view.getByText('alpha')).toBeTruthy();
	});
});


describe('FileBrowser filter pagination', () => {
 it('returns to page one when changing type and selects only visible videos', async () => {
  const items = Array.from({ length: 110 }, (_, i) => ({ name: `folder-${i}`, path: `/library/folder-${i}`, is_dir: true, size: 0, mod_time: '2024-01-01T00:00:00Z' }));
  items.push({ name: 'video.mp4', path: '/library/video.mp4', is_dir: false, size: 1, mod_time: '2024-01-01T00:00:00Z' });
  const onFileSelect = vi.fn();
  const view = render(FileBrowser, { initialData: { current_path: '/library', parent_path: '/', items }, showEntryFilter: true, onFileSelect });
  await fireEvent.click(view.getAllByRole('button', { name: '›' })[0]);
  await fireEvent.change(view.getByRole('combobox'), { target: { value: 'files' } });
  expect(view.getByText('video.mp4')).toBeTruthy();
  expect(view.queryByText('folder-0')).toBeNull();
  await fireEvent.click(view.getByText('Select All'));
  expect(onFileSelect).toHaveBeenLastCalledWith(['/library/video.mp4']);
 });
});
