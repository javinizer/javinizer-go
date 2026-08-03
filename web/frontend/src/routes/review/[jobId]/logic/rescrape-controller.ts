import type {
	BatchJobResponse,
	BatchRescrapeResponse,
	FileResult,
	Movie,
	Scraper,
} from '$lib/api/types';
import { overlayPosterState, posterEditTargetFilePaths } from '../stores/overlay-field-override';

export type ScalarStrategy =
	| ''
	| 'prefer-nfo'
	| 'prefer-scraper'
	| 'preserve-existing'
	| 'fill-missing-only'
	| 'merge-arrays';

export type ArrayStrategy = '' | 'merge' | 'replace';

interface RescrapeControllerDeps {
	getJobId: () => string;
	getCurrentResult: () => FileResult | undefined;
	getJob: () => BatchJobResponse | null;
	setJob: (job: BatchJobResponse) => void;
	getEditedMovies: () => Map<string, Movie>;
	getAvailableScrapers: () => Scraper[];
	setAvailableScrapers: (scrapers: Scraper[]) => void;
	getRescrapeResultId: () => string;
	setRescrapeResultId: (resultId: string) => void;
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
	// Mirrors bulkRescrapeMutation (review-mutations.svelte.ts): a rescrape
	// can replace the effective poster source, which clears the crop bounds
	// server-side — the local geometry measured against the OLD image must
	// then go too, for EVERY part of the movie.
	clearPosterCropStates: (filePaths: string[]) => void;
	// Mirrors bulkRescrapeMutation's invalidation: a successful rescrape
	// commits server-side changes outside the synthetic result update below
	// (multipart siblings, rekeyed assets), and a FAILED rescrape may still
	// have applied partial state — refetch in both cases.
	invalidateJobQueries: () => void;
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
			},
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

	async function openRescrapeModal(resultId: string) {
		if (deps.getAvailableScrapers().length === 0) {
			try {
				deps.setAvailableScrapers(await deps.api.getScrapers());
			} catch (error) {
				deps.toastError('Failed to load scrapers');
				return;
			}
		}

		deps.setRescrapeResultId(resultId);
		deps.setSelectedScrapers(
			deps
				.getAvailableScrapers()
				.filter((scraper) => scraper.enabled)
				.map((scraper) => scraper.name),
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

		const rescrapeResultId = deps.getRescrapeResultId();
		setRescrapingState(deps, rescrapeResultId, true);

		try {
			const scalarStrategy = deps.getRescrapeScalarStrategy();
			const arrayStrategy = deps.getRescrapeArrayStrategy();

			const response = await deps.api.rescrapeBatchMovie(deps.getJobId(), rescrapeResultId, {
				force: true,
				selected_scrapers: selectedScrapers,
				manual_search_input: effectiveManualSearchMode
					? effectiveManualSearchInput.trim()
					: undefined,
				preset: deps.getRescrapePreset() as 'conservative' | 'gap-fill' | 'aggressive' | undefined,
				scalar_strategy:
					scalarStrategy === '' ? undefined : (scalarStrategy as Exclude<ScalarStrategy, ''>),
				array_strategy:
					arrayStrategy === '' ? undefined : (arrayStrategy as Exclude<ArrayStrategy, ''>),
			});

			const updatedMovie = response.movie;
			// Capture the PRE-update job: sibling resolution below keys on the
			// (possibly pre-rekey) movie_id of the rescraped result — same as
			// the bulk path's rescrapedPaths capture.
			const currentJob = deps.getJob();
			if (currentJob && currentResult.file_path) {
				const filePath = currentResult.file_path;
				const newResults = { ...currentJob.results };
				newResults[filePath] = {
					...newResults[filePath],
					status: 'completed',
					// A rescrape can re-key the movie; the result key (file_path)
					// survives, so propagate the new movie_id here or the result
					// keeps pointing at the old key (bulk gets this for free by
					// adopting the whole server job — the single path synthesizes
					// its result update and must mirror it).
					movie_id: updatedMovie.id,
					movie: updatedMovie,
					field_sources: response.field_sources ?? newResults[filePath]?.field_sources,
					actress_sources: response.actress_sources ?? newResults[filePath]?.actress_sources,
				};
				deps.setJob({ ...currentJob, results: newResults });

				// Mirror bulkRescrapeMutation's crop invalidation signal: bounds
				// null/absent in the response means the server cleared the crop
				// (effective poster source changed) — drop the local geometry for
				// every part. Resolve targets off the PRE-rekey job (currentJob)
				// exactly like the bulk path.
				if ((updatedMovie.poster_crop_bounds ?? null) === null) {
					deps.clearPosterCropStates(
						posterEditTargetFilePaths(currentJob.results ?? {}, rescrapeResultId),
					);
				}
			}
			deps.invalidateJobQueries();

			const editedMovies = deps.getEditedMovies();
			if (editedMovies.has(currentResult.file_path)) {
				editedMovies.delete(currentResult.file_path);
			}

			// Sibling pending-edit reconciliation (P1): the backend fans the
			// rescraped movie's poster state out to EVERY same-ID sibling part
			// (rescrape_phase.go I7 single-sibling poster fan-out), but the
			// response and the synthetic result update above only carry the
			// RESCAPED file. A pending edit on sibling B still holds the
			// pre-rescrape poster group (stale cropped_poster_url / crop bounds
			// / Original* revert baseline), and buildMovieToSave resubmits every
			// field on Save — overwriting the server fan-out with that stale
			// snapshot. Overlay the server's poster state onto sibling pending
			// edits (preserving their unrelated edited fields), mirroring
			// applyFieldOverrideToEditedMovies / the bulk path's
			// adopt-server-state contract. Targets resolve off the PRE-rekey
			// job exactly like the crop-geometry clear above.
			if (currentJob) {
				for (const fp of posterEditTargetFilePaths(currentJob.results ?? {}, rescrapeResultId)) {
					if (fp === currentResult.file_path) continue;
					const pending = editedMovies.get(fp);
					if (!pending) continue;
					const merged: Movie = { ...pending };
					overlayPosterState(merged, updatedMovie);
					editedMovies.set(fp, merged);
				}
			}

			deps.toastSuccess(
				effectiveManualSearchMode
					? `Successfully scraped metadata for ${effectiveManualSearchInput.trim()}`
					: 'Successfully rescraped',
			);
			deps.setShowRescrapeModal(false);
		} catch (error) {
			const errorMessage = error instanceof Error ? error.message : JSON.stringify(error);
			deps.toastError(
				(effectiveManualSearchMode ? 'Manual search failed: ' : 'Rescrape failed: ') + errorMessage,
			);
			// A failed single rescrape can still have committed partial
			// server-side state (same failure surface as bulk: merged fields
			// persist before the error) — refetch so the client doesn't keep
			// showing the pre-rescrape snapshot. Mirrors bulkRescrapeMutation.
			deps.invalidateJobQueries();
		} finally {
			setRescrapingState(deps, rescrapeResultId, false);
		}
	}

	return {
		applyRescrapePreset,
		openRescrapeModal,
		executeRescrape,
	};
}
