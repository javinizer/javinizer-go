import { describe, it, expect, vi, afterEach } from 'vitest';
import { createPosterCropController, type PosterCropDragState } from './poster-crop-controller';
import { BaseClient } from '$lib/api/clients/common';
import type { FileResult, Movie, PosterCropBounds } from '$lib/api/types';
import {
	getDefaultPosterCropBox,
	restoreCropBox,
	type PosterCropBox,
	type PosterCropMetrics,
	type PosterCropState,
} from '../review-utils';

interface CallLog {
	calls: string[];
	applyPosterFromUrlAsync: ReturnType<typeof vi.fn>;
	mutatePosterCropAsync: ReturnType<typeof vi.fn>;
	setCropApplying: ReturnType<typeof vi.fn>;
}

function makeController(opts: {
	editedPosterUrl?: string;
	serverPosterUrl?: string;
	editedCoverUrl?: string;
	serverCoverUrl?: string;
	cropBox?: PosterCropBox | null;
	maxPosterHeight?: number | null;
	persistRejects?: boolean;
}): { controller: ReturnType<typeof createPosterCropController>; log: CallLog } {
	const movie: Movie = {
		id: 'STARS-136',
		title: 'Test Movie',
		poster_url: opts.editedPosterUrl ?? 'https://dmm/jacket-full.jpg',
		...(opts.editedCoverUrl !== undefined ? { cover_url: opts.editedCoverUrl } : {})
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
			poster_url: opts.serverPosterUrl ?? 'https://dmm/digital-poster.jpg',
			...(opts.serverCoverUrl !== undefined ? { cover_url: opts.serverCoverUrl } : {})
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

	it('pre-syncs via poster-from-URL when the server poster_url is EMPTY (cover-backed movie with unsaved poster_url edit)', async () => {
		// Cover-backed movie: server-side poster_url is empty, the user pasted
		// a new URL client-side. openPosterCropModal already shows the edited
		// URL via the image proxy, so applyPosterCrop must pre-sync that URL to
		// {movieId}-full.jpg BEFORE cropping — otherwise the backend crops the
		// COVER with bounds measured on the edited image, and discarding the
		// pending edit would leave wrong bounds attached to the cover.
		const { controller, log } = makeController({
			editedPosterUrl: 'https://dmm/jacket-full.jpg',
			serverPosterUrl: ''
		});

		await controller.applyPosterCrop();

		expect(log.applyPosterFromUrlAsync).toHaveBeenCalledWith('res-1', 'https://dmm/jacket-full.jpg');
		expect(log.mutatePosterCropAsync).toHaveBeenCalledTimes(1);
		expect(log.calls).toEqual(['applying:true', 'persist', 'crop', 'applying:false']);
	});

	it('still skips the pre-sync when neither server nor edited poster_url is set (no edit at all)', async () => {
		const { controller, log } = makeController({
			editedPosterUrl: '',
			serverPosterUrl: ''
		});

		await controller.applyPosterCrop();

		expect(log.applyPosterFromUrlAsync).not.toHaveBeenCalled();
		expect(log.mutatePosterCropAsync).toHaveBeenCalledTimes(1);
	});

	it('pre-syncs an unsaved cover_url edit on a cover-backed movie (effective source = poster_url || cover_url)', async () => {
		// Cover-backed movie: poster_url is EMPTY on both sides, the effective
		// poster source is the cover (backend: effectivePosterSourceOf). The
		// user pasted a new COVER URL client-side. Without the pre-sync the
		// crop endpoint would crop the OLD cached cover with bounds measured
		// against the edited image the modal showed.
		const { controller, log } = makeController({
			editedPosterUrl: '',
			serverPosterUrl: '',
			editedCoverUrl: 'https://dmm/new-cover.jpg',
			serverCoverUrl: 'https://dmm/old-cover.jpg'
		});

		await controller.applyPosterCrop();

		expect(log.applyPosterFromUrlAsync).toHaveBeenCalledWith('res-1', 'https://dmm/new-cover.jpg');
		expect(log.mutatePosterCropAsync).toHaveBeenCalledTimes(1);
		expect(log.calls).toEqual(['applying:true', 'persist', 'crop', 'applying:false']);
	});

	it('skips the pre-sync when a cover-backed movie has no source drift (cover unchanged on both sides)', async () => {
		const { controller, log } = makeController({
			editedPosterUrl: '',
			serverPosterUrl: '',
			editedCoverUrl: 'https://dmm/cover.jpg',
			serverCoverUrl: 'https://dmm/cover.jpg'
		});

		await controller.applyPosterCrop();

		expect(log.applyPosterFromUrlAsync).not.toHaveBeenCalled();
		expect(log.mutatePosterCropAsync).toHaveBeenCalledTimes(1);
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

	function makeCropController(opts: { editedPosterUrl?: string; serverPosterUrl?: string; editedCoverUrl?: string; serverCoverUrl?: string } = {}) {
		BaseClient.setSessionID('sid-abc');
		const setCropSourceURL = vi.fn();
		const editedPosterUrl = opts.editedPosterUrl ?? 'https://dmm/poster-GOOD-001.jpg';
		const serverPosterUrl = opts.serverPosterUrl ?? editedPosterUrl;
		const movie: Movie = {
			id: 'GOOD-001',
			title: 'Test Movie',
			poster_url: editedPosterUrl,
			...(opts.editedCoverUrl !== undefined ? { cover_url: opts.editedCoverUrl } : {}),
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
			movie: {
				id: 'GOOD-001',
				title: 'Test Movie',
				poster_url: serverPosterUrl,
				...(opts.serverCoverUrl !== undefined ? { cover_url: opts.serverCoverUrl } : {}),
			},
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

	it('measures the crop on the EDITED URL (via the image proxy) when the server poster_url is empty (cover-backed movie with unsaved edit)', () => {
		// Consistency with applyPosterCrop: the modal shows the edited image
		// even though the server-side poster_url is EMPTY (cover-backed), and
		// applyPosterCrop pre-syncs that same URL before cropping — the crop
		// bounds are measured and applied against the same image.
		const { controller, setCropSourceURL } = makeCropController({
			editedPosterUrl: 'https://dmm/jacket-new.jpg',
			serverPosterUrl: '',
		});
		controller.openPosterCropModal();

		expect(setCropSourceURL).toHaveBeenCalledTimes(1);
		const url = setCropSourceURL.mock.calls[0][0] as string;
		expect(url).toContain(
			`/api/v1/temp/image?url=${encodeURIComponent('https://dmm/jacket-new.jpg')}`,
		);
		expect(url).not.toContain('/posters/job-1/GOOD-001-full.jpg');
		expect(url).toContain('session=sid-abc');
	});

	it('measures the crop on the EDITED cover URL for a cover-backed movie with an unsaved cover_url edit', () => {
		// Zero poster_url on both sides: the effective poster source is the
		// cover (backend: effectivePosterSourceOf), so an unsaved cover_url
		// edit must flip the modal to the image proxy — otherwise the preview
		// shows the cached OLD cover while the user measures bounds meant for
		// the new one.
		const { controller, setCropSourceURL } = makeCropController({
			editedPosterUrl: '',
			serverPosterUrl: '',
			editedCoverUrl: 'https://dmm/new-cover.jpg',
			serverCoverUrl: 'https://dmm/old-cover.jpg',
		});
		controller.openPosterCropModal();

		expect(setCropSourceURL).toHaveBeenCalledTimes(1);
		const url = setCropSourceURL.mock.calls[0][0] as string;
		expect(url).toContain(
			`/api/v1/temp/image?url=${encodeURIComponent('https://dmm/new-cover.jpg')}`,
		);
		expect(url).not.toContain('/posters/job-1/GOOD-001-full.jpg');
	});

	it('uses the cached job poster for a cover-backed movie with no source drift (cover unchanged)', () => {
		const { controller, setCropSourceURL } = makeCropController({
			editedPosterUrl: '',
			serverPosterUrl: '',
			editedCoverUrl: 'https://dmm/cover.jpg',
			serverCoverUrl: 'https://dmm/cover.jpg',
		});
		controller.openPosterCropModal();

		expect(setCropSourceURL).toHaveBeenCalledTimes(1);
		const url = setCropSourceURL.mock.calls[0][0] as string;
		expect(url).toContain('/posters/job-1/GOOD-001-full.jpg');
		expect(url).not.toContain('/api/v1/temp/image?url=');
	});
});

describe('handlePosterCropImageLoad — seeds from server-stored poster_crop_bounds', () => {
	// Regression: with no LOCAL crop state (siblings of a multipart crop,
	// fresh devices), the editor opened on a blind default box even though
	// the server held the recorded crop in movie.poster_crop_bounds — the
	// recorded crop was invisible AND a blind Apply overwrote it.
	function fakeImageLoadEvent(width: number, height: number): Event {
		return {
			currentTarget: {
				naturalWidth: width,
				naturalHeight: height,
				clientWidth: width / 2,
				clientHeight: height / 2,
				offsetLeft: 0,
				offsetTop: 0,
			},
		} as unknown as Event;
	}

	function makeLoadController(
		opts: {
			posterCropBounds?: PosterCropBounds | null;
			savedCrop?: PosterCropState;
		} = {},
	) {
		const movie: Movie = {
			id: 'AAA-001',
			title: 'Test Movie',
			poster_url: 'https://dmm/poster.jpg',
			poster_crop_bounds: opts.posterCropBounds ?? null,
		};
		const result: FileResult = {
			result_id: 'res-1',
			file_path: '/tmp/AAA-001.mp4',
			movie_id: 'AAA-001',
			status: 'completed',
			started_at: '',
			is_multi_part: false,
			part_number: 0,
			part_suffix: '',
			movie,
		};
		const cropStates = new Map<string, PosterCropState>();
		if (opts.savedCrop) cropStates.set(result.file_path, opts.savedCrop);
		const setCropBox = vi.fn();
		const controller = createPosterCropController({
			getBrowser: () => true,
			getJobId: () => 'job-1',
			getCurrentMovie: () => movie,
			getCurrentResult: () => result,
			getShowPosterCropModal: () => true,
			setShowPosterCropModal: () => {},
			setPosterCropLoadError: () => {},
			getCropSourceURL: () => '',
			setCropSourceURL: () => {},
			getCropImageElement: () => null,
			setCropImageElement: () => {},
			getCropMetrics: () => null,
			setCropMetrics: () => {},
			getCropBox: () => null,
			setCropBox,
			getMaxPosterHeight: () => null,
			setMaxPosterHeight: () => {},
			getCropDragState: () => null,
			setCropDragState: () => {},
			getPosterCropStates: () => cropStates,
			applyPosterFromUrlAsync: vi.fn(async () => {}),
			mutatePosterCropAsync: vi.fn(async () => {}),
			setCropApplying: () => {},
		});
		return { controller, setCropBox };
	}

	it('restores the server-stored crop (normalized against its recorded dims) when no local geometry exists', () => {
		const { controller, setCropBox } = makeLoadController({
			posterCropBounds: {
				x: 40,
				y: 20,
				width: 180,
				height: 270,
				image_width: 400,
				image_height: 600,
			},
		});

		controller.handlePosterCropImageLoad(fakeImageLoadEvent(800, 1200));

		// Ratios from the recorded 400x600 dims re-applied to the 800x1200 source.
		expect(setCropBox).toHaveBeenCalledWith({ x: 80, y: 40, width: 360, height: 540 });
	});

	it('falls back to the default box when the server bounds carry no usable source dims', () => {
		const { controller, setCropBox } = makeLoadController({
			posterCropBounds: { x: 40, y: 20, width: 180, height: 270 },
		});

		controller.handlePosterCropImageLoad(fakeImageLoadEvent(800, 1200));

		expect(setCropBox).toHaveBeenCalledWith(getDefaultPosterCropBox(800, 1200));
	});

	it('prefers the locally stored crop state over the server bounds', () => {
		const savedCrop: PosterCropState = { xRatio: 0.5, yRatio: 0, widthRatio: 0.25, heightRatio: 1 };
		const { controller, setCropBox } = makeLoadController({
			posterCropBounds: {
				x: 40,
				y: 20,
				width: 180,
				height: 270,
				image_width: 400,
				image_height: 600,
			},
			savedCrop,
		});

		controller.handlePosterCropImageLoad(fakeImageLoadEvent(800, 1200));

		expect(setCropBox).toHaveBeenCalledWith(restoreCropBox(savedCrop, 800, 1200));
	});
});
