import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import NfoDiffSummary from './NfoDiffSummary.svelte';
import type { FieldDifference } from '$lib/api/types';

vi.mock('$lib/paraglide/messages', () => ({
	movie_field_title: () => 'Title',
	movie_field_description: () => 'Description',
	movie_field_director: () => 'Director',
	movie_field_maker: () => 'Studio / Maker',
	movie_field_label: () => 'Label',
	movie_field_series: () => 'Series',
	movie_field_content_id: () => 'Content ID',
	movie_field_runtime: () => 'Runtime (minutes)',
	movie_field_rating: () => 'Rating',
	movie_field_release_date: () => 'Release Date',
	movie_field_cover: () => 'Cover URL',
	movie_field_poster: () => 'PosterURL',
	movie_field_trailer: () => 'Trailer URL',
	movie_field_actresses: () => 'Actresses',
	movie_field_genres: () => 'Genres',
	review_fields_will_change: ({ count }: { count: number }) => `${count} fields will change`,
	review_fields_will_change_one: ({ count }: { count: number }) => `${count} field will change`,
	review_nfo_diff_fields: ({ count }: { count: number }) => `${count} fields differ from the existing NFO`,
	review_nfo_diff_fields_one: ({ count }: { count: number }) => `${count} field differs from the existing NFO`,
	review_hide_unchanged_fields: () => 'Hide unchanged fields',
	review_show_unchanged_fields: ({ count }: { count: number }) => `Show ${count} unchanged fields`,
	review_nfo_value_label: () => 'Current NFO',
	review_scraped_value_label: () => 'Scraped (new)',
}));

describe('NfoDiffSummary', () => {
	it('renders nothing when there are no differences', () => {
		const { container } = render(NfoDiffSummary, { nfoDifferences: [] });
		expect(container.textContent).toBe('');
	});

	it('renders the summary bar with the change count', () => {
		const diffs: FieldDifference[] = [
			{ field: 'title', nfo_value: 'Old', scraped_value: 'New' },
			{ field: 'maker', nfo_value: 'SOD', scraped_value: 'S1' },
			{ field: 'runtime', nfo_value: 100, scraped_value: 120 },
		];
		const { container } = render(NfoDiffSummary, { nfoDifferences: diffs });
		expect(container.textContent).toContain('3 fields differ from the existing NFO');
	});

	it('uses the singular form for a single change', () => {
		const { container } = render(NfoDiffSummary, {
			nfoDifferences: [{ field: 'title', nfo_value: 'Old', scraped_value: 'New' }],
		});
		expect(container.textContent).toContain('1 field differs from the existing NFO');
	});

	it('expands to show the full color-coded diff table', async () => {
		const diffs: FieldDifference[] = [
			{ field: 'title', nfo_value: 'Old', scraped_value: 'New' },
			{ field: 'cover_url', nfo_value: '', scraped_value: 'https://example.com/x.jpg' },
			{ field: 'trailer_url', nfo_value: 'https://example.com/t.mp4', scraped_value: '' },
		];
		const { container, getByRole } = render(NfoDiffSummary, { nfoDifferences: diffs });
		await fireEvent.click(getByRole('button'));
		expect(container.textContent).toContain('Old');
		expect(container.textContent).toContain('New');
		expect(container.textContent).toContain('https://example.com/x.jpg');
		expect(container.textContent).toContain('https://example.com/t.mp4');
	});

	it('collapses identical fields by default and reveals them on toggle', async () => {
		const diffs: FieldDifference[] = [{ field: 'title', nfo_value: 'Old', scraped_value: 'New' }];
		const { container, getByRole } = render(NfoDiffSummary, { nfoDifferences: diffs });
		await fireEvent.click(getByRole('button'));
		expect(container.textContent).toContain('Show 14 unchanged fields');
		expect(container.textContent).not.toContain('Studio / Maker');
		const unchangedToggle = Array.from(container.querySelectorAll('button')).find((b) =>
			b.textContent?.includes('unchanged'),
		);
		expect(unchangedToggle).toBeTruthy();
		await fireEvent.click(unchangedToggle!);
		expect(container.textContent).toContain('Hide unchanged fields');
		expect(container.textContent).toContain('Studio / Maker');
	});
});
