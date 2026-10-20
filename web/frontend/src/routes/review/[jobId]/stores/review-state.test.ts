import { describe, it, expect } from 'vitest';
import { createReviewSelectionModel } from './review-selection.svelte';
import type { Movie } from '$lib/api/types';

interface MovieGroup {
	movieId: string;
	results: { file_path: string }[];
	primaryResult: { file_path: string; movie: Movie | null };
}

function createModel(movieGroups: MovieGroup[]) {
	return createReviewSelectionModel({
		getMovieGroups: () => movieGroups,
		getEffectiveMovie: (_path, original) => original ?? null,
		getCompletenessConfig: () => undefined,
	});
}

function makeMovie(id: string, overrides: Partial<Movie> = {}): Movie {
	return { id, title: `${id} Title`, ...overrides };
}

function makeMovieGroup(movieId: string, movie: Movie): MovieGroup {
	return {
		movieId,
		results: [{ file_path: `/path/to/${movieId}.mp4` }],
		primaryResult: { file_path: `/path/to/${movieId}.mp4`, movie },
	};
}

describe('selection state management', () => {
	const groups = [
		makeMovieGroup('IPX-535', makeMovie('IPX-535')),
		makeMovieGroup('ABC-123', makeMovie('ABC-123')),
		makeMovieGroup('SSIS-001', makeMovie('SSIS-001')),
		makeMovieGroup('SSIS-002', makeMovie('SSIS-002')),
		makeMovieGroup('SSIS-003', makeMovie('SSIS-003')),
	];

	describe('toggleMovieSelection', () => {
		it('toggling an unselected ID adds it', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(true);
			expect(state.selectedCount).toBe(1);
		});

		it('toggling a selected ID removes it', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(true);
			state.toggleMovieSelection('IPX-535', false);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(false);
			expect(state.selectedCount).toBe(0);
		});

		it('updates lastSelectedMovieId on select only', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.lastSelectedMovieId).toBe('IPX-535');
			state.toggleMovieSelection('SSIS-001', false);
			expect(state.lastSelectedMovieId).toBe('SSIS-001');
		});

		it('does not update lastSelectedMovieId on deselect', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			state.toggleMovieSelection('ABC-123', false);
			expect(state.lastSelectedMovieId).toBe('ABC-123');
			state.toggleMovieSelection('ABC-123', false);
			expect(state.lastSelectedMovieId).toBe('ABC-123');
		});

		it('shift-click with lastSelectedMovieId selects range', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			state.toggleMovieSelection('SSIS-002', true);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(true);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-002')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-003')).toBe(false);
			expect(state.selectedCount).toBe(4);
		});

		it('shift-click without lastSelectedMovieId toggles normally', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('ABC-123', true);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedCount).toBe(1);
		});

		it('shift-click with reverse range selects correctly', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('SSIS-002', false);
			state.toggleMovieSelection('ABC-123', true);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-002')).toBe(true);
			expect(state.selectedCount).toBe(3);
		});
	});

	describe('toggleSelectionMode', () => {
		it('clears selectedMovieIds when toggled off', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			state.toggleMovieSelection('ABC-123', false);
			expect(state.selectedCount).toBe(2);
			state.toggleSelectionMode();
			expect(state.selectedCount).toBe(0);
		});

		it('resets lastSelectedMovieId when toggled off', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.lastSelectedMovieId).toBe('IPX-535');
			state.toggleSelectionMode();
			expect(state.lastSelectedMovieId).toBeNull();
		});

		it('prevents toggleMovieSelection when selection mode is off', () => {
			const state = createModel(groups);
			state.toggleMovieSelection('IPX-535', false);
			expect(state.selectedCount).toBe(0);
		});
	});

	describe('selectMovieRange', () => {
		it('selects all IDs from start to end index', () => {
			const state = createModel(groups);
			state.selectMovieRange(1, 3);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-002')).toBe(true);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(false);
		});

		it('works with reversed indices', () => {
			const state = createModel(groups);
			state.selectMovieRange(3, 1);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-002')).toBe(true);
		});

		it('single index range selects one movie', () => {
			const state = createModel(groups);
			state.selectMovieRange(2, 2);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedCount).toBe(1);
		});
	});

	describe('selectAllMovies', () => {
		it('selects all movie IDs from filtered groups', () => {
			const state = createModel(groups);
			state.selectAllMovies();
			expect(state.selectedCount).toBe(5);
			for (const g of groups) {
				expect(state.selectedMovieIds.has(g.movieId)).toBe(true);
			}
		});
	});

	describe('deselectAllMovies', () => {
		it('clears all selected IDs', () => {
			const state = createModel(groups);
			state.selectAllMovies();
			expect(state.selectedCount).toBe(5);
			state.deselectAllMovies();
			expect(state.selectedCount).toBe(0);
		});

		it('resets lastSelectedMovieId to null', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.lastSelectedMovieId).toBe('IPX-535');
			state.deselectAllMovies();
			expect(state.lastSelectedMovieId).toBeNull();
		});
	});

	describe('selectedCount', () => {
		it('reflects number of selected IDs', () => {
			const state = createModel(groups);
			state.toggleSelectionMode();
			expect(state.selectedCount).toBe(0);
			state.toggleMovieSelection('IPX-535', false);
			expect(state.selectedCount).toBe(1);
			state.toggleMovieSelection('ABC-123', false);
			expect(state.selectedCount).toBe(2);
		});
	});

	describe('allSelected', () => {
		it('true when all filtered groups are selected', () => {
			const state = createModel(groups);
			state.selectAllMovies();
			expect(state.allSelected).toBe(true);
		});

		it('false when no groups are selected', () => {
			const state = createModel(groups);
			expect(state.allSelected).toBe(false);
		});

		it('false when some groups are not selected', () => {
			const state = createModel(groups);
			state.selectMovieRange(0, 3);
			expect(state.allSelected).toBe(false);
		});

		it('true when all filtered are selected even with extra IDs from unfiltered', () => {
			const state = createModel(groups);
			state.selectAllMovies();
			state.selectedMovieIds.add('EXTRA-ID');
			expect(state.allSelected).toBe(true);
		});
	});
});

