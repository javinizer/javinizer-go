import { cleanup, render } from '@testing-library/svelte';
import { afterEach, expect, it, vi } from 'vitest';
import type { OfflineEvidenceReport } from '$lib/api/types';
import Card from './OfflineEvidenceCard.svelte';
const fixture = (): OfflineEvidenceReport => ({
	outcome: 'evidence_available',
	admitted_at: '2026-10-26T10:00:00Z',
	snapshot_digest: 'generation-A',
	observations: [
		{
			source: 'r18dev',
			source_id: '007',
			outcome: 'evidence_available',
			japanese_name: '<img src=x onerror=alert(1)>',
			aliases: ['Whole source alias'],
			source_url: 'https://source.invalid/not-fetched',
			observed_at: '2026-10-25T10:00:00Z',
			dataset_sha256: 'source-digest',
			parser_revision: 'parser-v1',
		},
		{ source: 'r18dev', source_id: '7', outcome: 'unmatched' },
	],
});
afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});
it('old jobs without a sidecar render nothing', () => {
	expect(render(Card).queryByRole('region')).toBeNull();
});
it('renders exact distinct references and escaped historical source assertions, with no actions or fetch', () => {
	const fetch = vi.fn();
	vi.stubGlobal('fetch', fetch);
	const report = fixture();
	const before = structuredClone(report);
	const view = render(Card, { report });
	expect(view.getByText('r18dev:007')).toBeTruthy();
	expect(view.getByText('r18dev:7')).toBeTruthy();
	expect(view.getByText('<img src=x onerror=alert(1)>')).toBeTruthy();
	expect(view.getByText('Whole source alias')).toBeTruthy();
	expect(view.getByText('https://source.invalid/not-fetched')).toBeTruthy();
	expect(view.getByText('generation-A')).toBeTruthy();
	expect(view.container.querySelectorAll('button,input,img,a')).toHaveLength(0);
	expect(fetch).not.toHaveBeenCalled();
	expect(report).toEqual(before);
});
it.each(['stale', 'future', 'ambiguous', 'unsupported_identity', 'provider_failed', 'unexpected'])(
	'hides proposed names for %s observations',
	(outcome) => {
		const report = fixture();
		report.observations![0].outcome = outcome;
		const view = render(Card, { report });
		expect(view.queryByText('<img src=x onerror=alert(1)>')).toBeNull();
		expect(view.queryByText('Whole source alias')).toBeNull();
	},
);
it('renders the selected stored generation and clears when the next committed report is absent', async () => {
	const view = render(Card, { report: fixture() });
	const next = fixture();
	next.snapshot_digest = 'generation-B';
	next.recovered = true;
	await view.rerender({ report: next });
	expect(view.queryByText('generation-A')).toBeNull();
	expect(view.getByText('generation-B')).toBeTruthy();
	expect(view.getByText(/recovered the previous valid artifact/)).toBeTruthy();
	await view.rerender({ report: undefined });
	expect(view.queryByRole('region')).toBeNull();
});
it('explains live API identity unsupported without inventing a match', () => {
	const view = render(Card, { report: { outcome: 'unsupported_identity', admitted_at: 'stored' } });
	expect(view.getByText(/live R18 API IDs are not verified/)).toBeTruthy();
});
