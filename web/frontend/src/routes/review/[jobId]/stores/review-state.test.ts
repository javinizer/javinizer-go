import { describe, it, expect } from 'vitest';
import { calculateCompleteness, type CompletenessTier } from '$lib/utils/completeness';
import type { Movie } from '$lib/api/types';

interface MovieGroup {
	movieId: string;
	results: { file_path: string }[];
	primaryResult: { data: Movie | null };
}

function createTestState(movieGroups: MovieGroup[]) {
	const selectedMovieIds = new Set<string>();
	let lastSelectedMovieId: string | null = null;
	let selectionMode = false;
	const completenessFilter = new Set<CompletenessTier>(['incomplete', 'partial', 'complete']);

	function getFilteredMovieGroups(): MovieGroup[] {
		if (completenessFilter.size === 3) return movieGroups;
		return movieGroups.filter(group => {
			const movie = group.primaryResult.data;
			if (!movie) return false;
			const { tier } = calculateCompleteness(movie);
			return completenessFilter.has(tier);
		});
	}

	function toggleMovieSelection(movieId: string, shiftKey: boolean) {
		if (!selectionMode) return;
		const filtered = getFilteredMovieGroups();
		if (shiftKey && lastSelectedMovieId !== null) {
			const fromIndex = filtered.findIndex(g => g.movieId === lastSelectedMovieId);
			const toIndex = filtered.findIndex(g => g.movieId === movieId);
			if (fromIndex !== -1 && toIndex !== -1) {
				selectMovieRange(fromIndex, toIndex);
			}
		} else {
			if (selectedMovieIds.has(movieId)) {
				selectedMovieIds.delete(movieId);
			} else {
				selectedMovieIds.add(movieId);
				lastSelectedMovieId = movieId;
			}
		}
	}

	function selectMovieRange(fromIndex: number, toIndex: number) {
		const filtered = getFilteredMovieGroups();
		const start = Math.min(fromIndex, toIndex);
		const end = Math.max(fromIndex, toIndex);
		for (let i = start; i <= end; i++) {
			const group = filtered[i];
			if (group) {
				selectedMovieIds.add(group.movieId);
			}
		}
	}

	function selectAllMovies() {
		for (const group of getFilteredMovieGroups()) {
			selectedMovieIds.add(group.movieId);
		}
	}

	function deselectAllMovies() {
		selectedMovieIds.clear();
		lastSelectedMovieId = null;
	}

	function toggleCompletenessTier(tier: CompletenessTier) {
		if (completenessFilter.has(tier)) {
			completenessFilter.delete(tier);
		} else {
			completenessFilter.add(tier);
		}
	}

	function toggleSelectionMode() {
		selectionMode = !selectionMode;
		if (!selectionMode) {
			selectedMovieIds.clear();
			lastSelectedMovieId = null;
		}
	}

	function getTierCounts(): Record<CompletenessTier, number> {
		const counts: Record<CompletenessTier, number> = { incomplete: 0, partial: 0, complete: 0 };
		for (const group of movieGroups) {
			const movie = group.primaryResult.data;
			if (movie) {
				const { tier } = calculateCompleteness(movie);
				counts[tier]++;
			}
		}
		return counts;
	}

	function getSelectedCount(): number {
		return selectedMovieIds.size;
	}

	function isAllSelected(): boolean {
		const filtered = getFilteredMovieGroups();
		return filtered.length > 0 && filtered.every(g => selectedMovieIds.has(g.movieId));
	}

	return {
		selectedMovieIds,
		lastSelectedMovieId: () => lastSelectedMovieId,
		completenessFilter,
		toggleMovieSelection,
		toggleSelectionMode,
		selectMovieRange,
		selectAllMovies,
		deselectAllMovies,
		toggleCompletenessTier,
		getFilteredMovieGroups,
		getTierCounts,
		getSelectedCount,
		isAllSelected,
	};
}

function makeMovie(id: string, overrides: Partial<Movie> = {}): Movie {
	return { id, title: `${id} Title`, ...overrides };
}

