import { describe, it, expect, vi, afterEach } from 'vitest';
import { createPosterCropController, type PosterCropDragState } from './poster-crop-controller';
import { BaseClient } from '$lib/api/clients/common';
import type { FileResult, Movie } from '$lib/api/types';
import type { PosterCropBox, PosterCropMetrics, PosterCropState } from '../review-utils';

interface CallLog {
	calls: string[];
	applyPosterFromUrlAsync: ReturnType<typeof vi.fn>;
	mutatePosterCropAsync: ReturnType<typeof vi.fn>;
	setCropApplying: ReturnType<typeof vi.fn>;
}

function makeController(opts: {
	editedPosterUrl?: string;
	serverPosterUrl?: string;
	cropBox?: PosterCropBox | null;
	maxPosterHeight?: number | null;
	persistRejects?: boolean;
}): { controller: ReturnType<typeof createPosterCropController>; log: CallLog } {
	const movie: Movie = {
		id: 'STARS-136',
		title: 'Test Movie',
		poster_url: opts.editedPosterUrl ?? 'https://dmm/jacket-full.jpg'
	};
	const result: FileResult = {
		result_id: 'res-1',
		file_path: '/tmp/test-video.mp4',
		movie_id: 'STARS-136',
		status: 'completed',
		started_at: '',
		is_multi_part: false,
		part_number: 0,
		part_suffix: '',
		movie: {
			id: 'STARS-136',
			title: 'Test Movie',
			poster_url: opts.serverPosterUrl ?? 'https://dmm/digital-poster.jpg'
		}
	};

	const calls: string[] = [];
	const applyPosterFromUrlAsync = vi.fn(async (_resultId: string, _url: string) => {
		calls.push('persist');
		if (opts.persistRejects) throw new Error('download failed');
	});
	const mutatePosterCropAsync = vi.fn(async (_jobId: string, _resultId: string, _crop: PosterCropBox, _max?: number) => {
		calls.push('crop');
	});
	const setCropApplying = vi.fn((applying: boolean) => {
		calls.push(`applying:${applying}`);
	});

	const noop = () => {};
	const log: CallLog = { calls, applyPosterFromUrlAsync, mutatePosterCropAsync, setCropApplying };

	const controller = createPosterCropController({
		getBrowser: () => true,
		getJobId: () => 'job-1',
		getCurrentMovie: () => movie,
		getCurrentResult: () => result,
		getShowPosterCropModal: () => true,
		setShowPosterCropModal: noop,
		setPosterCropLoadError: noop,
		getCropSourceURL: () => '',
		setCropSourceURL: noop,
		getCropImageElement: () => null,
		setCropImageElement: noop,
		getCropMetrics: () => null,
		setCropMetrics: noop,
		getCropBox: () => opts.cropBox === undefined ? { x: 0, y: 0, width: 100, height: 200 } : opts.cropBox,
		setCropBox: noop,
		getMaxPosterHeight: () => opts.maxPosterHeight === undefined ? null : opts.maxPosterHeight,
		setMaxPosterHeight: noop,
		getCropDragState: (): PosterCropDragState | null => null,
		setCropDragState: noop,
		getPosterCropStates: () => new Map<string, PosterCropState>(),
		applyPosterFromUrlAsync,
		mutatePosterCropAsync,
		setCropApplying
	});

	return { controller, log };
}

