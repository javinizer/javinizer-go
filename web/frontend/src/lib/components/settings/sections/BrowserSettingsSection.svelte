<script lang="ts">
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
		onChange: (path: string, value: unknown) => void;
	}

	let { config, inputClass, onChange }: Props = $props();

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
	title="Browser Settings"
	description="Configure browser automation for scrapers that require JavaScript rendering. Browser must be enabled both globally here and per-scraper in Scraper Settings."
	defaultExpanded={false}
>
	<SettingsSubsection title="General">
		<FormToggle
			id="browser-enabled"
			label="Enable Browser Automation"
			description="Master switch for browser automation. When disabled, all scrapers will use direct HTTP requests even if they have 'Use Browser' enabled."
			checked={getBrowserValue('enabled', BROWSER_DEFAULTS.enabled)}
			onchange={(val) => onChange('scrapers.browser.enabled', val)}
		/>
	</SettingsSubsection>

	{#if browserEnabled}
		<div transition:slide={{ duration: 200 }}>
			<SettingsSubsection title="Browser Configuration">
				<fieldset class="space-y-0">
					<FormTextInput
						id="browser-binary-path"
						label="Browser Binary Path"
						description="Path to Chrome/Chromium executable. Leave empty for auto-discovery on macOS, Linux, or Windows."
						value={getBrowserValue('binary_path', '')}
						placeholder="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
						onchange={(val) => onChange('scrapers.browser.binary_path', val)}
					/>

					<div class="grid grid-cols-1 md:grid-cols-2 gap-4 py-4 border-b border-border">
						<FormNumberInput
							id="browser-timeout"
							label="Operation Timeout"
							description="Maximum time to wait for browser operations"
							value={getBrowserValue('timeout', BROWSER_DEFAULTS.timeout)}
							min={1}
							max={300}
							unit="seconds"
							onchange={(val) => onChange('scrapers.browser.timeout', val)}
						/>
						<FormNumberInput
							id="browser-max-retries"
							label="Max Retries"
							description="Retry attempts for failed browser operations"
							value={getBrowserValue('max_retries', BROWSER_DEFAULTS.max_retries)}
							min={0}
							max={10}
							onchange={(val) => onChange('scrapers.browser.max_retries', val)}
						/>
					</div>

					<div class="grid grid-cols-1 md:grid-cols-2 gap-4 py-4 border-b border-border">
						<FormNumberInput
							id="browser-window-width"
							label="Window Width"
							description="Browser viewport width in pixels"
							value={getBrowserValue('window_width', BROWSER_DEFAULTS.window_width)}
							min={640}
							max={3840}
							unit="px"
							onchange={(val) => onChange('scrapers.browser.window_width', val)}
						/>
						<FormNumberInput
							id="browser-window-height"
							label="Window Height"
							description="Browser viewport height in pixels"
							value={getBrowserValue('window_height', BROWSER_DEFAULTS.window_height)}
							min={480}
							max={2160}
							unit="px"
							onchange={(val) => onChange('scrapers.browser.window_height', val)}
						/>
					</div>

					<FormTextInput
						id="browser-user-agent"
						label="User Agent Override"
						description="Custom User-Agent string for browser (empty = use scraper's User-Agent)"
						value={getBrowserValue('user_agent', '')}
						placeholder="Mozilla/5.0..."
						onchange={(val) => onChange('scrapers.browser.user_agent', val)}
					/>
				</fieldset>
			</SettingsSubsection>

			<SettingsSubsection title="Performance Options">
				<fieldset class="space-y-0">
					<FormToggle
						id="browser-headless"
						label="Headless Mode"
						description="Run browser without visible window (faster, uses less resources)"
						checked={getBrowserValue('headless', BROWSER_DEFAULTS.headless)}
						onchange={(val) => onChange('scrapers.browser.headless', val)}
					/>
					<FormToggle
						id="browser-stealth-mode"
						label="Stealth Mode"
						description="Enable anti-detection measures to avoid being blocked by websites"
						checked={getBrowserValue('stealth_mode', BROWSER_DEFAULTS.stealth_mode)}
						onchange={(val) => onChange('scrapers.browser.stealth_mode', val)}
					/>
					<FormToggle
						id="browser-block-images"
						label="Block Images"
						description="Block image loading for faster page loads"
						checked={getBrowserValue('block_images', BROWSER_DEFAULTS.block_images)}
						onchange={(val) => onChange('scrapers.browser.block_images', val)}
					/>
					<FormToggle
						id="browser-block-css"
						label="Block CSS"
						description="Block CSS loading (may break some sites)"
						checked={getBrowserValue('block_css', BROWSER_DEFAULTS.block_css)}
						onchange={(val) => onChange('scrapers.browser.block_css', val)}
					/>
				</fieldset>
			</SettingsSubsection>

			<SettingsSubsection title="Debug Options">
				<fieldset class="space-y-0">
					<FormNumberInput
						id="browser-slow-mo"
						label="Slow Motion Delay"
						description="Add delay between actions for debugging (0 = disabled)"
						value={getBrowserValue('slow_mo', BROWSER_DEFAULTS.slow_mo)}
						min={0}
						max={5000}
						unit="ms"
						onchange={(val) => onChange('scrapers.browser.slow_mo', val)}
					/>
					<FormToggle
						id="browser-debug-visible"
						label="Debug Visible"
						description="Show browser window for debugging (overrides headless mode)"
						checked={getBrowserValue('debug_visible', BROWSER_DEFAULTS.debug_visible)}
						onchange={(val) => onChange('scrapers.browser.debug_visible', val)}
					/>
				</fieldset>
			</SettingsSubsection>
		</div>
	{/if}
</SettingsSection>