function makeMovieGroup(movieId: string, movie: Movie): MovieGroup {
	return {
		movieId,
		results: [{ file_path: `/path/to/${movieId}.mp4` }],
		primaryResult: { data: movie }
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
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(true);
			expect(state.getSelectedCount()).toBe(1);
		});

		it('toggling a selected ID removes it', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(true);
			state.toggleMovieSelection('IPX-535', false);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(false);
			expect(state.getSelectedCount()).toBe(0);
		});

		it('updates lastSelectedMovieId on select only', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.lastSelectedMovieId()).toBe('IPX-535');
			state.toggleMovieSelection('SSIS-001', false);
			expect(state.lastSelectedMovieId()).toBe('SSIS-001');
		});

		it('does not update lastSelectedMovieId on deselect', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			state.toggleMovieSelection('ABC-123', false);
			expect(state.lastSelectedMovieId()).toBe('ABC-123');
			state.toggleMovieSelection('ABC-123', false);
			expect(state.lastSelectedMovieId()).toBe('ABC-123');
		});

		it('shift-click with lastSelectedMovieId selects range', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			state.toggleMovieSelection('SSIS-002', true);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(true);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-002')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-003')).toBe(false);
			expect(state.getSelectedCount()).toBe(4);
		});

		it('shift-click without lastSelectedMovieId toggles normally', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('ABC-123', true);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.getSelectedCount()).toBe(1);
		});

		it('shift-click with reverse range selects correctly', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('SSIS-002', false);
			state.toggleMovieSelection('ABC-123', true);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-002')).toBe(true);
			expect(state.getSelectedCount()).toBe(3);
		});
	});

	describe('toggleSelectionMode', () => {
		it('clears selectedMovieIds when toggled off', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			state.toggleMovieSelection('ABC-123', false);
			expect(state.getSelectedCount()).toBe(2);
			state.toggleSelectionMode();
			expect(state.getSelectedCount()).toBe(0);
		});

		it('resets lastSelectedMovieId when toggled off', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.lastSelectedMovieId()).toBe('IPX-535');
			state.toggleSelectionMode();
			expect(state.lastSelectedMovieId()).toBeNull();
		});

		it('prevents toggleMovieSelection when selection mode is off', () => {
			const state = createTestState(groups);
			state.toggleMovieSelection('IPX-535', false);
			expect(state.getSelectedCount()).toBe(0);
		});
	});

	describe('selectMovieRange', () => {
		it('selects all IDs from start to end index', () => {
			const state = createTestState(groups);
			state.selectMovieRange(1, 3);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-002')).toBe(true);
			expect(state.selectedMovieIds.has('IPX-535')).toBe(false);
		});

		it('works with reversed indices', () => {
			const state = createTestState(groups);
			state.selectMovieRange(3, 1);
			expect(state.selectedMovieIds.has('ABC-123')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.selectedMovieIds.has('SSIS-002')).toBe(true);
		});

		it('single index range selects one movie', () => {
			const state = createTestState(groups);
			state.selectMovieRange(2, 2);
			expect(state.selectedMovieIds.has('SSIS-001')).toBe(true);
			expect(state.getSelectedCount()).toBe(1);
		});
	});

	describe('selectAllMovies', () => {
		it('selects all movie IDs from filtered groups', () => {
			const state = createTestState(groups);
			state.selectAllMovies();
			expect(state.getSelectedCount()).toBe(5);
			for (const g of groups) {
				expect(state.selectedMovieIds.has(g.movieId)).toBe(true);
			}
		});
	});

	describe('deselectAllMovies', () => {
		it('clears all selected IDs', () => {
			const state = createTestState(groups);
			state.selectAllMovies();
			expect(state.getSelectedCount()).toBe(5);
			state.deselectAllMovies();
			expect(state.getSelectedCount()).toBe(0);
		});

		it('resets lastSelectedMovieId to null', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			state.toggleMovieSelection('IPX-535', false);
			expect(state.lastSelectedMovieId()).toBe('IPX-535');
			state.deselectAllMovies();
			expect(state.lastSelectedMovieId()).toBeNull();
		});
	});

	describe('selectedCount', () => {
		it('reflects number of selected IDs', () => {
			const state = createTestState(groups);
			state.toggleSelectionMode();
			expect(state.getSelectedCount()).toBe(0);
			state.toggleMovieSelection('IPX-535', false);
			expect(state.getSelectedCount()).toBe(1);
			state.toggleMovieSelection('ABC-123', false);
			expect(state.getSelectedCount()).toBe(2);
		});
	});

	describe('allSelected', () => {
		it('true when all filtered groups are selected', () => {
			const state = createTestState(groups);
			state.selectAllMovies();
			expect(state.isAllSelected()).toBe(true);
		});

		it('false when no groups are selected', () => {
			const state = createTestState(groups);
			expect(state.isAllSelected()).toBe(false);
		});

		it('false when some groups are not selected', () => {
			const state = createTestState(groups);
			state.selectMovieRange(0, 3);
			expect(state.isAllSelected()).toBe(false);
		});

		it('true when all filtered are selected even with extra IDs from unfiltered', () => {
			const state = createTestState(groups);
			state.selectAllMovies();
			state.selectedMovieIds.add('EXTRA-ID');
			expect(state.isAllSelected()).toBe(true);
		});
	});
});

