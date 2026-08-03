import { describe, it, expect } from 'vitest';
import type { FileResult, Movie } from '$lib/api/types';
import {
	applyFieldOverrideToEditedMovies,
	applyFieldOverrideToResults,
} from './overlay-field-override';

function makeMovie(overrides: Partial<Movie> = {}): Movie {
	return {
		id: 'ABC-001',
		code: 'ABC-001',
		title: 'Orig Title',
		display_title: 'Orig Title',
		maker: 'Orig Maker',
		release_date: '2020-01-01',
		release_year: 2020,
		...overrides,
	};
}

function makeResult(overrides: Partial<FileResult> = {}): FileResult {
	return {
		result_id: 'r1',
		file_path: '/a/ABC-001-pt1.mp4',
		movie_id: 'ABC-001',
		status: 'completed',
		started_at: '2024-01-01T00:00:00Z',
		is_multi_part: true,
		part_number: 1,
		part_suffix: '',
		movie: makeMovie(),
		...overrides,
	};
}

// Multipart movie ABC-001 (two parts) plus an unrelated movie XYZ-999. Each
// part carries its own per-part identity fields (original_filename) that a
// fan-out must never overwrite with the selected part's values.
function makeMultipartResults(): Record<string, FileResult> {
	return {
		'/a/ABC-001-pt1.mp4': makeResult({
			result_id: 'r1',
			file_path: '/a/ABC-001-pt1.mp4',
			movie: makeMovie({ original_filename: 'ABC-001-pt1.mp4' }),
		}),
		'/a/ABC-001-pt2.mp4': makeResult({
			result_id: 'r2',
			file_path: '/a/ABC-001-pt2.mp4',
			movie: makeMovie({
				poster_url: 'https://example.com/old-poster.jpg',
				original_filename: 'ABC-001-pt2.mp4',
			}),
		}),
		'/a/XYZ-999.mp4': makeResult({
			result_id: 'r3',
			file_path: '/a/XYZ-999.mp4',
			movie_id: 'XYZ-999',
			is_multi_part: false,
			movie: makeMovie({ id: 'XYZ-999', title: 'Other Title' }),
		}),
	};
}

// The response carries the SELECTED part's movie — note its original_filename
// is pt1's. Siblings must never inherit it.
const overriddenPosterMovie = makeMovie({
	title: 'New Title From dmm',
	display_title: 'New Title From dmm',
	poster_url: 'https://dmm.example/new-poster.jpg',
	cover_url: 'https://dmm.example/cover.jpg',
	cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
	should_crop_poster: false,
	poster_crop_bounds: null,
	original_filename: 'ABC-001-pt1.mp4',
});

