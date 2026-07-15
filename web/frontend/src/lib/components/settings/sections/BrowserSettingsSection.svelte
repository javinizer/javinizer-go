<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { slide } from 'svelte/transition';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import type { BrowserConfig, ScrapersConfig, SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
		onChange: (path: string, value: unknown) => void;
	}

	let { config, inputClass, selectClass, onChange }: Props = $props();

	// Helper to safely get nested value with fallback defaults
	// These defaults are used for UI display only, not stored as hardcoded config
	function getBrowserValue<K extends keyof BrowserConfig>(
		key: K,
		defaultValue: NonNullable<BrowserConfig[K]>
	): NonNullable<BrowserConfig[K]> {
		return (config.scrapers?.browser?.[key] ?? defaultValue) as NonNullable<BrowserConfig[K]>;
	}

	// Default values for browser config fields (used for UI only, not hardcoded in component)
	const BROWSER_DEFAULTS: BrowserConfig = {
		enabled: false,
		binary_path: '',
		timeout: 30,
		max_retries: 3,
		headless: true,
		stealth_mode: true,
		window_width: 1920,
		window_height: 1080,
		slow_mo: 0,
		block_images: true,
		block_css: false,
		user_agent: '',
		debug_visible: false
	};

	const browserEnabled = $derived(config.scrapers?.browser?.enabled ?? false);
</script>

<SettingsSection
	title={m.settings_browser_title()}
	description={m.settings_browser_desc()}
	defaultExpanded={false}
>
	<SettingsSubsection title={m.settings_browser_general()}>
		<FormToggle
			id="browser-enabled"
			label={m.settings_browser_enable_label()}
			description={m.settings_browser_enable_desc()}
			checked={getBrowserValue('enabled', BROWSER_DEFAULTS.enabled)}
			onchange={(val) => onChange('scrapers.browser.enabled', val)}
		/>
	</SettingsSubsection>

	{#if browserEnabled}
		<div transition:slide={{ duration: 200 }}>
			<SettingsSubsection title={m.settings_browser_config()}>
				<fieldset class="space-y-0">
					<FormTextInput
						id="browser-binary-path"
						label={m.settings_browser_binary_path_label()}
						description={m.settings_browser_binary_path_desc()}
						value={getBrowserValue('binary_path', '')}
						placeholder="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
						onchange={(val) => onChange('scrapers.browser.binary_path', val)}
					/>

					<div class="grid grid-cols-1 md:grid-cols-2 gap-4 py-4 border-b border-border">
						<FormNumberInput
							id="browser-timeout"
							label={m.settings_browser_timeout_label()}
							description={m.settings_browser_timeout_desc()}
							value={getBrowserValue('timeout', BROWSER_DEFAULTS.timeout)}
							min={1}
							max={300}
							unit={m.common_unit_seconds()}
							onchange={(val) => onChange('scrapers.browser.timeout', val)}
						/>
						<FormNumberInput
							id="browser-max-retries"
							label={m.settings_browser_max_retries_label()}
							description={m.settings_browser_max_retries_desc()}
							value={getBrowserValue('max_retries', BROWSER_DEFAULTS.max_retries)}
							min={0}
							max={10}
							onchange={(val) => onChange('scrapers.browser.max_retries', val)}
						/>
					</div>

					<div class="grid grid-cols-1 md:grid-cols-2 gap-4 py-4 border-b border-border">
						<FormNumberInput
							id="browser-window-width"
							label={m.settings_browser_window_width_label()}
							description={m.settings_browser_window_width_desc()}
							value={getBrowserValue('window_width', BROWSER_DEFAULTS.window_width)}
							min={640}
							max={3840}
							unit={m.common_unit_px()}
							onchange={(val) => onChange('scrapers.browser.window_width', val)}
						/>
						<FormNumberInput
							id="browser-window-height"
							label={m.settings_browser_window_height_label()}
							description={m.settings_browser_window_height_desc()}
							value={getBrowserValue('window_height', BROWSER_DEFAULTS.window_height)}
							min={480}
							max={2160}
							unit={m.common_unit_px()}
							onchange={(val) => onChange('scrapers.browser.window_height', val)}
						/>
					</div>

					<FormTextInput
						id="browser-user-agent"
						label={m.settings_browser_user_agent_label()}
						description={m.settings_browser_user_agent_desc()}
						value={getBrowserValue('user_agent', '')}
						placeholder="Mozilla/5.0..."
						onchange={(val) => onChange('scrapers.browser.user_agent', val)}
					/>
				</fieldset>
			</SettingsSubsection>

			<SettingsSubsection title={m.settings_browser_perf()}>
				<fieldset class="space-y-0">
					<FormToggle
						id="browser-headless"
						label={m.settings_browser_headless_label()}
						description={m.settings_browser_headless_desc()}
						checked={getBrowserValue('headless', BROWSER_DEFAULTS.headless)}
						onchange={(val) => onChange('scrapers.browser.headless', val)}
					/>
					<FormToggle
						id="browser-stealth-mode"
						label={m.settings_browser_stealth_label()}
						description={m.settings_browser_stealth_desc()}
						checked={getBrowserValue('stealth_mode', BROWSER_DEFAULTS.stealth_mode)}
						onchange={(val) => onChange('scrapers.browser.stealth_mode', val)}
					/>
					<FormToggle
						id="browser-block-images"
						label={m.settings_browser_block_images_label()}
						description={m.settings_browser_block_images_desc()}
						checked={getBrowserValue('block_images', BROWSER_DEFAULTS.block_images)}
						onchange={(val) => onChange('scrapers.browser.block_images', val)}
					/>
					<FormToggle
						id="browser-block-css"
						label={m.settings_browser_block_css_label()}
						description={m.settings_browser_block_css_desc()}
						checked={getBrowserValue('block_css', BROWSER_DEFAULTS.block_css)}
						onchange={(val) => onChange('scrapers.browser.block_css', val)}
					/>
				</fieldset>
			</SettingsSubsection>

			<SettingsSubsection title={m.settings_browser_debug()}>
				<fieldset class="space-y-0">
					<FormNumberInput
						id="browser-slow-mo"
						label={m.settings_browser_slow_mo_label()}
						description={m.settings_browser_slow_mo_desc()}
						value={getBrowserValue('slow_mo', BROWSER_DEFAULTS.slow_mo)}
						min={0}
						max={5000}
						unit={m.common_unit_ms()}
						onchange={(val) => onChange('scrapers.browser.slow_mo', val)}
					/>
					<FormToggle
						id="browser-debug-visible"
						label={m.settings_browser_debug_visible_label()}
						description={m.settings_browser_debug_visible_desc()}
						checked={getBrowserValue('debug_visible', BROWSER_DEFAULTS.debug_visible)}
						onchange={(val) => onChange('scrapers.browser.debug_visible', val)}
					/>
				</fieldset>
			</SettingsSubsection>
		</div>
	{/if}
</SettingsSection>
