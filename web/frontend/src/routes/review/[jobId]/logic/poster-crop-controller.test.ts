import { describe, it, expect, vi, afterEach } from 'vitest';
import { createPosterCropController, type PosterCropDragState } from './poster-crop-controller';
import { BaseClient } from '$lib/api/clients/common';
import type { FileResult, Movie, PosterCropBounds } from '$lib/api/types';
import {
	getDefaultPosterCropBox,
	normalizeCropBox,
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
	const mutatePosterCropAsync = vi.fn(async (_jobId: string, _resultId: string, _crop: PosterCropBox, _max?: number, _expectedSourceURL?: string) => {
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

		expect(log.mutatePosterCropAsync).toHaveBeenCalledWith('job-1', 'res-1', expect.any(Object), 1200, sameUrl, undefined);
	});

	it('sends the effective source the coordinates were measured against (poster_url) with the crop', async () => {
		// Codex P2 (cross-tab/device): the server validates expected_source_url
		// under the poster-source lock; a same-tab measurement must name the
		// SERVER-effective source even after the drift pre-sync moved the server
		// to it.
		const { controller, log } = makeController({
			editedPosterUrl: 'https://dmm/jacket-full.jpg',
			serverPosterUrl: 'https://dmm/digital-poster.jpg'
		});

		await controller.applyPosterCrop();

		expect(log.mutatePosterCropAsync).toHaveBeenCalledWith(
			'job-1',
			'res-1',
			expect.any(Object),
			undefined,
			'https://dmm/jacket-full.jpg',
			undefined
		);
	});

	it('sends a cover-backed effective source (poster_url || cover_url) with the crop', async () => {
		const { controller, log } = makeController({
			editedPosterUrl: '',
			serverPosterUrl: '',
			editedCoverUrl: 'https://dmm/cover.jpg',
			serverCoverUrl: 'https://dmm/cover.jpg'
		});

		await controller.applyPosterCrop();

		expect(log.mutatePosterCropAsync).toHaveBeenCalledWith(
			'job-1',
			'res-1',
			expect.any(Object),
			undefined,
			'https://dmm/cover.jpg',
			undefined
		);
	});

	it('omits the expected source (legacy mode) when the movie has neither poster_url nor cover_url', async () => {
		const { controller, log } = makeController({
			editedPosterUrl: '',
			serverPosterUrl: ''
		});

		await controller.applyPosterCrop();

		expect(log.mutatePosterCropAsync).toHaveBeenCalledWith(
			'job-1',
			'res-1',
			expect.any(Object),
			undefined,
			undefined,
			undefined
		);
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

	// Codex P2 (crop source token must be CAPTURED at image-load time): a job
	// refetch may swap the reactive currentMovie A→B while the crop modal is
	// still open. The crop box, metrics and cropSourceURL still describe A,
	// but applyPosterCrop used to recompute editedSource from the now-reactive
	// movie B — making the server guard validate stale A coordinates against
	// B. expected_source_url must be the source captured when the DISPLAYED
	// image loaded.
	describe('expected_source_url is captured from the displayed image, not recomputed from live state', () => {
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

		function makeRefetchController() {
			const movie: Movie = {
				id: 'STARS-136',
				title: 'Test Movie',
				poster_url: 'https://dmm/poster-A.jpg',
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
					poster_url: 'https://dmm/poster-A.jpg',
				}
			};

			// Simulated server: the crop endpoint rejects (409) when the
			// submitted expected_source_url does not match the server's current
			// effective source for the movie.
			const server = { conflicts: 0 };
			const mutatePosterCropAsync = vi.fn(async (_jobId: string, _resultId: string, _crop: PosterCropBox, _max?: number, expectedSourceURL?: string) => {
				const serverSource = result.movie?.poster_url || result.movie?.cover_url || '';
				if (expectedSourceURL && serverSource && expectedSourceURL !== serverSource) {
					server.conflicts++;
					throw Object.assign(new Error('source changed; reload and re-measure'), { status: 409 });
				}
			});
			const applyPosterFromUrlAsync = vi.fn(async () => {});
			const noop = () => {};
			let cropSourceURL = '';

			const controller = createPosterCropController({
				getBrowser: () => true,
				getJobId: () => 'job-1',
				getCurrentMovie: () => movie,
				getCurrentResult: () => result,
				getShowPosterCropModal: () => true,
				setShowPosterCropModal: noop,
				setPosterCropLoadError: noop,
				getCropSourceURL: () => cropSourceURL,
				setCropSourceURL: (u: string) => { cropSourceURL = u; },
				getCropImageElement: () => null,
				setCropImageElement: noop,
				getCropMetrics: () => null,
				setCropMetrics: noop,
				getCropBox: () => ({ x: 0, y: 0, width: 100, height: 200 }),
				setCropBox: noop,
				getMaxPosterHeight: () => null,
				setMaxPosterHeight: noop,
				getCropDragState: (): PosterCropDragState | null => null,
				setCropDragState: noop,
				getPosterCropStates: () => new Map<string, PosterCropState>(),
				applyPosterFromUrlAsync,
				mutatePosterCropAsync,
				setCropApplying: noop
			});

			return { controller, movie, result, server, mutatePosterCropAsync, applyPosterFromUrlAsync };
		}

		it('refetch flips poster A→B after image load: submission sends the CAPTURED A source and the simulated server conflicts (409)', async () => {
			const { controller, movie, result, server, mutatePosterCropAsync, applyPosterFromUrlAsync } = makeRefetchController();

			// Request issued for the A image (movie still A): the guard pair is
			// captured HERE, at issue time.
			controller.openPosterCropModal();

			// Image loads while the movie is A: the displayed image, metrics and
			// box all describe A.
			controller.handlePosterCropImageLoad(fakeImageLoadEvent(400, 600));

			// Job refetch arrives while the modal is open: another tab changed
			// poster A→B. Both the live edited movie and the server-side movie
			// now carry B (no unsaved local edit → no pre-sync drift).
			movie.poster_url = 'https://dmm/poster-B.jpg';
			if (result.movie) result.movie.poster_url = 'https://dmm/poster-B.jpg';

			await controller.applyPosterCrop();

			// The crop IS attempted, but with the CAPTURED A source — never the
			// recomputed live B source.
			expect(mutatePosterCropAsync).toHaveBeenCalledWith(
				'job-1',
				'res-1',
				expect.any(Object),
				undefined,
				'https://dmm/poster-A.jpg',
				undefined
			);
			// No drift pre-sync: live edited source === server source (both B).
			expect(applyPosterFromUrlAsync).not.toHaveBeenCalled();
			// The captured-A token mismatches server-side B → the 409 guard fires.
			expect(server.conflicts).toBe(1);
		});

		it('unchanged-source flow after image load still succeeds (200): captured token matches the server source', async () => {
			const { controller, server, mutatePosterCropAsync, applyPosterFromUrlAsync } = makeRefetchController();

			controller.openPosterCropModal();
			controller.handlePosterCropImageLoad(fakeImageLoadEvent(400, 600));

			// No refetch drift: the movie is still A at Apply time.
			await controller.applyPosterCrop();

			expect(mutatePosterCropAsync).toHaveBeenCalledWith(
				'job-1',
				'res-1',
				expect.any(Object),
				undefined,
				'https://dmm/poster-A.jpg',
				undefined
			);
			expect(applyPosterFromUrlAsync).not.toHaveBeenCalled();
			expect(server.conflicts).toBe(0);
		});

		// Codex P2 follow-up (Codex flagged the surviving window): the refetch
		// can land BETWEEN the image request and its load event — A was
		// requested, the reactive movie flips to B, and only THEN does image A's
		// load event fire (event.currentTarget still describes A). A LOAD-time
		// capture would pair B's source with A's dimensions, defeating the 409
		// guard; the capture must happen when the request is ISSUED.
		it('refetch lands BETWEEN the image request and its load event: the issued A pair still wins (fail pre-fix)', async () => {
			const { controller, movie, result, server, mutatePosterCropAsync, applyPosterFromUrlAsync } = makeRefetchController();

			// Image A's request is issued while the movie is A — the pair is
			// bound to this request NOW, not at load time.
			controller.openPosterCropModal();

			// Refetch swaps the reactive movie AND the server-side movie A→B
			// while image A is still in flight.
			movie.poster_url = 'https://dmm/poster-B.jpg';
			if (result.movie) result.movie.poster_url = 'https://dmm/poster-B.jpg';

			// Only now does image A's load event fire: currentTarget/seeding
			// still describes A — the B movie must NOT overwrite the guard pair.
			controller.handlePosterCropImageLoad(fakeImageLoadEvent(400, 600));

			await controller.applyPosterCrop();

			// Captured A source (server is B) → the 409 guard fires. No drift
			// pre-sync: live edited source === server source (both B).
			expect(mutatePosterCropAsync).toHaveBeenCalledWith(
				'job-1',
				'res-1',
				expect.any(Object),
				undefined,
				'https://dmm/poster-A.jpg',
				undefined
			);
			expect(applyPosterFromUrlAsync).not.toHaveBeenCalled();
			expect(server.conflicts).toBe(1);
		});
	});
});