describe('applyFieldOverrideToResults (multipart fanout)', () => {
	it('selected result takes response.movie wholesale; siblings keep per-part identity fields', () => {
		const results = makeMultipartResults();
		const updated = applyFieldOverrideToResults(results, 'r1', 'title', {
			movie: overriddenPosterMovie,
			field_sources: { title: 'dmm' },
			actress_sources: {},
		});

		// Edited part gets the response movie + provenance:
		expect(updated['/a/ABC-001-pt1.mp4'].movie).toEqual(overriddenPosterMovie);
		expect(updated['/a/ABC-001-pt1.mp4'].field_sources).toEqual({ title: 'dmm' });

		// Sibling keeps its OWN per-part identity fields while gaining the
		// overridden field + synchronized poster state + fan-out provenance:
		const sibling = updated['/a/ABC-001-pt2.mp4'].movie!;
		expect(sibling).not.toEqual(overriddenPosterMovie);
		expect(sibling.original_filename).toBe('ABC-001-pt2.mp4');
		expect(sibling.id).toBe('ABC-001');
		expect(sibling.code).toBe('ABC-001');
		expect(sibling.maker).toBe('Orig Maker');
		expect(sibling.title).toBe('New Title From dmm');
		expect(sibling.display_title).toBe('New Title From dmm');
		expect(updated['/a/ABC-001-pt2.mp4'].field_sources).toEqual({ title: 'dmm' });
		// Full poster state lands on the sibling, so a later whole-movie Save of
		// that entry cannot resubmit the stale pre-override snapshot:
		expect(sibling.poster_url).toBe('https://dmm.example/new-poster.jpg');
		expect(sibling.cover_url).toBe('https://dmm.example/cover.jpg');
		expect(sibling.cropped_poster_url).toBe('/api/v1/temp/posters/job/ABC-001.jpg?v=2');
		expect(sibling.should_crop_poster).toBe(false);
		expect(sibling.poster_crop_bounds).toBeNull();

		// Unrelated movie entries are untouched and the original map is unmodified:
		expect(updated['/a/XYZ-999.mp4'].movie?.title).toBe('Other Title');
		expect(results['/a/ABC-001-pt1.mp4'].movie?.title).toBe('Orig Title');
	});

	it('regression: siblings with distinct original_filenames never inherit the selected part\'s filename', () => {
		const results = makeMultipartResults();
		// Emulate the pre-fix corruption scenario: after the fan-out the user edits
		// and saves the sibling; the snapshot it submits must still carry pt2's
		// filename, or updateBatchMovie would fan the wrong name across all parts.
		const updated = applyFieldOverrideToResults(results, 'r1', 'title', {
			movie: overriddenPosterMovie,
		});

		expect(updated['/a/ABC-001-pt1.mp4'].movie?.original_filename).toBe('ABC-001-pt1.mp4');
		expect(updated['/a/ABC-001-pt2.mp4'].movie?.original_filename).toBe('ABC-001-pt2.mp4');
	});

	it('sibling poster state is mirrored wholesale even for non-poster overrides', () => {
		const results = makeMultipartResults();
		results['/a/ABC-001-pt2.mp4'].movie!.should_crop_poster = true;
		results['/a/ABC-001-pt2.mp4'].movie!.poster_crop_bounds = {
			x: 5,
			y: 5,
			width: 100,
			height: 150,
		};
		const updated = applyFieldOverrideToResults(results, 'r1', 'title', {
			movie: overriddenPosterMovie,
		});
		const sibling = updated['/a/ABC-001-pt2.mp4'].movie!;
		expect(sibling.should_crop_poster).toBe(false);
		expect(sibling.poster_crop_bounds).toBeNull();
		expect(sibling.title).toBe('New Title From dmm');
	});

	it('keeps per-entry provenance when the response omits sources', () => {
		const results = makeMultipartResults();
		results['/a/ABC-001-pt2.mp4'].field_sources = { title: 'r18dev' };
		const updated = applyFieldOverrideToResults(results, 'r1', 'title', {
			movie: overriddenPosterMovie,
		});
		expect(updated['/a/ABC-001-pt2.mp4'].field_sources).toEqual({ title: 'r18dev' });
	});

	it('is a no-op when the response carries no movie', () => {
		const results = makeMultipartResults();
		expect(applyFieldOverrideToResults(results, 'r1', 'title', {})).toBe(results);
	});
});

