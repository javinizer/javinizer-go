import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import NfoDiffBadge from './NfoDiffBadge.svelte';
import type { FieldDifference } from '$lib/api/types';

vi.mock('$lib/paraglide/messages', () => ({
	review_nfo_diff_tooltip: () => 'Current NFO differs from scraped value',
	review_nfo_value_label: () => 'Current NFO',
	review_scraped_value_label: () => 'Scraped (new)',
}));

const diff: FieldDifference = {
	field: 'title',
	nfo_value: '[MKMP-094] Ayaka Tomoda',
	scraped_value: 'Ayaka Tomoda',
};

describe('NfoDiffBadge', () => {
	it('renders the amber alert badge', () => {
		const { container } = render(NfoDiffBadge, { diff });
		const button = container.querySelector('button[aria-label="Current NFO differs from scraped value"]');
		expect(button).toBeTruthy();
		expect(button?.getAttribute('aria-expanded')).toBe('false');
	});

	it('does not show the side-by-side panel until clicked', () => {
		const { container } = render(NfoDiffBadge, { diff });
		expect(container.textContent).not.toContain('[MKMP-094] Ayaka Tomoda');
	});

	it('expands the inline panel on click showing both values', async () => {
		const { container, getByRole } = render(NfoDiffBadge, { diff });
		const button = getByRole('button');
		await fireEvent.click(button);
		expect(button.getAttribute('aria-expanded')).toBe('true');
		expect(container.textContent).toContain('[MKMP-094] Ayaka Tomoda');
		expect(container.textContent).toContain('Ayaka Tomoda');
	});

	it('collapses the panel on a second click', async () => {
		const { container, getByRole } = render(NfoDiffBadge, { diff });
		const button = getByRole('button');
		await fireEvent.click(button);
		await fireEvent.click(button);
		expect(button.getAttribute('aria-expanded')).toBe('false');
		expect(container.textContent).not.toContain('[MKMP-094] Ayaka Tomoda');
	});

	it('renders an em-dash for empty NFO values', async () => {
		const { container, getByRole } = render(NfoDiffBadge, {
			diff: { field: 'cover_url', nfo_value: '', scraped_value: 'https://example.com/x.jpg' },
		});
		await fireEvent.click(getByRole('button'));
		expect(container.textContent).toContain('—');
	});
});