describe('completeness filter', () => {
	const groups = [
		makeMovieGroup('INCOMPLETE-1', makeMovie('INCOMPLETE-1')),
		makeMovieGroup(
			'PARTIAL-1',
			makeMovie('PARTIAL-1', {
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'A' }],
				genres: [{ id: 1, name: 'Drama' }],
			}),
		),
		makeMovieGroup(
			'COMPLETE-1',
			makeMovie('COMPLETE-1', {
				poster_url: 'https://example.com/poster.jpg',
				cover_url: 'https://example.com/cover.jpg',
				actresses: [{ id: 1, first_name: 'A' }],
				genres: [{ id: 1, name: 'Drama' }],
				description: 'A description',
				maker: 'Studio',
				release_date: '2024-01-01',
				director: 'Director',
				runtime: 120,
				trailer_url: 'https://example.com/trailer.mp4',
				screenshot_urls: ['url1', 'url2', 'url3'],
				label: 'Label',
				series: 'Series',
			}),
		),
	];

	describe('toggleCompletenessTier', () => {
		it('adds a tier not in filter', () => {
			const state = createModel(groups);
			state.completenessFilter.delete('incomplete');
			expect(state.completenessFilter.has('incomplete')).toBe(false);
			state.toggleCompletenessTier('incomplete');
			expect(state.completenessFilter.has('incomplete')).toBe(true);
		});

		it('removes a tier already in filter', () => {
			const state = createModel(groups);
			expect(state.completenessFilter.has('incomplete')).toBe(true);
			state.toggleCompletenessTier('incomplete');
			expect(state.completenessFilter.has('incomplete')).toBe(false);
		});

		it('can remove all tiers resulting in empty filter', () => {
			const state = createModel(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('partial');
			state.toggleCompletenessTier('complete');
			expect(state.completenessFilter.size).toBe(0);
		});
	});

	describe('filteredMovieGroups', () => {
		it('all tiers active returns all groups', () => {
			const state = createModel(groups);
			expect(state.filteredMovieGroups.length).toBe(3);
		});

		it('only incomplete tier returns incomplete movies', () => {
			const state = createModel(groups);
			state.toggleCompletenessTier('partial');
			state.toggleCompletenessTier('complete');
			const filtered = state.filteredMovieGroups;
			expect(filtered.length).toBe(1);
			expect(filtered[0].movieId).toBe('INCOMPLETE-1');
		});

		it('only partial tier returns partial movies', () => {
			const state = createModel(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('complete');
			const filtered = state.filteredMovieGroups;
			expect(filtered.length).toBe(1);
			expect(filtered[0].movieId).toBe('PARTIAL-1');
		});

		it('only complete tier returns complete movies', () => {
			const state = createModel(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('partial');
			const filtered = state.filteredMovieGroups;
			expect(filtered.length).toBe(1);
			expect(filtered[0].movieId).toBe('COMPLETE-1');
		});

		it('empty filter returns no groups', () => {
			const state = createModel(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('partial');
			state.toggleCompletenessTier('complete');
			expect(state.filteredMovieGroups.length).toBe(0);
		});
	});

	describe('tierCounts', () => {
		it('counts movies per tier correctly', () => {
			const state = createModel(groups);
			const counts = state.tierCounts;
			expect(counts.incomplete).toBe(1);
			expect(counts.partial).toBe(1);
			expect(counts.complete).toBe(1);
		});

		it('all incomplete when only title provided', () => {
			const allIncomplete = [
				makeMovieGroup('MOV-1', makeMovie('MOV-1')),
				makeMovieGroup('MOV-2', makeMovie('MOV-2')),
			];
			const state = createModel(allIncomplete);
			const counts = state.tierCounts;
			expect(counts.incomplete).toBe(2);
			expect(counts.partial).toBe(0);
			expect(counts.complete).toBe(0);
		});
	});

	describe('selectAllMovies respects filter', () => {
		it('select all only selects filtered groups', () => {
			const state = createModel(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('partial');
			state.selectAllMovies();
			expect(state.selectedCount).toBe(1);
			expect(state.selectedMovieIds.has('COMPLETE-1')).toBe(true);
		});
	});
});

describe('selection model edge cases and lifecycle', () => {
	it('includes missing metadata with every tier enabled but cannot classify it', () => {
		const groups: MovieGroup[] = [
			{
				movieId: 'MISSING',
				results: [],
				primaryResult: { file_path: '/missing.mkv', movie: null },
			},
			makeMovieGroup('A', makeMovie('A')),
		];
		const state = createModel(groups);
		expect(state.filteredMovieGroups).toBe(groups);
		expect(state.tierCounts).toEqual({ incomplete: 1, partial: 0, complete: 0 });
		state.toggleCompletenessTier('complete');
		expect(state.filteredMovieGroups.map((group) => group.movieId)).toEqual(['A']);
	});

	it('ignores out-of-bounds range entries and reports empty lists as not all selected', () => {
		const state = createModel([makeMovieGroup('A', makeMovie('A'))]);
		state.selectMovieRange(-2, 2);
		expect([...state.selectedMovieIds]).toEqual(['A']);
		expect(createModel([]).allSelected).toBe(false);
	});

	it('keeps the anchor when deleting another ID, but clears it when deleting that anchor', () => {
		const state = createModel(['A', 'B', 'C'].map((id) => makeMovieGroup(id, makeMovie(id))));
		state.toggleSelectionMode();
		state.toggleMovieSelection('A', false);
		state.toggleMovieSelection('B', false);
		state.deleteSelectedMovieId('A');
		expect(state.lastSelectedMovieId).toBe('B');
		expect([...state.selectedMovieIds]).toEqual(['B']);
		state.deleteSelectedMovieId('B');
		expect(state.lastSelectedMovieId).toBeNull();
		state.toggleMovieSelection('C', true);
		expect([...state.selectedMovieIds]).toEqual(['C']);
	});

	it('fully resets for a new review while preserving public set identities', () => {
		const state = createModel([makeMovieGroup('A', makeMovie('A'))]);
		const selectedIds = state.selectedMovieIds;
		const filter = state.completenessFilter;
		state.toggleSelectionMode();
		state.toggleMovieSelection('A', false);
		state.toggleCompletenessTier('incomplete');
		state.reset();
		expect(state.selectedMovieIds).toBe(selectedIds);
		expect(state.completenessFilter).toBe(filter);
		expect(state.selectionMode).toBe(false);
		expect(state.lastSelectedMovieId).toBeNull();
		expect(state.selectedCount).toBe(0);
		expect([...state.completenessFilter]).toEqual(['incomplete', 'partial', 'complete']);
	});
});
