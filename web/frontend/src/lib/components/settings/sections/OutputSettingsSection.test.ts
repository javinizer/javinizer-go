import { beforeEach, describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import type { SettingsConfig } from '$lib/api/types';
import OutputSettingsSection from './OutputSettingsSection.svelte';

beforeEach(() => {
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
});

describe('automatic poster fallback setting', () => {
	it.each([undefined, '', 'legacy', 'center', 'fit'] as const)(
		'renders persisted %s and saves selections',
		async (mode) => {
			const config = { output: { poster_fallback_mode: mode } } as SettingsConfig;
			const view = render(OutputSettingsSection, {
				props: { config, inputClass: '', selectClass: '' },
			});
			await fireEvent.click(view.getByRole('button', { name: /Output/i }));
			const select = view.getByLabelText('Automatic poster fallback') as HTMLSelectElement;
			expect(select.value).toBe(mode || 'legacy');
			await fireEvent.change(select, { target: { value: 'fit' } });
			expect(config.output.poster_fallback_mode).toBe('fit');
			expect(view.getByText(/Native posters and manual crops are unchanged/)).toBeTruthy();
		},
	);
});