describe('applyPosterCrop — persist edited URL before cropping (issue #37)', () => {
	it('persists the edited poster URL before applying the crop when URL differs from server', async () => {
		const { controller, log } = makeController({
			editedPosterUrl: 'https://dmm/jacket-full.jpg',
			serverPosterUrl: 'https://dmm/digital-poster.jpg'
		});

		await controller.applyPosterCrop();

		// Persist was called with the edited URL, before the crop.
		expect(log.applyPosterFromUrlAsync).toHaveBeenCalledWith('res-1', 'https://dmm/jacket-full.jpg');
		expect(log.mutatePosterCropAsync).toHaveBeenCalledTimes(1);
		expect(log.calls).toEqual(['applying:true', 'persist', 'crop', 'applying:false']);
	});

	it('does not persist when the poster URL matches the server (no client-side edit)', async () => {
		const sameUrl = 'https://dmm/digital-poster.jpg';
		const { controller, log } = makeController({
			editedPosterUrl: sameUrl,
			serverPosterUrl: sameUrl
		});

		await controller.applyPosterCrop();

		expect(log.applyPosterFromUrlAsync).not.toHaveBeenCalled();
		expect(log.mutatePosterCropAsync).toHaveBeenCalledTimes(1);
		expect(log.calls).toEqual(['applying:true', 'crop', 'applying:false']);
	});

	it('aborts the crop if persisting the URL fails, but still clears cropApplying', async () => {
		const { controller, log } = makeController({
			editedPosterUrl: 'https://dmm/jacket-full.jpg',
			serverPosterUrl: 'https://dmm/digital-poster.jpg',
			persistRejects: true
		});

		await controller.applyPosterCrop();

		expect(log.applyPosterFromUrlAsync).toHaveBeenCalledTimes(1);
		expect(log.mutatePosterCropAsync).not.toHaveBeenCalled();
		// finally block still runs
		expect(log.calls).toContain('applying:false');
		expect(log.calls).not.toContain('crop');
	});

	it('passes maxPosterHeight through to the crop mutation', async () => {
		const sameUrl = 'https://dmm/poster.jpg';
		const { controller, log } = makeController({
			editedPosterUrl: sameUrl,
			serverPosterUrl: sameUrl,
			maxPosterHeight: 1200
		});

		await controller.applyPosterCrop();

		expect(log.mutatePosterCropAsync).toHaveBeenCalledWith('job-1', 'res-1', expect.any(Object), 1200);
	});

	it('does nothing when there is no crop box', async () => {
		const { controller, log } = makeController({
			editedPosterUrl: 'https://dmm/jacket-full.jpg',
			serverPosterUrl: 'https://dmm/digital-poster.jpg',
			cropBox: null
		});

		await controller.applyPosterCrop();

		expect(log.applyPosterFromUrlAsync).not.toHaveBeenCalled();
		expect(log.mutatePosterCropAsync).not.toHaveBeenCalled();
		expect(log.setCropApplying).not.toHaveBeenCalled();
	});
});

describe('openPosterCropModal — crop source URL formation (poster rendering regressions)', () => {
	// Regression: openPosterCropModal built the source URL with a duplicated
	// '?' separator (...?session=abc?v=123), corrupting the session value →
	// 401 → onerror → "Poster source is not available for manual cropping".
	// The fix uses the correct separator ('&' when '?' is already present).
	// These tests pin the URL shape by capturing setCropSourceURL calls.

	afterEach(() => {
		BaseClient.setSessionID(null);
	});

	function makeCropController() {
		BaseClient.setSessionID('sid-abc');
		const setCropSourceURL = vi.fn();
		const movie: Movie = {
			id: 'GOOD-001',
			title: 'Test Movie',
			poster_url: 'https://dmm/poster-GOOD-001.jpg',
		};
		const result: FileResult = {
			result_id: 'res-1',
			file_path: '/tmp/GOOD-001.mp4',
			movie_id: 'GOOD-001',
			status: 'completed',
			started_at: '',
			is_multi_part: false,
			part_number: 0,
			part_suffix: '',
			movie: { id: 'GOOD-001', title: 'Test Movie', poster_url: 'https://dmm/poster-GOOD-001.jpg' },
		};
		const controller = createPosterCropController({
			getBrowser: () => true,
			getJobId: () => 'job-1',
			getCurrentMovie: () => movie,
			getCurrentResult: () => result,
			getShowPosterCropModal: () => false,
			setShowPosterCropModal: () => {},
			setPosterCropLoadError: () => {},
			getCropSourceURL: () => '',
			setCropSourceURL,
			getCropImageElement: () => null,
			setCropImageElement: () => {},
			getCropMetrics: () => null,
			setCropMetrics: () => {},
			getCropBox: () => null,
			setCropBox: () => {},
			getMaxPosterHeight: () => null,
			setMaxPosterHeight: () => {},
			getCropDragState: () => null,
			setCropDragState: () => {},
			getPosterCropStates: () => new Map<string, PosterCropState>(),
			applyPosterFromUrlAsync: vi.fn(async () => {}),
			mutatePosterCropAsync: vi.fn(async () => {}),
			setCropApplying: () => {},
			now: () => 12345,
		});
		return { controller, setCropSourceURL };
	}

	it('builds the crop source URL with at most one ? separator and includes the session param (no duplicated ? corrupting the query string)', () => {
		const { controller, setCropSourceURL } = makeCropController();
		controller.openPosterCropModal();

		expect(setCropSourceURL).toHaveBeenCalledTimes(1);
		const url = setCropSourceURL.mock.calls[0][0] as string;
		expect(url, 'crop source URL must be populated').toBeTruthy();
		expect(url, 'crop source URL must include the session param').toContain('session=sid-abc');
		expect((url.match(/\?/g) ?? []).length, `crop source URL must have at most one "?", got: ${url}`).toBeLessThanOrEqual(1);
	});

	it('uses the correct separator when appending the cache-busting v= param to a session-tagged URL', () => {
		const { controller, setCropSourceURL } = makeCropController();
		controller.openPosterCropModal();

		const url = setCropSourceURL.mock.calls[0][0] as string;
		expect(url, 'crop source URL must include the session param').toContain('session=sid-abc');
		expect(url, 'crop source URL must include the cache-busting v= param').toContain('v=12345');
		// The temp-poster URL carries ?session=... ; the v= cache-buster must
		// be appended with '&' (not '?'), producing ?session=...&v=12345.
		// A regression producing ?session=...?v=12345 (two '?') would fail
		// both this assertion and the at-most-one-? assertion above.
		expect(url, 'session + v params must be joined with &, not a duplicated ?').toMatch(/[?&]v=12345/);
	});
});