// Codex P2 (cache generation token): a rescrape or poster-from-URL refresh
// can replace {id}-full.jpg's bytes from the SAME source URL — every URL
// guard passes while the measured coordinate space changed. The controller
// reads the X-Poster-Revision header of the SAME GET response whose bytes it
// displays (fetch → blob → object URL) and threads it to Apply as
// expected_poster_revision. Codex P1 follow-up: the former independent HEAD
// could observe a different generation than the <img>'s GET when a refresh
// landed between the two — the revision must come from the GET that produced
// the displayed bytes, by construction.
describe('expected_poster_revision is captured with the displayed crop image and threaded to Apply', () => {
	function makeRevisionController(fetchCropImage?: (url: string) => Promise<{ objectURL: string; revision: string }>) {
		const movie: Movie = {
			id: 'STARS-136',
			title: 'Test Movie',
			poster_url: 'https://dmm/poster-A.jpg',
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
				poster_url: 'https://dmm/poster-A.jpg',
			}
		};
		let cropSourceURL = '';
		const mutatePosterCropAsync = vi.fn(async () => {});
		const noop = () => {};
		const controller = createPosterCropController({
			getBrowser: () => true,
			getJobId: () => 'job-1',
			getCurrentMovie: () => movie,
			getCurrentResult: () => result,
			getShowPosterCropModal: () => true,
			setShowPosterCropModal: noop,
			setPosterCropLoadError: noop,
			getCropSourceURL: () => cropSourceURL,
			setCropSourceURL: (u: string) => { cropSourceURL = u; },
			getCropImageElement: () => null,
			setCropImageElement: noop,
			getCropMetrics: () => null,
			setCropMetrics: noop,
			getCropBox: () => ({ x: 0, y: 0, width: 100, height: 200 }),
			setCropBox: noop,
			getMaxPosterHeight: () => null,
			setMaxPosterHeight: noop,
			getCropDragState: (): PosterCropDragState | null => null,
			setCropDragState: noop,
			getPosterCropStates: () => new Map<string, PosterCropState>(),
			applyPosterFromUrlAsync: vi.fn(async () => {}),
			mutatePosterCropAsync,
			setCropApplying: noop,
			...(fetchCropImage ? { fetchCropImage } : {}),
			now: () => 12345,
		});
		const flush = () => new Promise((r) => setTimeout(r, 0));
		return { controller, mutatePosterCropAsync, getCropSourceURL: () => cropSourceURL, flush };
	}

	it('fetches the revision WITH the -full.jpg bytes and submits it with the crop', async () => {
		const fetchCropImage = vi.fn(async (_url: string) => ({
			objectURL: 'blob:crop-image-A',
			revision: '1699999999999999999-123456',
		}));
		const { controller, mutatePosterCropAsync, getCropSourceURL, flush } = makeRevisionController(fetchCropImage);

		controller.openPosterCropModal();
		await flush(); // let the async image+revision resolution land on the token

		// ONE GET for the whole flow — the modal displays the fetched blob.
		expect(fetchCropImage).toHaveBeenCalledTimes(1);
		expect(fetchCropImage).toHaveBeenCalledWith(
			'/api/v1/temp/posters/job-1/STARS-136-full.jpg?v=12345'
		);
		expect(getCropSourceURL()).toBe('blob:crop-image-A');

		await controller.applyPosterCrop();

		expect(mutatePosterCropAsync).toHaveBeenCalledWith(
			'job-1',
			'res-1',
			expect.any(Object),
			undefined,
			'https://dmm/poster-A.jpg',
			'1699999999999999999-123456'
		);
	});

	// Structural Codex P1 guard: the revision is a property of the response
	// that produced the DISPLAYED bytes. Simulate a same-URL rescrape landing
	// mid-flight: the FIRST response (bytes the modal shows) carries revision
	// 1; any subsequent independent lookup would observe revision 2. Because
	// the controller never issues that second lookup (single GET per issue),
	// the submitted revision must be revision 1 — matching the displayed
	// image's generation, never a later one.
	it('binds the revision to the DISPLAYED response — by construction no GET-vs-lookup skew is possible', async () => {
		let generation = 1;
		const fetchCropImage = vi.fn(async (_url: string) => ({
			objectURL: `blob:generation-${generation}`,
			revision: `rev-gen-${generation}`,
		}));
		const { controller, mutatePosterCropAsync, getCropSourceURL, flush } = makeRevisionController(fetchCropImage);

		controller.openPosterCropModal();
		generation = 2; // a same-URL refresh lands while the modal is open
		await flush();

		expect(getCropSourceURL()).toBe('blob:generation-1');

		await controller.applyPosterCrop();

		expect(fetchCropImage).toHaveBeenCalledTimes(1);
		expect(mutatePosterCropAsync).toHaveBeenCalledWith(
			'job-1',
			'res-1',
			expect.any(Object),
			undefined,
			'https://dmm/poster-A.jpg',
			'rev-gen-1'
		);
	});

	it('legacy path: no image fetcher → <img> GETs the URL directly, revision omitted (URL-only guard)', async () => {
		const { controller, mutatePosterCropAsync, getCropSourceURL, flush } = makeRevisionController();

		controller.openPosterCropModal();
		await flush();

		expect(getCropSourceURL()).toBe('/api/v1/temp/posters/job-1/STARS-136-full.jpg?v=12345');

		await controller.applyPosterCrop();

		expect(mutatePosterCropAsync).toHaveBeenCalledWith(
			'job-1',
			'res-1',
			expect.any(Object),
			undefined,
			'https://dmm/poster-A.jpg',
			undefined
		);
	});

	it('a failed -full.jpg fetch falls back to the preview image through the SAME bound fetch', async () => {
		// The -full.jpg GET fails; the error path re-issues against the
		// preview {id}.jpg — also via fetchCropImage, so whatever that
		// response displays is what the (empty) revision pair describes.
		const fetchCropImage = vi
			.fn<(url: string) => Promise<{ objectURL: string; revision: string }>>()
			.mockImplementationOnce(() => Promise.reject(new Error('gone')))
			.mockImplementationOnce(() => Promise.resolve({ objectURL: 'blob:preview', revision: '' }));
		const { controller, mutatePosterCropAsync, getCropSourceURL, flush } = makeRevisionController(fetchCropImage);

		controller.openPosterCropModal();
		await flush();
		await flush();

		expect(fetchCropImage).toHaveBeenCalledTimes(2);
		expect(fetchCropImage).toHaveBeenNthCalledWith(2,
			'/api/v1/temp/posters/job-1/STARS-136.jpg?v=12345'
		);
		expect(getCropSourceURL()).toBe('blob:preview');

		await controller.applyPosterCrop();

		// The preview carries no X-Poster-Revision → legacy URL-only guard.
		expect(mutatePosterCropAsync).toHaveBeenCalledWith(
			'job-1',
			'res-1',
			expect.any(Object),
			undefined,
			'https://dmm/poster-A.jpg',
			undefined
		);
	});

	it('a stale image resolution is dropped once a NEWER request superseded the token', async () => {
		let resolveFirst: ((v: { objectURL: string; revision: string }) => void) | null = null;
		const fetchCropImage = vi
			.fn<(url: string) => Promise<{ objectURL: string; revision: string }>>()
			.mockImplementationOnce(
				() => new Promise((r) => { resolveFirst = r; })
			)
			.mockImplementationOnce(
				() => Promise.resolve({ objectURL: 'blob:second', revision: 'rev-second' })
			);
		const { controller, mutatePosterCropAsync, getCropSourceURL, flush } = makeRevisionController(fetchCropImage);

		controller.openPosterCropModal();
		controller.openPosterCropModal(); // re-issue: the first token is superseded
		await flush(); // the second fetch resolves and lands on the NEW token
		// TS's control-flow analysis cannot see the closure assignment above.
		const resolveStale = resolveFirst as ((v: { objectURL: string; revision: string }) => void) | null;
		resolveStale?.({ objectURL: 'blob:stale', revision: 'rev-stale' });
		await flush();

		// The stale resolution neither replaced the display nor barnacled onto
		// the live token.
		expect(getCropSourceURL()).toBe('blob:second');

		await controller.applyPosterCrop();

		expect(mutatePosterCropAsync).toHaveBeenCalledWith(
			'job-1',
			'res-1',
			expect.any(Object),
			undefined,
			'https://dmm/poster-A.jpg',
			'rev-second'
		);
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

	it('prefers the SERVER-stored bounds over older LOCAL geometry when another tab/device re-cropped', () => {
		// Codex P2 (cross-tab/device): this browser saved a crop locally, then
		// ANOTHER tab changed the server-side crop; a job refetch supplied the
		// newer poster_crop_bounds. Seeding from the LOCAL entry would reopen
		// the stale rectangle and an unchanged Apply would overwrite the newer
		// persisted crop.
		const staleLocalCrop: PosterCropState = { xRatio: 0.5, yRatio: 0, widthRatio: 0.25, heightRatio: 1 };
		const { controller, setCropBox } = makeLoadController({
			posterCropBounds: {
				x: 40,
				y: 20,
				width: 180,
				height: 270,
				image_width: 400,
				image_height: 600,
			},
			savedCrop: staleLocalCrop,
		});

		controller.handlePosterCropImageLoad(fakeImageLoadEvent(800, 1200));

		// The server-derived box WINS: ratios from the recorded 400x600 dims
		// re-applied to the 800x1200 source.
		expect(setCropBox).toHaveBeenCalledWith({ x: 80, y: 40, width: 360, height: 540 });
	});

	it('falls back to the locally stored crop state when the server carries no recorded crop', () => {
		const savedCrop: PosterCropState = { xRatio: 0.5, yRatio: 0, widthRatio: 0.25, heightRatio: 1 };
		const { controller, setCropBox } = makeLoadController({
			posterCropBounds: null,
			savedCrop,
		});

		controller.handlePosterCropImageLoad(fakeImageLoadEvent(800, 1200));

		expect(setCropBox).toHaveBeenCalledWith(restoreCropBox(savedCrop, 800, 1200));
	});

	it('own-edit flow presents identical geometry under both rules (response overlay + local save agree)', () => {
		// After this browser's own crop save, the mutation both (a) writes the
		// normalized box into posterCropStates and (b) overlays the response's
		// poster_crop_bounds into the results. Reopening must therefore seed the
		// same box whether the server-bounds rule or the local-fallback rule
		// supplies the geometry — no drift between the two paths.
		const submitted: PosterCropBox = { x: 80, y: 40, width: 360, height: 540 }; // measured on the 800x1200 source
		const metricsThatSavedIt = { sourceWidth: 800, sourceHeight: 1200, displayWidth: 400, displayHeight: 600, imageOffsetX: 0, imageOffsetY: 0 };
		const savedCrop: PosterCropState = normalizeCropBox(submitted, metricsThatSavedIt);
		const { controller, setCropBox } = makeLoadController({
			posterCropBounds: {
				x: submitted.x,
				y: submitted.y,
				width: submitted.width,
				height: submitted.height,
				image_width: 800,
				image_height: 1200,
			},
			savedCrop,
		});

		controller.handlePosterCropImageLoad(fakeImageLoadEvent(800, 1200));

		const serverSourcedBox = { x: 80, y: 40, width: 360, height: 540 };
		expect(setCropBox).toHaveBeenCalledWith(serverSourcedBox);
		expect(restoreCropBox(savedCrop, 800, 1200)).toEqual(serverSourcedBox);
	});
});
