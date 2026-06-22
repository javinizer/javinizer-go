/**
 * Multipart file grouping spec.
 *
 * Real stack: browser → Vite proxy → real POST /api/v1/batch/scrape →
 * real scanner + matcher (regex mode, config in cmd/javinizer-e2e) +
 * real worker pool's fan-out + the real e2emock scraper.
 *
 * Pins:
 * - Two files with a `-pt1` / `-pt2` suffix (MULTI-001-pt1.mp4 +
 *   MULTI-001-pt2.mp4) are both parsed by the real matcher to the SAME
 *   MovieID ("MULTI-001"), proving the matcher's regex correctly extracts
 *   the ID prefix and ignores the part suffix.
 * - Both results share the same MovieID — a regression in the matcher's
 *   regex (e.g. one that captures the part suffix as part of the ID)
 *   would produce mismatched IDs and fail here.
 *
 * Why we only assert MovieID consistency (not is_multi_part=True):
 *   The is_multi_part flag is populated by the scanner's FileMatchInfo
 *   derivation, which requires real sibling-file discovery on disk.
 *   The e2emock setup writes 1-byte placeholder files; it does not
 *   exercise the full scanner FileMatchInfo path. So the testable
 *   assertion here is matcher-level: both files parse to the same
 *   MovieID + the scrape job returns one MovieResult per input file
 *   (rather than silently deduplicating).
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
	type BatchJobResponse,
} from '../helpers';

test.describe('Multipart: matcher extracts shared MovieID across -ptN suffixed files', () => {
	test('two files with same ID prefix + -pt1/-pt2 suffix both yield the same MovieID', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, {
			files: [`${DEFAULT_INPUT_DIR}/MULTI-001-pt1.mp4`, `${DEFAULT_INPUT_DIR}/MULTI-001-pt2.mp4`],
		});
		const job: BatchJobResponse = await waitForJobCompletion(request, job_id);

		// Both input files produce a result entry — proves the worker pool
		// didn't silently dedupe by MovieID (which would be a regression
		// hiding the second part).
		const resultEntries = Object.entries(job.results);
		expect(resultEntries, 'job must have a result entry per input file').toHaveLength(2);

		const movieIds = resultEntries.map(([, r]) => r.movie_id);
		// Core assertion: the matcher's regex extracted the SAME MovieID
		// from both filenames, ignoring the -pt1/-pt2 suffix. A regression
		// in the matcher regex would produce mismatched IDs here.
		expect(
			movieIds.every((id) => id === 'MULTI-001'),
			`both files must parse to MovieID "MULTI-001" — got ${JSON.stringify(movieIds)}`,
		).toBeTruthy();
	});

	test('multipart job status reflects scrape outcome of every part (no part silently dropped)', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const job_id = await submitScrape(request, {
			files: [`${DEFAULT_INPUT_DIR}/MULTI-001-pt1.mp4`, `${DEFAULT_INPUT_DIR}/MULTI-001-pt2.mp4`],
		});
		const job = await waitForJobCompletion(request, job_id);

		// The job's per-file counts must add up — total_files equals the
		// number of input files, and (completed + failed + excluded) must
		// sum to total_files. A regression that loses a part silently
		// shows up as a count mismatch.
		expect(job.total_files, 'total_files must equal the number of submitted files').toBe(2);
		const accounted = job.completed + job.failed;
		const excludedCount = Object.values(job.excluded).filter(Boolean).length;
		expect(accounted + excludedCount, 'completed + failed + excluded must sum to total_files').toBe(
			job.total_files,
		);

		// Each result must have a non-empty result_id + non-empty file_path
		// (the field-drop bug class: result_id / file_path dropped on
		// non-happy paths).
		for (const [filePath, result] of Object.entries(job.results)) {
			expect(result.result_id, `result for ${filePath} must have a result_id`).toBeTruthy();
			expect(result.file_path, `result for ${filePath} must have file_path`).toBe(filePath);
		}
	});
});
