import { describe, it, expect, vi } from 'vitest';
import { createRescrapeController } from './rescrape-controller';
import type {
	ArrayStrategy,
	ScalarStrategy,
} from './rescrape-controller';
import type {
	BatchJobResponse,
	BatchRescrapeResponse,
	FileResult,
	Movie,
	Scraper,
} from '$lib/api/types';

/**
 * Coverage for the single-result rescrape reconciliation of translation
 * warning fields. The endpoint issues no progress broadcast, so the response
 * is the only carrier: a warning produced by the rescrape must REPLACE any
 * prior warning on the local FileResult, and a clean rescrape (fields
 * omitted) must CLEAR the stale warning from an earlier attempt.
 */

const FILE_PATH = '/tmp/IPX-951.mp4';

function makeMovie(id: string): Movie {
	return { id, title: id } as Movie;
}

function makeResult(overrides: Partial<FileResult> = {}): FileResult {
	return {
		result_id: 'IPX-951',
		file_path: FILE_PATH,
		movie_id: 'IPX-951',
		status: 'completed',
		movie: makeMovie('IPX-951'),
		started_at: '2026-01-01T00:00:00Z',
		is_multi_part: false,
		part_number: 1,
		part_suffix: '',
		...overrides,
	};
}

function makeJob(result: FileResult): BatchJobResponse {
	return {
		id: 'job-1',
		status: 'completed',
		results: { [FILE_PATH]: result },
	} as unknown as BatchJobResponse;
}

interface DepsOverrides {
	currentResult?: FileResult;
	job?: BatchJobResponse;
	rescrapeResponse?: BatchRescrapeResponse;
}

function makeDeps(overrides: DepsOverrides = {}) {
	const currentResult = overrides.currentResult ?? makeResult();
	let currentJob: BatchJobResponse | null = overrides.job ?? makeJob(currentResult);
	const calls = {
		setJob: [] as BatchJobResponse[],
		toastSuccess: [] as string[],
		toastError: [] as string[],
	};

	const deps = {
		getJobId: () => 'job-1',
		getCurrentResult: () => currentResult,
		getJob: () => currentJob,
		setJob: (job: BatchJobResponse, _expectedJobId?: string, _expectedGeneration?: number) => {
			calls.setJob.push(job);
			currentJob = job;
		},
		isCurrentOperation: () => true,
		getRouteGeneration: () => 1,
		getEditedMovies: () => new Map<string, Movie>(),
		getAvailableScrapers: () => [{ name: 'stub', enabled: true } as Scraper],
		setAvailableScrapers: (_scrapers: Scraper[]) => {},
		getRescrapeResultId: () => 'IPX-951',
		setRescrapeResultId: (_resultId: string) => {},
		getSelectedScrapers: () => ['stub'],
		setSelectedScrapers: (_scrapers: string[]) => {},
		getManualSearchMode: () => false,
		setManualSearchMode: (_manual: boolean) => {},
		getManualSearchInput: () => '',
		setManualSearchInput: (_input: string) => {},
		setShowRescrapeModal: (_show: boolean) => {},
		getRescrapePreset: () => undefined,
		setRescrapePreset: (_preset: string | undefined) => {},
		getRescrapeScalarStrategy: () => '' as ScalarStrategy,
		setRescrapeScalarStrategy: (_strategy: ScalarStrategy) => {},
		getRescrapeArrayStrategy: () => '' as ArrayStrategy,
		setRescrapeArrayStrategy: (_strategy: ArrayStrategy) => {},
		getRescrapingStates: () => new Map<string, boolean>(),
		toastSuccess: (message: string, _duration?: number) => {
			calls.toastSuccess.push(message);
		},
		toastError: (message: string, _duration?: number) => {
			calls.toastError.push(message);
		},
		api: {
			getScrapers: async () => [] as Scraper[],
			rescrapeBatchMovie: vi.fn().mockImplementation(async () => {
				if (!overrides.rescrapeResponse) {
					throw new Error('rescrapeResponse fixture required');
				}
				return overrides.rescrapeResponse;
			}),
		},
	};

	return { deps, calls };
}

describe('rescrape controller translation-warning reconciliation', () => {
	it('replaces a stale warning with the warning produced by the rescrape', async () => {
		const stale = makeResult({
			translation_warning: 'Translation (Google Translate (free)): provider unavailable',
			translation_warning_code: 'unavailable',
		});
		const rescrapeResponse: BatchRescrapeResponse = {
			movie: makeMovie('IPX-951'),
			translation_warning:
				'Translation (Google Translate (free)): rate limited - retry later, switch provider, or configure paid mode/API key',
			translation_warning_code: 'rate_limited',
		};
		const { deps, calls } = makeDeps({
			currentResult: stale,
			job: makeJob(stale),
			rescrapeResponse,
		});

		await createRescrapeController(deps).executeRescrape();

		expect(calls.toastError).toHaveLength(0);
		const updated = calls.setJob.at(-1)?.results[FILE_PATH];
		expect(updated?.translation_warning).toBe(rescrapeResponse.translation_warning);
		expect(updated?.translation_warning_code).toBe('rate_limited');
		expect(updated?.movie).toEqual(rescrapeResponse.movie);
		expect(updated?.status).toBe('completed');
	});

	it('surfaces a warning when the prior result was clean', async () => {
		const clean = makeResult();
		const rescrapeResponse: BatchRescrapeResponse = {
			movie: makeMovie('IPX-951'),
			translation_warning: 'Translation (openai): external service error',
			translation_warning_code: 'service_error',
		};
		const { deps, calls } = makeDeps({
			currentResult: clean,
			job: makeJob(clean),
			rescrapeResponse,
		});

		await createRescrapeController(deps).executeRescrape();

		const updated = calls.setJob.at(-1)?.results[FILE_PATH];
		expect(updated?.translation_warning).toBe('Translation (openai): external service error');
		expect(updated?.translation_warning_code).toBe('service_error');
	});

	it('clears the stale warning when the rescrape was translation-clean', async () => {
		const stale = makeResult({
			translation_warning: 'Translation (Google Translate (free)): rate limited - retry later',
			translation_warning_code: 'rate_limited',
		});
		const rescrapeResponse: BatchRescrapeResponse = {
			movie: makeMovie('IPX-951'),
			// no warning fields — this rescrape translated cleanly
		};
		const { deps, calls } = makeDeps({
			currentResult: stale,
			job: makeJob(stale),
			rescrapeResponse,
		});

		await createRescrapeController(deps).executeRescrape();

		const updated = calls.setJob.at(-1)?.results[FILE_PATH];
		expect(updated?.translation_warning).toBeUndefined();
		expect(updated?.translation_warning_code).toBeUndefined();
	});
});
