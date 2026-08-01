import { describe, expect, it } from 'vitest';
import { fireEvent, render } from '@testing-library/svelte';
import VideoOperationSelector from './VideoOperationSelector.svelte';
import componentSource from './VideoOperationSelector.svelte?raw';
import browsePageSource from './+page.svelte?raw';

describe('VideoOperationSelector', () => {
	it('renders one described radio group with all five operations', () => {
		const { getByRole, getAllByRole, getByText } = render(VideoOperationSelector, { value: null });
		expect(getByRole('group', { name: 'Video file operation' })).toBeTruthy();
		expect(getAllByRole('radio')).toHaveLength(5);
		expect(getByText('Metadata and artwork only')).toBeTruthy();
		expect(getByText('Choose what happens to the selected video files. Metadata and media policies are configured separately.')).toBeTruthy();
	});

	it('supports click and standard arrow-key radio navigation with visible selection', async () => {
		const { getByRole, getByText } = render(VideoOperationSelector, { value: null });
		const organize = getByRole('radio', { name: /Organize into another location/ }) as HTMLInputElement;
		const renameInPlace = getByRole('radio', { name: /Rename in place/ }) as HTMLInputElement;
		await fireEvent.click(organize);
		organize.focus();
		await fireEvent.keyDown(organize, { key: 'ArrowRight' });
		await Promise.resolve();
		expect(renameInPlace.checked).toBe(true);
		expect(document.activeElement).toBe(renameInPlace);
		expect(getByText('✓')).toBeTruthy();
	});

	it('associates dynamic validation with the radio group', () => {
		const { getByRole } = render(VideoOperationSelector, { value: null, errorId: 'apply-plan-errors' });
		expect(getByRole('group', { name: 'Video file operation' }).getAttribute('aria-describedby')).toBe('video-operation-help apply-plan-errors');
	});

	it('suppresses reduced-motion effects without duplicating the plan in the action bar', () => {
		const { container } = render(VideoOperationSelector, { value: 'organize' });
		expect(container.querySelector('.sm\\:grid-cols-2')).toBeTruthy();
		expect(componentSource).toContain('prefers-reduced-motion');
		expect(componentSource).toContain('transition: none');
		expect(browsePageSource).not.toContain('data-testid="compact-plan-summary"');
		expect(browsePageSource).toContain('@media (prefers-reduced-motion: reduce)');
		expect(browsePageSource).toContain('animation: none !important');
	});
});