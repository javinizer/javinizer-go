import type { BatchJobResponse, BatchRescrapeResponse, FileResult, Movie, Scraper } from '$lib/api/types';

export type ScalarStrategy =
	| ''
	| 'prefer-nfo'
	| 'prefer-scraper'
	| 'preserve-existing'
	| 'fill-missing-only';

export type ArrayStrategy = '' | 'merge' | 'replace';

interface RescrapeControllerDeps {
	getJobId: () => string;
	getCurrentResult: () => FileResult | undefined;
	getJob: () => BatchJobResponse | null;
	setJob: (job: BatchJobResponse) => void;
	getEditedMovies: () => Map<string, Movie>;
	getAvailableScrapers: () => Scraper[];
	setAvailableScrapers: (scrapers: Scraper[]) => void;
	getRescrapeMovieId: () => string;
	setRescrapeMovieId: (movieId: string) => void;
	getSelectedScrapers: () => string[];
	setSelectedScrapers: (scrapers: string[]) => void;
	getManualSearchMode: () => boolean;
	setManualSearchMode: (manual: boolean) => void;
	getManualSearchInput: () => string;
	setManualSearchInput: (input: string) => void;
	setShowRescrapeModal: (show: boolean) => void;
	getRescrapePreset: () => string | undefined;
	setRescrapePreset: (preset: string | undefined) => void;
	getRescrapeScalarStrategy: () => ScalarStrategy;
	setRescrapeScalarStrategy: (strategy: ScalarStrategy) => void;
	getRescrapeArrayStrategy: () => ArrayStrategy;
	setRescrapeArrayStrategy: (strategy: ArrayStrategy) => void;
	getRescrapingStates: () => Map<string, boolean>;
	toastSuccess: (message: string, duration?: number) => void;
	toastError: (message: string, duration?: number) => void;
	api: {
		getScrapers: () => Promise<Scraper[]>;
		rescrapeBatchMovie: (
			jobId: string,
			movieId: string,
			req: {
				force?: boolean;
				selected_scrapers?: string[];
				manual_search_input?: string;
				preset?: 'conservative' | 'gap-fill' | 'aggressive';
				scalar_strategy?: Exclude<ScalarStrategy, ''>;
				array_strategy?: Exclude<ArrayStrategy, ''>;
			}
		) => Promise<BatchRescrapeResponse>;
	};
}

function setRescrapingState(deps: RescrapeControllerDeps, movieId: string, value: boolean) {
	const states = deps.getRescrapingStates();
	if (value) {
		states.set(movieId, true);
	} else {
		states.delete(movieId);
	}
}

export function createRescrapeController(deps: RescrapeControllerDeps) {
	function applyRescrapePreset(preset: 'conservative' | 'gap-fill' | 'aggressive') {
		deps.setRescrapePreset(preset);
		switch (preset) {
			case 'conservative':
				deps.setRescrapeScalarStrategy('preserve-existing');
				deps.setRescrapeArrayStrategy('merge');
				break;
			case 'gap-fill':
				deps.setRescrapeScalarStrategy('fill-missing-only');
				deps.setRescrapeArrayStrategy('merge');
				break;
			case 'aggressive':
				deps.setRescrapeScalarStrategy('prefer-scraper');
				deps.setRescrapeArrayStrategy('replace');
				break;
		}
	}

	async function openRescrapeModal(movieId: string) {
		if (deps.getAvailableScrapers().length === 0) {
			try {
				deps.setAvailableScrapers(await deps.api.getScrapers());
			} catch (error) {
				deps.toastError('Failed to load scrapers');
				return;
			}
		}

		deps.setRescrapeMovieId(movieId);
		deps.setSelectedScrapers(
			deps
				.getAvailableScrapers()
				.filter((scraper) => scraper.enabled)
				.map((scraper) => scraper.name)
		);
		deps.setManualSearchMode(false);
		deps.setManualSearchInput('');
		deps.setShowRescrapeModal(true);
	}

	async function executeRescrape(mode?: { manualSearchMode: boolean; manualSearchInput: string }) {
		const selectedScrapers = deps.getSelectedScrapers();
		if (selectedScrapers.length === 0) {
			deps.toastError('Please select at least one scraper');
			return;
		}

		const currentResult = deps.getCurrentResult();
		if (!currentResult) {
			deps.toastError('No current movie to update');
			return;
		}

		// Use the passed mode if available, otherwise fall back to deps getters
		const effectiveManualSearchMode = mode?.manualSearchMode ?? deps.getManualSearchMode();
		const effectiveManualSearchInput = mode?.manualSearchInput ?? deps.getManualSearchInput();

		if (effectiveManualSearchMode) {
			const input = effectiveManualSearchInput.trim();
			if (!input) {
				deps.toastError('Please enter a content ID, DVD ID, or URL');
				return;
			}
		}

		const rescrapeMovieId = deps.getRescrapeMovieId();
		setRescrapingState(deps, rescrapeMovieId, true);

		try {
			const scalarStrategy = deps.getRescrapeScalarStrategy();
			const arrayStrategy = deps.getRescrapeArrayStrategy();

			const response = await deps.api.rescrapeBatchMovie(deps.getJobId(), rescrapeMovieId, {
				force: true,
				selected_scrapers: selectedScrapers,
				manual_search_input: effectiveManualSearchMode
					? effectiveManualSearchInput.trim()
					: undefined,
				preset: deps.getRescrapePreset() as 'conservative' | 'gap-fill' | 'aggressive' | undefined,
				scalar_strategy:
					scalarStrategy === ''
						? undefined
						: (scalarStrategy as Exclude<ScalarStrategy, ''>),
				array_strategy:
					arrayStrategy === '' ? undefined : (arrayStrategy as Exclude<ArrayStrategy, ''>)
			});

			const updatedMovie = response.movie;
			if (deps.getJob() && currentResult.file_path) {
				const filePath = currentResult.file_path;
				const currentJob = deps.getJob()!;
				const newResults = { ...currentJob.results };
				newResults[filePath] = {
					...newResults[filePath],
					data: updatedMovie,
					field_sources: response.field_sources ?? newResults[filePath].field_sources,
					actress_sources: response.actress_sources ?? newResults[filePath].actress_sources
				};
				deps.setJob({ ...currentJob, results: newResults });

				// Update rescrapeMovieId with the new ID from the rescrape result
				// This ensures consecutive rescrapes use the correct movie ID
				const newMovieId = updatedMovie?.id || updatedMovie?.content_id || rescrapeMovieId;
				if (newMovieId && newMovieId !== rescrapeMovieId) {
					deps.setRescrapeMovieId(newMovieId);
				}
			}

			const editedMovies = deps.getEditedMovies();
			if (editedMovies.has(currentResult.file_path)) {
				editedMovies.delete(currentResult.file_path);
			}

			deps.toastSuccess(
				effectiveManualSearchMode
					? `Successfully scraped metadata for ${effectiveManualSearchInput.trim()}`
					: `Successfully rescraped ${rescrapeMovieId}`
			);
			deps.setShowRescrapeModal(false);
		} catch (error) {
			const errorMessage = error instanceof Error ? error.message : JSON.stringify(error);
			deps.toastError((effectiveManualSearchMode ? 'Manual search failed: ' : 'Rescrape failed: ') + errorMessage);
		} finally {
			if (deps.getRescrapeMovieId() !== rescrapeMovieId) {
				setRescrapingState(deps, rescrapeMovieId, false);
			}
			setRescrapingState(deps, deps.getRescrapeMovieId(), false);
		}
	}

	return {
		applyRescrapePreset,
		openRescrapeModal,
		executeRescrape
	};
}
