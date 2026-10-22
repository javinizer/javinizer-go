import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest';
import { render, fireEvent, cleanup } from '@testing-library/svelte';
import * as m from '$lib/paraglide/messages';
import type { SettingsConfig } from '$lib/api/types';
import TranslationSettingsSection from './TranslationSettingsSection.svelte';

beforeEach(() => {
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null })
	});
});
afterEach(cleanup);

const providers = [
	{ provider: 'openai', key: 'openai', fallback: 'gpt-4o-mini', placeholder: 'gpt-4o-mini', requiresKey: true },
	{ provider: 'openai-compatible', key: 'openai_compatible', fallback: '', placeholder: 'llama3', requiresKey: false },
	{ provider: 'anthropic', key: 'anthropic', fallback: '', placeholder: 'claude-sonnet-4-20250514', requiresKey: true }
] as const;

for (const provider of providers) {
	describe(`${provider.provider} model picker`, () => {
		async function setup(options: { settings?: Record<string, string>; models?: string[]; loading?: boolean; enabled?: boolean; missing?: boolean } = {}) {
			const config = { metadata: { translation: {
				enabled: options.enabled ?? true, provider: provider.provider,
				...(options.missing ? {} : { [provider.key]: { base_url: 'https://models.example/v1', api_key: 'test-only', ...options.settings } })
			} } } as unknown as SettingsConfig;
			const fetchTranslationModels = vi.fn(async () => {});
			const view = render(TranslationSettingsSection, { props: {
				config, inputClass: 'test-input', selectClass: 'test-select', fetchTranslationModels,
				fetchingTranslationModels: options.loading ?? false, translationModelOptions: options.models ?? []
			} });
			await fireEvent.click(view.getByRole('button', { name: new RegExp(m.settings_translation_title()) }));
			const input = view.container.querySelector(`#translation-${provider.key}-model-input`) as HTMLInputElement;
			return { ...view, config, input, fetchTranslationModels };
		}

		it('keeps provider defaults and creates missing settings on trimmed manual input', async () => {
			const view = await setup({ missing: true });
			expect(view.input.value).toBe(provider.fallback);
			expect(view.input.placeholder).toBe(provider.placeholder);
			expect(view.queryByRole('combobox', { name: m.settings_translation_model_label() })).toBeNull();
			expect(view.getByText(m.settings_translation_manual_override())).toBeTruthy();
			await fireEvent.input(view.input, { target: { value: '  custom-model  ' } });
			expect(view.config.metadata.translation![provider.key]?.model).toBe('custom-model');
		});

		it('renders labeled model options, loaded URL and supports selection and manual override', async () => {
			const view = await setup({ settings: { model: 'model-b' }, models: ['model-a', 'model-b'] });
			const select = view.getByLabelText(m.settings_translation_model_label()) as HTMLSelectElement;
			expect(select.id).toBe(`translation-${provider.key}-model-select`);
			expect(select.value).toBe('model-b');
			expect(select.classList.contains('test-select')).toBe(true);
			expect(view.getByText(m.settings_translation_models_loaded({ url: 'https://models.example/v1' }))).toBeTruthy();
			await fireEvent.change(select, { target: { value: 'model-a' } });
			expect(view.config.metadata.translation![provider.key]?.model).toBe('model-a');
			await fireEvent.input(view.input, { target: { value: ' manual ' } });
			expect(view.config.metadata.translation![provider.key]?.model).toBe('manual');
		});

		it('selects the first fetched model only when the persisted model is undefined', async () => {
			const view = await setup({ models: ['first', 'second'] });
			expect(view.config.metadata.translation![provider.key]?.model).toBe('first');
		});

		it('does not replace a persisted manual model absent from fetched options', async () => {
			const view = await setup({ settings: { model: 'manual' }, models: ['first', 'second'] });
			expect(view.config.metadata.translation![provider.key]?.model).toBe('manual');
			expect(view.input.value).toBe('manual');
			expect((view.getByLabelText(m.settings_translation_model_label()) as HTMLSelectElement).selectedIndex).toBe(-1);
		});

		it.each(['', '   '])('requires a nonblank URL (%j)', async (base_url) => {
			const view = await setup({ settings: { base_url } });
			expect((view.getByRole('button', { name: m.settings_translation_fetch_models() }) as HTMLButtonElement).disabled).toBe(true);
		});

		it.each(['', '   '])('preserves provider API key requirements (%j)', async (api_key) => {
			const view = await setup({ settings: { api_key } });
			const button = view.getByRole('button', { name: m.settings_translation_fetch_models() }) as HTMLButtonElement;
			expect(button.disabled).toBe(provider.requiresKey);
			if (!provider.requiresKey) {
				await fireEvent.click(button);
				expect(view.fetchTranslationModels).toHaveBeenCalledOnce();
			}
		});

		it('delegates fetching and renders the loading state without replacing the manual model', async () => {
			const view = await setup({ settings: { model: 'saved' } });
			await fireEvent.click(view.getByRole('button', { name: m.settings_translation_fetch_models() }));
			expect(view.fetchTranslationModels).toHaveBeenCalledOnce();
			await view.rerender({ fetchingTranslationModels: true });
			expect((view.getByRole('button', { name: m.settings_translation_fetching() }) as HTMLButtonElement).disabled).toBe(true);
			expect(view.container.querySelector('.animate-spin')).toBeTruthy();
			expect(view.input.value).toBe('saved');
			await view.rerender({ fetchingTranslationModels: false, translationModelOptions: [] });
			expect(view.input.value).toBe('saved');
			expect(view.queryByText(m.settings_translation_models_loaded({ url: 'https://models.example/v1' }))).toBeNull();
		});

		it('disables all controls with translation disabled', async () => {
			const view = await setup({ enabled: false, models: ['model-a'] });
			expect(view.input.closest('fieldset')?.disabled).toBe(true);
			expect(view.getByLabelText(m.settings_translation_model_label()).closest('fieldset')?.disabled).toBe(true);
		});
	});
}
