import { SvelteSet } from 'svelte/reactivity';
import type { CompletenessConfig, Movie } from '$lib/api/types';
import { calculateCompleteness, type CompletenessTier } from '$lib/utils/completeness';

interface ReviewSelectionGroup {
	movieId: string;
	primaryResult: { file_path: string; movie?: Movie | null };
}

interface ReviewSelectionDeps<Group extends ReviewSelectionGroup> {
	getMovieGroups: () => Group[];
	getEffectiveMovie: (filePath: string, original: Movie | null | undefined) => Movie | null;
	getCompletenessConfig: () => CompletenessConfig | undefined;
}

/** Selection is independent of visibility: filtering never discards selected IDs. */
export function createReviewSelectionModel<Group extends ReviewSelectionGroup>(
	deps: ReviewSelectionDeps<Group>,
) {
	const selectedMovieIds = new SvelteSet<string>();
	let lastSelectedMovieId = $state<string | null>(null);
	const completenessFilter = new SvelteSet<CompletenessTier>(['incomplete', 'partial', 'complete']);
	let selectionMode = $state(false);

	const tierCounts = $derived.by<Record<CompletenessTier, number>>(() => {
		const counts: Record<CompletenessTier, number> = { incomplete: 0, partial: 0, complete: 0 };
		for (const group of deps.getMovieGroups()) {
			const movie = deps.getEffectiveMovie(
				group.primaryResult.file_path,
				group.primaryResult.movie,
			);
			if (movie) {
				const { tier } = calculateCompleteness(movie, deps.getCompletenessConfig());
				counts[tier]++;
			}
		}
		return counts;
	});

	const filteredMovieGroups = $derived(
		completenessFilter.size === 3
			? deps.getMovieGroups()
			: deps.getMovieGroups().filter((group) => {
					const movie = deps.getEffectiveMovie(
						group.primaryResult.file_path,
						group.primaryResult.movie,
					);
					if (!movie) return false;
					const { tier } = calculateCompleteness(movie, deps.getCompletenessConfig());
					return completenessFilter.has(tier);
				}),
	);

	function toggleMovieSelection(movieId: string, shiftKey: boolean) {
		if (!selectionMode) return;
		if (shiftKey && lastSelectedMovieId !== null) {
			const fromIndex = filteredMovieGroups.findIndex((g) => g.movieId === lastSelectedMovieId);
			const toIndex = filteredMovieGroups.findIndex((g) => g.movieId === movieId);
			// A hidden or removed endpoint deliberately leaves selection and anchor unchanged.
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
		const start = Math.min(fromIndex, toIndex);
		const end = Math.max(fromIndex, toIndex);
		for (let i = start; i <= end; i++) {
			const group = filteredMovieGroups[i];
			if (group) {
				selectedMovieIds.add(group.movieId);
			}
		}
	}

	function selectAllMovies() {
		for (const group of filteredMovieGroups) {
			selectedMovieIds.add(group.movieId);
		}
	}

	function deselectAllMovies() {
		selectedMovieIds.clear();
		lastSelectedMovieId = null;
	}

	function deleteSelectedMovieId(movieId: string) {
		selectedMovieIds.delete(movieId);
		if (lastSelectedMovieId === movieId) {
			lastSelectedMovieId = null;
		}
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
		if (!selectionMode) deselectAllMovies();
	}

	function reset() {
		deselectAllMovies();
		selectionMode = false;
		completenessFilter.clear();
		completenessFilter.add('incomplete');
		completenessFilter.add('partial');
		completenessFilter.add('complete');
	}

	const selectedCount = $derived(selectedMovieIds.size);
	const allSelected = $derived(
		filteredMovieGroups.length > 0 &&
			filteredMovieGroups.every((g) => selectedMovieIds.has(g.movieId)),
	);

	return {
		selectedMovieIds,
		completenessFilter,
		get lastSelectedMovieId() {
			return lastSelectedMovieId;
		},
		get selectionMode() {
			return selectionMode;
		},
		get tierCounts() {
			return tierCounts;
		},
		get filteredMovieGroups() {
			return filteredMovieGroups;
		},
		get selectedCount() {
			return selectedCount;
		},
		get allSelected() {
			return allSelected;
		},
		toggleMovieSelection,
		selectMovieRange,
		selectAllMovies,
		deselectAllMovies,
		deleteSelectedMovieId,
		toggleCompletenessTier,
		toggleSelectionMode,
		reset,
	};
}
