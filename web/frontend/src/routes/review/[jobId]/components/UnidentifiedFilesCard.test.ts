import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import UnidentifiedFilesCard from './UnidentifiedFilesCard.svelte';
import type { FileResult } from '$lib/api/types';

function makeResult(overrides: Partial<FileResult> = {}): FileResult {
	return {
		result_id: 'r1',
		file_path: '/videos/ABP-930.mp4',
		movie_id: 'ABP-930',
		status: 'failed',
		error: 'context deadline exceeded',
		is_multi_part: false,
		part_number: 0,
		part_suffix: '',
		started_at: '',
		...overrides,
	};
}

describe('UnidentifiedFilesCard error rendering', () => {
	const cases: Array<{ code: FileResult['error_code']; expected: string }> = [
		{ code: 'unavailable', expected: 'Source temporarily unavailable' },
		{ code: 'not_found', expected: 'Movie not found' },
		{ code: 'rate_limited', expected: 'Rate limited, try again later' },
		{ code: 'blocked', expected: 'Blocked by source' },
	];

	for (const { code, expected } of cases) {
		it(`renders localized message for error_code=${code}`, () => {
			const { getByText } = render(UnidentifiedFilesCard, {
				props: { failedResults: [makeResult({ error_code: code })], onSearchManually: () => {} }
			});
			expect(getByText(expected)).toBeTruthy();
		});
	}

	it('falls back to raw error string for unknown error_code', () => {
		const { getByText } = render(UnidentifiedFilesCard, {
			props: {
				failedResults: [makeResult({ error_code: 'unknown', error: 'some raw failure' })],
				onSearchManually: () => {}
			}
		});
		expect(getByText('some raw failure')).toBeTruthy();
	});

	it('falls back to raw error string when error_code is absent', () => {
		const { getByText } = render(UnidentifiedFilesCard, {
			props: {
				failedResults: [makeResult({ error_code: undefined, error: 'no code here' })],
				onSearchManually: () => {}
			}
		});
		expect(getByText('no code here')).toBeTruthy();
	});

	it('uses the localized message for the tooltip, not the raw error', () => {
		const { container } = render(UnidentifiedFilesCard, {
			props: {
				failedResults: [makeResult({ error_code: 'unavailable', error: 'context deadline exceeded' })],
				onSearchManually: () => {}
			}
		});
		const p = container.querySelector('p.text-destructive');
		expect(p).toBeTruthy();
		expect(p?.getAttribute('title')).toBe('Source temporarily unavailable');
		expect(p?.textContent?.trim()).toBe('Source temporarily unavailable');
	});
});