describe('completeness filter', () => {
	const groups = [
		makeMovieGroup('INCOMPLETE-1', makeMovie('INCOMPLETE-1')),
		makeMovieGroup('PARTIAL-1', makeMovie('PARTIAL-1', {
			poster_url: 'https://example.com/poster.jpg',
			cover_url: 'https://example.com/cover.jpg',
			actresses: [{ id: 1, first_name: 'A' }],
			genres: [{ id: 1, name: 'Drama' }],
		})),
		makeMovieGroup('COMPLETE-1', makeMovie('COMPLETE-1', {
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
		})),
	];

	describe('toggleCompletenessTier', () => {
		it('adds a tier not in filter', () => {
			const state = createTestState(groups);
			state.completenessFilter.delete('incomplete');
			expect(state.completenessFilter.has('incomplete')).toBe(false);
			state.toggleCompletenessTier('incomplete');
			expect(state.completenessFilter.has('incomplete')).toBe(true);
		});

		it('removes a tier already in filter', () => {
			const state = createTestState(groups);
			expect(state.completenessFilter.has('incomplete')).toBe(true);
			state.toggleCompletenessTier('incomplete');
			expect(state.completenessFilter.has('incomplete')).toBe(false);
		});

		it('can remove all tiers resulting in empty filter', () => {
			const state = createTestState(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('partial');
			state.toggleCompletenessTier('complete');
			expect(state.completenessFilter.size).toBe(0);
		});
	});

	describe('filteredMovieGroups', () => {
		it('all tiers active returns all groups', () => {
			const state = createTestState(groups);
			expect(state.getFilteredMovieGroups().length).toBe(3);
		});

		it('only incomplete tier returns incomplete movies', () => {
			const state = createTestState(groups);
			state.toggleCompletenessTier('partial');
			state.toggleCompletenessTier('complete');
			const filtered = state.getFilteredMovieGroups();
			expect(filtered.length).toBe(1);
			expect(filtered[0].movieId).toBe('INCOMPLETE-1');
		});

		it('only partial tier returns partial movies', () => {
			const state = createTestState(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('complete');
			const filtered = state.getFilteredMovieGroups();
			expect(filtered.length).toBe(1);
			expect(filtered[0].movieId).toBe('PARTIAL-1');
		});

		it('only complete tier returns complete movies', () => {
			const state = createTestState(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('partial');
			const filtered = state.getFilteredMovieGroups();
			expect(filtered.length).toBe(1);
			expect(filtered[0].movieId).toBe('COMPLETE-1');
		});

		it('empty filter returns no groups', () => {
			const state = createTestState(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('partial');
			state.toggleCompletenessTier('complete');
			expect(state.getFilteredMovieGroups().length).toBe(0);
		});
	});

	describe('tierCounts', () => {
		it('counts movies per tier correctly', () => {
			const state = createTestState(groups);
			const counts = state.getTierCounts();
			expect(counts.incomplete).toBe(1);
			expect(counts.partial).toBe(1);
			expect(counts.complete).toBe(1);
		});

		it('all incomplete when only title provided', () => {
			const allIncomplete = [
				makeMovieGroup('MOV-1', makeMovie('MOV-1')),
				makeMovieGroup('MOV-2', makeMovie('MOV-2')),
			];
			const state = createTestState(allIncomplete);
			const counts = state.getTierCounts();
			expect(counts.incomplete).toBe(2);
			expect(counts.partial).toBe(0);
			expect(counts.complete).toBe(0);
		});
	});

	describe('selectAllMovies respects filter', () => {
		it('select all only selects filtered groups', () => {
			const state = createTestState(groups);
			state.toggleCompletenessTier('incomplete');
			state.toggleCompletenessTier('partial');
			state.selectAllMovies();
			expect(state.getSelectedCount()).toBe(1);
			expect(state.selectedMovieIds.has('COMPLETE-1')).toBe(true);
		});
	});
});