describe('openPosterCropModal — canonical movie ID wins over stale result FK (issue: 300MIUM-1360)', () => {
	// Regression: openPosterCropModal built the crop source URL from
	// result.movie_id (a stale FK that can diverge from movie.id after a
	// rescrape/ID canonicalization). The server names poster files by the
	// canonical movie.id (internal/poster/manager.go: "%s-full.jpg", posterID
	// where posterID = movie.id), so using result.movie_id requested a
	// non-existent file -> 404 -> "Poster source is not available".
	// Reported on review job 6c486399-37c5-44f5-b351-1755e22ef938 where
	// result.movie_id = "MIUM-1360" but movie.id = "300MIUM-1360".

	afterEach(() => {
		BaseClient.setSessionID(null);
	});

	function makeDivergentIdController() {
		BaseClient.setSessionID('sid-abc');
		const setCropSourceURL = vi.fn();
		const movie: Movie = {
			id: '300MIUM-1360',
			title: 'Test Movie',
			poster_url: 'https://dmm/poster.jpg',
		};
		const result: FileResult = {
			result_id: 'res-1',
			file_path: '/tmp/300MIUM-1360.mp4',
			movie_id: 'MIUM-1360',
			status: 'completed',
			started_at: '',
			is_multi_part: false,
			part_number: 0,
			part_suffix: '',
			movie: { id: '300MIUM-1360', title: 'Test Movie', poster_url: 'https://dmm/poster.jpg' },
		};
		const controller = createPosterCropController({
			getBrowser: () => true,
			getJobId: () => 'job-1',
			getCurrentMovie: () => movie,
			getCurrentResult: () => result,
			getShowPosterCropModal: () => false,
			setShowPosterCropModal: () => {},
			setPosterCropLoadError: () => {},
			getCropSourceURL: () => '',
			setCropSourceURL,
			getCropImageElement: () => null,
			setCropImageElement: () => {},
			getCropMetrics: () => null,
			setCropMetrics: () => {},
			getCropBox: () => null,
			setCropBox: () => {},
			getMaxPosterHeight: () => null,
			setMaxPosterHeight: () => {},
			getCropDragState: () => null,
			setCropDragState: () => {},
			getPosterCropStates: () => new Map<string, PosterCropState>(),
			applyPosterFromUrlAsync: vi.fn(async () => {}),
			mutatePosterCropAsync: vi.fn(async () => {}),
			setCropApplying: () => {},
			now: () => 12345,
		});
		return { controller, setCropSourceURL };
	}

	it('builds the crop source URL from the canonical movie.id, not the stale result.movie_id FK', () => {
		const { controller, setCropSourceURL } = makeDivergentIdController();
		controller.openPosterCropModal();

		expect(setCropSourceURL).toHaveBeenCalledTimes(1);
		const url = setCropSourceURL.mock.calls[0][0] as string;
		expect(url, 'crop source URL must use the canonical movie.id (300MIUM-1360-full.jpg)').toContain(
			'/api/v1/temp/posters/job-1/300MIUM-1360-full.jpg',
		);
		expect(url, 'crop source URL must NOT use the stale result.movie_id filename').not.toContain(
			'/MIUM-1360-full.jpg',
		);
		expect(url, 'crop source URL must NOT use the stale result.movie_id filename').not.toContain(
			'/MIUM-1360.jpg',
		);
	});
});
