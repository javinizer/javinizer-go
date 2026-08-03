import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('$lib/utils/desktop', () => ({ isDesktopApp: vi.fn() }));

import { isDesktopApp } from '$lib/utils/desktop';
import { saveJsonFile } from './download';

const mockedIsDesktop = vi.mocked(isDesktopApp);

describe('saveJsonFile', () => {
	const originalFetch = globalThis.fetch;

	beforeEach(() => {
		mockedIsDesktop.mockReset();
		vi.stubGlobal('URL', {
			...URL,
			createObjectURL: vi.fn(() => 'blob:mock'),
			revokeObjectURL: vi.fn(),
		});
	});

	afterEach(() => {
		globalThis.fetch = originalFetch;
		vi.unstubAllGlobals();
		vi.restoreAllMocks();
	});

	it('browser: triggers an anchor download and reports saved', async () => {
		mockedIsDesktop.mockReturnValue(false);
		const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});

		const result = await saveJsonFile('word-replacements.json', [
			{ original: 'a', replacement: 'b' },
		]);

		expect(result).toEqual({ saved: true });
		expect(click).toHaveBeenCalledTimes(1);
		expect(URL.createObjectURL).toHaveBeenCalledTimes(1);
		expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:mock');
	});

	it('browser: sets the download filename on the anchor', async () => {
		mockedIsDesktop.mockReturnValue(false);
		vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
			this: HTMLAnchorElement,
		) {
			expect(this.download).toBe('genre-replacements.json');
			expect(this.href).toBe('blob:mock');
		});

		await saveJsonFile('genre-replacements.json', []);
	});

	it('desktop: streams the body to /desktop/save-file and returns the chosen path', async () => {
		mockedIsDesktop.mockReturnValue(true);
		globalThis.fetch = vi.fn(() =>
			Promise.resolve({
				ok: true,
				json: () =>
					Promise.resolve({ saved: true, path: '/Users/test/Downloads/word-replacements.json' }),
			}),
		) as unknown as typeof globalThis.fetch;

		const result = await saveJsonFile('word-replacements.json', [
			{ original: 'a', replacement: 'b' },
		]);

		expect(result).toEqual({ saved: true, path: '/Users/test/Downloads/word-replacements.json' });
		expect(globalThis.fetch).toHaveBeenCalledWith(
			'/desktop/save-file?filename=word-replacements.json',
			expect.objectContaining({
				method: 'POST',
				headers: { 'Content-Type': 'application/octet-stream' },
			}),
		);
		const sentBody = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0][1].body;
		expect(sentBody).toBe(JSON.stringify([{ original: 'a', replacement: 'b' }], null, 2));
	});

	it('desktop: reports a cancelled native dialog without throwing', async () => {
		mockedIsDesktop.mockReturnValue(true);
		globalThis.fetch = vi.fn(() =>
			Promise.resolve({
				ok: true,
				json: () => Promise.resolve({ saved: false }),
			}),
		) as unknown as typeof globalThis.fetch;

		const result = await saveJsonFile('actresses.json', []);

		expect(result.saved).toBe(false);
		expect(result.path).toBeUndefined();
	});

	it('desktop: surfaces the backend error message', async () => {
		mockedIsDesktop.mockReturnValue(true);
		globalThis.fetch = vi.fn(() =>
			Promise.resolve({
				ok: false,
				status: 500,
				json: () => Promise.resolve({ error: 'desktop: save dialog failed: disk full' }),
			}),
		) as unknown as typeof globalThis.fetch;

		await expect(saveJsonFile('word-replacements.json', [])).rejects.toThrow('disk full');
	});

	it('desktop: falls back to the HTTP status when the error body is not JSON', async () => {
		mockedIsDesktop.mockReturnValue(true);
		globalThis.fetch = vi.fn(() =>
			Promise.resolve({
				ok: false,
				status: 404,
				json: () => Promise.reject(new Error('unexpected content-type')),
			}),
		) as unknown as typeof globalThis.fetch;

		await expect(saveJsonFile('word-replacements.json', [])).rejects.toThrow('HTTP 404');
	});
});
