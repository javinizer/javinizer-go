import { beforeEach, describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import type { SettingsConfig } from '$lib/api/types';
import NfoSettingsSection from './NfoSettingsSection.svelte';

beforeEach(() => {
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
});

function makeConfig(nfo: Record<string, unknown> = {}): SettingsConfig {
	return { metadata: { nfo: { enabled: true, ...nfo } } } as unknown as SettingsConfig;
}

async function renderExpanded(config: SettingsConfig) {
	const view = render(NfoSettingsSection, { props: { config } });
	await fireEvent.click(view.getByRole('button', { name: /NFO/i }));
	return view;
}

describe('NfoSettingsSection include_actress_images', () => {
	it('renders the Include actress images toggle enabled by default', async () => {
		const config = makeConfig();
		const view = await renderExpanded(config);
		const toggle = view.getByLabelText('Include actress images') as HTMLInputElement;
		expect(toggle.type).toBe('checkbox');
		expect(toggle.checked).toBe(true);
	});

	it('writes include_actress_images=false when the user disables it', async () => {
		const config = makeConfig();
		const view = await renderExpanded(config);
		await fireEvent.click(view.getByLabelText('Include actress images'));
		expect((config as any).metadata.nfo.include_actress_images).toBe(false);
	});

	it('reflects a persisted disabled setting', async () => {
		const config = makeConfig({ include_actress_images: false });
		const view = await renderExpanded(config);
		const toggle = view.getByLabelText('Include actress images') as HTMLInputElement;
		expect(toggle.checked).toBe(false);
	});
});