describe('applyFieldOverrideToEditedMovies (multipart fanout of pending edits)', () => {
	it('overlays the overridden field onto a sibling pending edit while preserving unrelated edits', () => {
		const results = makeMultipartResults();
		const editedMovies = new Map<string, Movie>([
			[
				'/a/ABC-001-pt2.mp4',
				makeMovie({
					// Unrelated pending edits the user already made:
					maker: 'User Edited Maker',
					// Stale pre-override poster state:
					poster_url: 'https://example.com/old-poster.jpg',
					cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=1',
					should_crop_poster: true,
					poster_crop_bounds: { x: 10, y: 10, width: 200, height: 300 },
				}),
			],
			['/a/XYZ-999.mp4', makeMovie({ id: 'XYZ-999', maker: 'Unrelated Edit' })],
		]);

		// Override applied to part 1; the pending edit lives only on part 2.
		applyFieldOverrideToEditedMovies(editedMovies, results, 'r1', 'poster_url', overriddenPosterMovie);

		const sibling = editedMovies.get('/a/ABC-001-pt2.mp4')!;
		expect(sibling.poster_url).toBe('https://dmm.example/new-poster.jpg');
		expect(sibling.cover_url).toBe('https://dmm.example/cover.jpg');
		expect(sibling.cropped_poster_url).toBe('/api/v1/temp/posters/job/ABC-001.jpg?v=2');
		expect(sibling.should_crop_poster).toBe(false);
		expect(sibling.poster_crop_bounds).toBeNull();
		// Unrelated pending edits on the sibling survive untouched:
		expect(sibling.maker).toBe('User Edited Maker');
		// Pending edits on unrelated movies are untouched:
		expect(editedMovies.get('/a/XYZ-999.mp4')?.maker).toBe('Unrelated Edit');
		expect(editedMovies.get('/a/XYZ-999.mp4')?.poster_url).toBeUndefined();
	});

	it('id override re-keys every multipart pending edit and carries the re-pointed poster state', () => {
		const results = makeMultipartResults();
		const editedMovies = new Map<string, Movie>([
			[
				'/a/ABC-001-pt1.mp4',
				makeMovie({
					maker: 'User1',
					cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
					original_cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=1',
					should_crop_poster: true,
					poster_crop_bounds: { x: 9, y: 9, width: 150, height: 200 },
				}),
			],
			[
				'/a/ABC-001-pt2.mp4',
				makeMovie({
					maker: 'User2',
					cropped_poster_url: '/api/v1/temp/posters/job/ABC-001.jpg?v=2',
				}),
			],
			['/a/XYZ-999.mp4', makeMovie({ id: 'XYZ-999', maker: 'Unrelated Edit' })],
		]);
		// The response movie after the id rekey: new id, BOTH preview URLs
		// re-pointed at the new cache key, bounds cleared server-side (absent
		// key), intent synced. Per-part identity (original_filename) must NOT
		// leak onto siblings — the response carries the selected part's name.
		const rekeyed = makeMovie({
			id: 'ABC-002',
			cropped_poster_url: '/api/v1/temp/posters/job/ABC-002.jpg?v=2',
			original_cropped_poster_url: '/api/v1/temp/posters/job/ABC-002.jpg?v=1',
			should_crop_poster: false,
			original_filename: 'ABC-001-pt1.mp4',
		});
		delete rekeyed.poster_crop_bounds;

		applyFieldOverrideToEditedMovies(editedMovies, results, 'r1', 'id', rekeyed);

		for (const path of ['/a/ABC-001-pt1.mp4', '/a/ABC-001-pt2.mp4']) {
			const edit = editedMovies.get(path)!;
			expect(edit.id, path).toBe('ABC-002');
			expect(
				edit.cropped_poster_url,
				`${path} must carry the re-keyed preview URL, not the deleted old cache key`,
			).toBe('/api/v1/temp/posters/job/ABC-002.jpg?v=2');
			expect(edit.original_cropped_poster_url, path).toBe(
				'/api/v1/temp/posters/job/ABC-002.jpg?v=1',
			);
			expect(edit.should_crop_poster, path).toBe(false);
			expect(edit.poster_crop_bounds, path).toBeNull();
		}
		// Unrelated pending edits survive; unrelated movies are untouched.
		expect(editedMovies.get('/a/ABC-001-pt1.mp4')?.maker).toBe('User1');
		expect(editedMovies.get('/a/ABC-001-pt2.mp4')?.maker).toBe('User2');
		expect(editedMovies.get('/a/XYZ-999.mp4')?.id).toBe('XYZ-999');
		expect(editedMovies.get('/a/XYZ-999.mp4')?.maker).toBe('Unrelated Edit');
	});

	it('overlays a scalar field override onto every multipart pending edit', () => {
		const results = makeMultipartResults();
		const editedMovies = new Map<string, Movie>([
			['/a/ABC-001-pt1.mp4', makeMovie({ director: 'User1', title: 'Stale' })],
			['/a/ABC-001-pt2.mp4', makeMovie({ director: 'User2', title: 'Stale' })],
		]);

		const src = makeMovie({ title: 'Server Title', display_title: 'Server Title' });
		applyFieldOverrideToEditedMovies(editedMovies, results, 'r1', 'title', src);

		expect(editedMovies.get('/a/ABC-001-pt1.mp4')?.title).toBe('Server Title');
		expect(editedMovies.get('/a/ABC-001-pt1.mp4')?.director).toBe('User1');
		expect(editedMovies.get('/a/ABC-001-pt2.mp4')?.title).toBe('Server Title');
		expect(editedMovies.get('/a/ABC-001-pt2.mp4')?.director).toBe('User2');
	});
});