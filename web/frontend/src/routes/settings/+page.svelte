<script lang="ts">
	import { onMount } from 'svelte';
	import { portalToBody } from '$lib/actions/portal';
	import { apiClient } from '$lib/api/client';
	import type { ScraperOption } from '$lib/api/types';
	import { Save, RefreshCw, CircleAlert, ArrowLeft, X } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { toastStore } from '$lib/stores/toast';
	import MetadataPriority from '$lib/components/priority/MetadataPriority.svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import ServerSettingsSection from '$lib/components/settings/sections/ServerSettingsSection.svelte';
	import ScraperSettingsSection from '$lib/components/settings/sections/ScraperSettingsSection.svelte';
	import FileOperationsSettingsSection from '$lib/components/settings/sections/FileOperationsSettingsSection.svelte';
	import OutputSettingsSection from '$lib/components/settings/sections/OutputSettingsSection.svelte';
	import DatabaseSettingsSection from '$lib/components/settings/sections/DatabaseSettingsSection.svelte';
	import TranslationSettingsSection from '$lib/components/settings/sections/TranslationSettingsSection.svelte';
	import NfoSettingsSection from '$lib/components/settings/sections/NfoSettingsSection.svelte';
	import ProxySettingsSection from '$lib/components/settings/sections/ProxySettingsSection.svelte';
	import PerformanceSettingsSection from '$lib/components/settings/sections/PerformanceSettingsSection.svelte';
	import FileMatchingSettingsSection from '$lib/components/settings/sections/FileMatchingSettingsSection.svelte';
	import LoggingSettingsSection from '$lib/components/settings/sections/LoggingSettingsSection.svelte';
	import MediaInfoSettingsSection from '$lib/components/settings/sections/MediaInfoSettingsSection.svelte';
	import BrowserSettingsSection from '$lib/components/settings/sections/BrowserSettingsSection.svelte';
	import GenreReplacementsSection from '$lib/components/settings/sections/GenreReplacementsSection.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import {
		getScraperProxyMode as getScraperProxyModePure,
		isProxyConfigDirty,
		isTestValid,
		type ScraperProxyMode,
		type TestResult
	} from '$lib/proxy/proxy-logic';

	interface ScraperItem {
		name: string;
		enabled: boolean;
		displayName: string;
		expanded: boolean;
		options: ScraperOption[];
	}

	let config: any = $state(null);
	let loading = $state(true);
	let saving = $state(false);
	let testingProxy = $state(false);
	let testingFlareSolverr = $state(false);
	let testingProfile = $state<Record<string, boolean>>({});
	let savingProfile = $state<Record<string, boolean>>({});
	let fetchingTranslationModels = $state(false);
	let translationModelOptions = $state<string[]>([]);
	let error = $state<string | null>(null);
	let showConfirmModal = $state(false);
	let scrapers = $state<ScraperItem[]>([]);

	// Test result tracking for test-before-save workflow
	let profileTestResults = $state<Record<string, TestResult>>({});
	let globalProxyTestResult = $state<TestResult | null>(null);
	let globalFlareSolverrTestResult = $state<TestResult | null>(null);

	// Verification tokens from test endpoints (for server-side verification on save)
	let verificationTokens = $state<Record<string, string>>({});

	const TEST_VALIDITY_MS = 5 * 60 * 1000; // 5 minutes

	// Baseline config snapshots for dirty state detection (set on load and after save)
	let proxyConfigBaseline = $state<string>('');
	let flaresolverrConfigBaseline = $state<string>('');

	function updateProxyConfigBaseline(): void {
		proxyConfigBaseline = JSON.stringify(config?.scrapers?.proxy);
		flaresolverrConfigBaseline = JSON.stringify(config?.scrapers?.flaresolverr);
	}

	function checkProxyConfigDirty(): boolean {
		const currentProxy = config?.scrapers?.proxy;
		const currentFlaresolverr = config?.scrapers?.flaresolverr;
		return isProxyConfigDirty(currentProxy, proxyConfigBaseline) ||
			   isProxyConfigDirty(currentFlaresolverr, flaresolverrConfigBaseline);
	}

	function hasPendingProxyTests(): boolean {
		// Check if any enabled proxy scope needs testing
		const globalProxyEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const flaresolverrEnabled = config?.scrapers?.flaresolverr?.enabled ?? false;

		if (!globalProxyEnabled && !flaresolverrEnabled && Object.keys(profileTestResults).length === 0) {
			return false;
		}

		// Check if any enabled scope has no valid test
		if (globalProxyEnabled && !canSaveGlobalProxy()) return true;
		if (flaresolverrEnabled && !canSaveGlobalFlareSolverr()) return true;
		for (const name of Object.keys(profileTestResults)) {
			if (!canSaveProfile(name)) return true;
		}

		return false;
	}

	// Derived state for save button enablement
	function canSaveProfile(profileName: string): boolean {
		const result = profileTestResults[profileName];
		const currentProfile = config?.scrapers?.proxy?.profiles?.[profileName];
		return isTestValid(result, currentProfile, TEST_VALIDITY_MS);
	}

	function canSaveGlobalProxy(): boolean {
		const currentProxy = config?.scrapers?.proxy;
		return isTestValid(globalProxyTestResult, currentProxy, TEST_VALIDITY_MS);
	}

	function canSaveGlobalFlareSolverr(): boolean {
		const currentFlaresolverr = config?.scrapers?.flaresolverr;
		return isTestValid(globalFlareSolverrTestResult, currentFlaresolverr, TEST_VALIDITY_MS);
	}

	function isTestExpired(result: TestResult | null | undefined): boolean {
		// A test is expired if it's not valid (null, failed, expired, or config changed)
		return !isTestValid(result, undefined, TEST_VALIDITY_MS);
	}

	// Clear test result when profile config changes (invalidates test-before-save)
	function invalidateProfileTest(profileName: string): void {
		if (profileTestResults[profileName]) {
			delete profileTestResults[profileName];
		}
	}

	// Clear global proxy test result when global proxy config changes
	function invalidateGlobalProxyTest(): void {
		globalProxyTestResult = null;
		delete verificationTokens['global'];
	}

	// Clear FlareSolverr test result when FlareSolverr config changes
	function invalidateGlobalFlareSolverrTest(): void {
		globalFlareSolverrTestResult = null;
		delete verificationTokens['flaresolverr'];
	}

	function buildVerificationTokenPayload(): Record<string, string> {
		const tokens: Record<string, string> = {};
		if (verificationTokens['global']) tokens['global'] = verificationTokens['global'];
		if (verificationTokens['flaresolverr']) tokens['flaresolverr'] = verificationTokens['flaresolverr'];
		return tokens;
	}

	function hasUnsavedProxyChanges(): boolean {
		return Object.keys(profileTestResults).length > 0 ||
			   globalProxyTestResult !== null ||
			   globalFlareSolverrTestResult !== null;
	}

	function canSafelySave(): boolean {
		// If proxy config hasn't changed, no need to check tests
		if (!checkProxyConfigDirty()) return true;

		// Config is dirty - check if all enabled proxy scopes have valid tests
		const globalProxyEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const flaresolverrEnabled = config?.scrapers?.flaresolverr?.enabled ?? false;

		// Global proxy is enabled and dirty - must have valid test
		if (globalProxyEnabled && !canSaveGlobalProxy()) return false;

		// FlareSolverr is enabled and dirty - must have valid test
		if (flaresolverrEnabled && !canSaveGlobalFlareSolverr()) return false;

		// Check all profiles that have been tested (or need testing)
		for (const name of Object.keys(profileTestResults)) {
			if (!canSaveProfile(name)) return false;
		}

		return true;
	}

	const inputClass =
		'w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background';

	// Build scraper list from config and API
	async function buildScraperList() {
		if (!config) return;
		if (!config.scrapers) config.scrapers = {};
		if (!Array.isArray(config.scrapers.priority)) config.scrapers.priority = [];

		try {
			// Fetch available scrapers from backend
			const response = await apiClient.getAvailableScrapers();

			// Create maps from API data
			const scraperDisplayNames: Record<string, string> = {};
			const scraperOptionsMap: Record<string, ScraperOption[]> = {};
			const scraperEnabledMap: Record<string, boolean> = {};

			response.scrapers.forEach(scraper => {
				scraperDisplayNames[scraper.name] = scraper.display_title;
				scraperOptionsMap[scraper.name] = scraper.options || [];
				scraperEnabledMap[scraper.name] = scraper.enabled;
			});

			// Merge configured priority order with all backend-supported scrapers.
			// This keeps user ordering while ensuring missing scraper sections are still visible.
			const mergedOrder: string[] = [];
			const seen = new Set<string>();

			(config.scrapers.priority || []).forEach((name: string) => {
				if (!seen.has(name)) {
					mergedOrder.push(name);
					seen.add(name);
				}
			});

			response.scrapers.forEach(scraper => {
				if (!seen.has(scraper.name)) {
					mergedOrder.push(scraper.name);
					seen.add(scraper.name);
				}
			});

			scrapers = mergedOrder.map((name: string) => {
				if (!config.scrapers[name]) {
					config.scrapers[name] = { enabled: scraperEnabledMap[name] ?? false };
				} else if (config.scrapers[name].enabled === undefined && scraperEnabledMap[name] !== undefined) {
					config.scrapers[name].enabled = scraperEnabledMap[name];
				}

				return {
					name,
					enabled: config.scrapers[name]?.enabled ?? false,
					displayName: scraperDisplayNames[name] || name,
					expanded: false,
					options: scraperOptionsMap[name] || []
				};
			});
			refreshLocalProxyProfileChoices();
		} catch (e) {
			console.error('Failed to fetch scrapers from API:', e);
			// Fallback to configured order + any scraper sections present in config
			const mergedOrder: string[] = [];
			const seen = new Set<string>();

			(config.scrapers.priority || []).forEach((name: string) => {
				if (!seen.has(name)) {
					mergedOrder.push(name);
					seen.add(name);
				}
			});

			Object.keys(config.scrapers)
				.filter((name: string) => name !== 'priority' && name !== 'proxy')
				.forEach((name: string) => {
					if (!seen.has(name)) {
						mergedOrder.push(name);
						seen.add(name);
					}
				});

			scrapers = mergedOrder.map((name: string) => ({
				name,
				enabled: config.scrapers[name]?.enabled ?? false,
				displayName: name,
				expanded: false,
				options: []
			}));
			refreshLocalProxyProfileChoices();
		}
	}

	// Check if scraper has options to show
	function scraperHasOptions(scraper: ScraperItem): boolean {
		return scraperSupportsProxyOptions(scraper) || getRenderableScraperOptions(scraper).length > 0;
	}

	function scraperSupportsProxyOptions(scraper: ScraperItem): boolean {
		return (scraper.options || []).some((option) => option.key.startsWith('proxy.'));
	}

	function getRenderableScraperOptions(scraper: ScraperItem): ScraperOption[] {
		return (scraper.options || []).filter((option) => !option.key.startsWith('proxy.') && !option.key.startsWith('download_proxy.'));
	}

	function getScraperConfigNames(): string[] {
		if (!config?.scrapers) return [];
		return Object.keys(config.scrapers).filter(
			(name: string) => !['priority', 'proxy', 'user_agent', 'referer', 'timeout_seconds', 'request_timeout_seconds'].includes(name)
		);
	}

	function stripLegacyDownloadProxyFields(): void {
		for (const scraperName of getScraperConfigNames()) {
			const scraperCfg = config?.scrapers?.[scraperName];
			if (scraperCfg?.download_proxy !== undefined) {
				delete scraperCfg.download_proxy;
			}
		}
	}

	function getScraperProxyMode(scraperName: string): ScraperProxyMode {
		const globalEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const override = config?.scrapers?.[scraperName]?.proxy;
		return getScraperProxyModePure(globalEnabled, override);
	}

	function setScraperProxyMode(scraperName: string, mode: ScraperProxyMode): void {
		if (!config?.scrapers) return;
		if (!config.scrapers[scraperName]) config.scrapers[scraperName] = {};
		if (!config.scrapers[scraperName].proxy || typeof config.scrapers[scraperName].proxy !== 'object') {
			config.scrapers[scraperName].proxy = {};
		}

		const proxyCfg = config.scrapers[scraperName].proxy;

		switch (mode) {
			case 'direct':
				proxyCfg.enabled = false;
				proxyCfg.profile = '';
				break;
			case 'inherit':
				proxyCfg.enabled = true;
				proxyCfg.profile = '';  // Empty means inherit default
				break;
			case 'specific':
				proxyCfg.enabled = true;
				if (!(proxyCfg.profile ?? '').trim()) {
					// Pick default or first available profile
					const defaultProfile = config.scrapers.proxy?.default_profile ?? '';
					const firstProfile = getProxyProfileNames()[0] ?? '';
					proxyCfg.profile = defaultProfile || firstProfile;
				}
				break;
		}

		config = JSON.parse(JSON.stringify(config));
	}

	function toggleExpanded(index: number) {
		scrapers[index].expanded = !scrapers[index].expanded;
	}

	function toggleScraperRow(index: number): void {
		const scraper = scrapers[index];
		if (!scraper?.enabled || !scraperHasOptions(scraper)) return;
		toggleExpanded(index);
	}

	function onScraperRowKeydown(event: KeyboardEvent, index: number): void {
		if (event.key !== 'Enter' && event.key !== ' ') return;
		event.preventDefault();
		toggleScraperRow(index);
	}

	function isInteractiveRowTarget(target: EventTarget | null): boolean {
		if (!(target instanceof Element)) return false;
		return !!target.closest('button, input, select, textarea, a, label');
	}

	function onScraperRowClick(event: MouseEvent, index: number): void {
		if (isInteractiveRowTarget(event.target)) return;
		toggleScraperRow(index);
	}

	// Helper to get option value from config (using snake_case keys)
	function getOptionValue(scraperName: string, optionKey: string): any {
		if (optionKey === 'download_proxy.enabled') {
			const downloadProxy = getNestedValue(config?.scrapers?.[scraperName], 'download_proxy');
			if (!downloadProxy || typeof downloadProxy !== 'object') return false;
			if (downloadProxy.enabled !== undefined) return !!downloadProxy.enabled;
			// Backward compatibility: legacy config may have profile/url without explicit enabled.
			return !!(
				downloadProxy.profile ||
				downloadProxy.url ||
				downloadProxy.username ||
				downloadProxy.password ||
				downloadProxy.use_main_proxy
			);
		}
		
		// Find the scraper and option definition to check for default value
		const scraper = scrapers.find(s => s.name === scraperName);
		const option = scraper?.options?.find(o => o.key === optionKey);
		
		// ALL scraper options now at top level - no more extra map
		const currentValue = getNestedValue(config?.scrapers?.[scraperName], optionKey);
		
		// Return default if no value set (undefined, null, or empty string)
		if (currentValue === undefined || currentValue === null || currentValue === '') {
			return option?.default ?? currentValue;
		}
		
		return currentValue;
	}

	function getNestedValue(obj: any, path: string): any {
		if (!obj) return undefined;
		return path.split('.').reduce((acc: any, key: string) => acc?.[key], obj);
	}

	function setNestedValue(obj: any, path: string, value: any): void {
		const keys = path.split('.');
		let current = obj;
		for (let i = 0; i < keys.length - 1; i++) {
			const key = keys[i];
			if (!current[key] || typeof current[key] !== 'object') {
				current[key] = {};
			}
			current = current[key];
		}
		current[keys[keys.length - 1]] = value;
	}

	function parseOptionNumber(value: string): number | undefined {
		const parsed = parseInt(value, 10);
		return Number.isNaN(parsed) ? undefined : parsed;
	}

	// Sanitize HTTP header values to prevent header injection
	function sanitizeHeaderValue(value: string): string {
		// Remove newlines, carriage returns, and control characters
		return value.replace(/[\r\n\x00-\x1F\x7F]/g, '');
	}

	function handleScraperUserAgentInput(e: Event) {
		const target = e.target as HTMLInputElement;
		config.scrapers.user_agent = sanitizeHeaderValue(target.value);
	}

	function handleScraperRefererInput(e: Event) {
		const target = e.target as HTMLInputElement;
		config.scrapers.referer = sanitizeHeaderValue(target.value);
	}

	function ensureProxyProfilesInitialized(): void {
		if (!config?.scrapers) config.scrapers = {};
		if (!config.scrapers.proxy) config.scrapers.proxy = {};
		if (!config.scrapers.proxy.profiles || typeof config.scrapers.proxy.profiles !== 'object' || Array.isArray(config.scrapers.proxy.profiles)) {
			config.scrapers.proxy.profiles = {};
		}

		const profiles = config.scrapers.proxy.profiles;
		if (Object.keys(profiles).length === 0) {
			profiles.main = {
				url: config.scrapers.proxy?.url ?? '',
				username: config.scrapers.proxy?.username ?? '',
				password: config.scrapers.proxy?.password ?? ''
			};
		}

		const defaultProfile = config.scrapers.proxy.default_profile;
		if (!defaultProfile || !profiles[defaultProfile]) {
			const names = Object.keys(profiles).sort();
			config.scrapers.proxy.default_profile = names.includes('main') ? 'main' : (names[0] ?? '');
		}
	}

	function ensureTranslationConfig(): void {
		if (!config?.metadata) config.metadata = {};
		if (!config.metadata.translation || typeof config.metadata.translation !== 'object') {
			config.metadata.translation = {};
		}

		const translation = config.metadata.translation;
		if (translation.enabled === undefined) translation.enabled = false;
		if (!translation.provider) translation.provider = 'openai';
		if (!translation.source_language) translation.source_language = 'en';
		if (!translation.target_language) translation.target_language = 'ja';
		if (!translation.timeout_seconds) translation.timeout_seconds = 60;
		if (translation.apply_to_primary === undefined) translation.apply_to_primary = true;
		if (translation.overwrite_existing_target === undefined) translation.overwrite_existing_target = true;

		if (!translation.fields || typeof translation.fields !== 'object') translation.fields = {};
		if (translation.fields.title === undefined) translation.fields.title = true;
		if (translation.fields.original_title === undefined) translation.fields.original_title = true;
		if (translation.fields.description === undefined) translation.fields.description = true;
		if (translation.fields.director === undefined) translation.fields.director = true;
		if (translation.fields.maker === undefined) translation.fields.maker = true;
		if (translation.fields.label === undefined) translation.fields.label = true;
		if (translation.fields.series === undefined) translation.fields.series = true;
		if (translation.fields.genres === undefined) translation.fields.genres = true;
		if (translation.fields.actresses === undefined) translation.fields.actresses = true;

		if (!translation.openai || typeof translation.openai !== 'object') translation.openai = {};
		if (!translation.openai.base_url) translation.openai.base_url = 'https://api.openai.com/v1';
		if (!translation.openai.model) translation.openai.model = 'gpt-4o-mini';
		if (!translation.openai.api_key) translation.openai.api_key = '';

		if (!translation.deepl || typeof translation.deepl !== 'object') translation.deepl = {};
		if (!translation.deepl.mode) translation.deepl.mode = 'free';
		if (!translation.deepl.base_url) translation.deepl.base_url = '';
		if (!translation.deepl.api_key) translation.deepl.api_key = '';

		if (!translation.google || typeof translation.google !== 'object') translation.google = {};
		if (!translation.google.mode) translation.google.mode = 'free';
		if (!translation.google.base_url) translation.google.base_url = '';
		if (!translation.google.api_key) translation.google.api_key = '';

		if (!translation.openai_compatible || typeof translation.openai_compatible !== 'object') translation.openai_compatible = {};
		if (!translation.openai_compatible.base_url) translation.openai_compatible.base_url = 'http://localhost:11434/v1';
		if (!translation.openai_compatible.model) translation.openai_compatible.model = '';
		if (!translation.openai_compatible.api_key) translation.openai_compatible.api_key = '';

		if (!translation.anthropic || typeof translation.anthropic !== 'object') translation.anthropic = {};
		if (!translation.anthropic.base_url) translation.anthropic.base_url = 'https://api.anthropic.com';
		if (!translation.anthropic.model) translation.anthropic.model = '';
		if (!translation.anthropic.api_key) translation.anthropic.api_key = '';
	}

	function getProxyProfileNames(): string[] {
		if (!config?.scrapers?.proxy?.profiles) return [];
		return Object.keys(config.scrapers.proxy.profiles).sort();
	}

	function updateScraperProfileRefs(oldName: string, newName: string): void {
		if (!config?.scrapers) return;
		getScraperConfigNames().forEach((scraperName: string) => {
			const scraperCfg = config.scrapers[scraperName];
			if (scraperCfg?.proxy?.profile === oldName) scraperCfg.proxy.profile = newName;
		});
	}

	function renameProxyProfile(oldName: string, rawNewName: string): void {
		if (!config?.scrapers?.proxy?.profiles) return;
		const newName = rawNewName.trim();
		if (!newName || oldName === newName) return;
		if (config.scrapers.proxy.profiles[newName]) {
			toastStore.error(`Profile "${newName}" already exists`, 4000);
			return;
		}

		const profileData = config.scrapers.proxy.profiles[oldName];
		delete config.scrapers.proxy.profiles[oldName];
		config.scrapers.proxy.profiles[newName] = profileData;

		if (config.scrapers.proxy.default_profile === oldName) {
			config.scrapers.proxy.default_profile = newName;
		}
		updateScraperProfileRefs(oldName, newName);
		config.scrapers.proxy.profiles = { ...config.scrapers.proxy.profiles };
		refreshLocalProxyProfileChoices();

		// Transfer test result from old name to new name (with updated snapshot)
		if (profileTestResults[oldName]) {
			profileTestResults[newName] = {
				...profileTestResults[oldName],
				configSnapshot: JSON.stringify(profileData)
			};
			delete profileTestResults[oldName];
		}

		invalidateGlobalProxyTest(); // Global proxy config may have changed
	}

	function addProxyProfile(): void {
		ensureProxyProfilesInitialized();
		let idx = 1;
		let name = `profile-${idx}`;
		while (config.scrapers.proxy.profiles[name]) {
			idx += 1;
			name = `profile-${idx}`;
		}

		config.scrapers.proxy.profiles[name] = {
			url: '',
			username: '',
			password: ''
		};

		if (!config.scrapers.proxy.default_profile) {
			config.scrapers.proxy.default_profile = name;
		}
		config.scrapers.proxy.profiles = { ...config.scrapers.proxy.profiles };
		refreshLocalProxyProfileChoices();
		invalidateGlobalProxyTest(); // New profile may become default
	}

	function removeProxyProfile(name: string): void {
		if (!config?.scrapers?.proxy?.profiles?.[name]) return;
		delete config.scrapers.proxy.profiles[name];
		updateScraperProfileRefs(name, '');

		const names = getProxyProfileNames();
		if (config.scrapers.proxy.default_profile === name) {
			config.scrapers.proxy.default_profile = names[0] ?? '';
		}
		config.scrapers.proxy.profiles = { ...config.scrapers.proxy.profiles };
		refreshLocalProxyProfileChoices();

		// Clean up test result for removed profile
		if (profileTestResults[name]) {
			delete profileTestResults[name];
		}

		invalidateGlobalProxyTest(); // Default profile may have changed
	}

	function setProxyProfileField(name: string, field: 'url' | 'username' | 'password', value: string): void {
		if (!config?.scrapers?.proxy?.profiles?.[name]) return;
		config.scrapers.proxy.profiles[name][field] = value;
		invalidateProfileTest(name); // Clear test result since config changed
		invalidateGlobalProxyTest(); // Global proxy may use this profile as default
	}

	async function saveProxyProfile(profileName: string): Promise<void> {
		if (!config?.scrapers?.proxy?.profiles?.[profileName]) return;
		if (savingProfile[profileName]) return;

		savingProfile[profileName] = true;
		error = null;
		config.scrapers.proxy.profiles = { ...config.scrapers.proxy.profiles };
		refreshLocalProxyProfileChoices();
		try {
			await apiClient.request('/api/v1/config', {
				method: 'PUT',
				body: JSON.stringify(config)
			});
			toastStore.success(`Profile "${profileName}" saved successfully.`, 4000);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save profile';
			toastStore.error(error, 5000);
		} finally {
			savingProfile[profileName] = false;
		}
	}

	async function runNamedProxyProfileTest(profileName: string) {
		const profile = config?.scrapers?.proxy?.profiles?.[profileName];
		if (!profile) {
			toastStore.error(`Profile "${profileName}" not found`, 5000);
			return;
		}
		if (!profile.url?.trim()) {
			toastStore.error(`Profile "${profileName}" needs a proxy URL before testing`, 5000);
			return;
		}

		testingProfile[profileName] = true;
		try {
			const defaultProfileName = config?.scrapers?.proxy?.default_profile ?? '';
			const shouldAlsoValidateGlobalProxy = (config?.scrapers?.proxy?.enabled ?? false) && profileName === defaultProfileName;

			// Test with current unsaved form values
			const result = await apiClient.testProxy({
				mode: 'direct',
				proxy: shouldAlsoValidateGlobalProxy
					? {
							enabled: true,
							profile: defaultProfileName,
							profiles: config?.scrapers?.proxy?.profiles ?? {}
						}
					: {
							enabled: true,
							profile: '',
							profiles: {
								[profileName]: {
									url: profile.url,
									username: profile.username ?? '',
									password: profile.password ?? ''
								}
							}
						}
			});

			// Store test result for save button state (with config snapshot for dirty detection)
			profileTestResults[profileName] = {
				success: result.success,
				timestamp: Date.now(),
				message: result.message,
				configSnapshot: JSON.stringify(profile)
			};

			// Testing the default profile also validates global proxy state for save gating.
			if (shouldAlsoValidateGlobalProxy) {
				globalProxyTestResult = {
					success: result.success,
					timestamp: Date.now(),
					message: result.message,
					configSnapshot: JSON.stringify(config?.scrapers?.proxy),
					verificationToken: result.verification_token,
					tokenExpiresAt: result.token_expires_at
				};
				if (result.verification_token) {
					verificationTokens['global'] = result.verification_token;
				} else if (!result.success) {
					delete verificationTokens['global'];
				}
			} else if (result.success && result.verification_token && (config?.scrapers?.proxy?.enabled ?? false)) {
				// For non-default profiles: also store global token when global proxy is enabled
				// This allows saving profiles even when they're not the default
				verificationTokens['global'] = result.verification_token;
			}

			if (result.success) {
				toastStore.success(`Profile "${profileName}" test passed (${result.duration_ms}ms): ${result.message}`, 7000);
			} else {
				toastStore.error(`Profile "${profileName}" test failed (${result.duration_ms}ms): ${result.message}`, 7000);
			}
		} catch (e) {
			profileTestResults[profileName] = { success: false, timestamp: Date.now() };
			const msg = e instanceof Error ? e.message : 'Profile proxy test failed';
			toastStore.error(msg, 7000);
		} finally {
			testingProfile[profileName] = false;
		}
	}

	function proxyProfileChoices() {
		return [{ value: '', label: 'Inherit Default' }, ...getProxyProfileNames().map((name) => ({ value: name, label: name }))];
	}

	function refreshLocalProxyProfileChoices(): void {
		const choices = proxyProfileChoices();
		scrapers = scrapers.map((scraper) => ({
			...scraper,
			options: (scraper.options || []).map((option) => {
				if (option.key === 'proxy.profile') {
					return { ...option, choices };
				}
				return option;
			})
		}));
	}

	function isOptionDisabled(scraperName: string, optionKey: string): boolean {
		const globalProxyEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const globalFlareSolverrEnabled = config?.scrapers?.flaresolverr?.enabled ?? false;
		const globalBrowserEnabled = config?.scrapers?.browser?.enabled ?? false;
		const globalScrapeActress = config?.scrapers?.scrape_actress ?? false;
		const scraperCfg = config?.scrapers?.[scraperName] ?? {};

		if (optionKey === 'use_flaresolverr') {
			return !globalFlareSolverrEnabled;
		}

		if (optionKey === 'use_browser') {
			return !globalBrowserEnabled;
		}

		if (optionKey === 'scrape_actress') {
			return !globalScrapeActress;
		}

		if (optionKey.startsWith('proxy.')) {
			if (!globalProxyEnabled) return true;

			const scraperProxyEnabled = scraperCfg?.proxy?.enabled ?? false;
			if (optionKey === 'proxy.enabled') return false;
			if (!scraperProxyEnabled) return true;

			if (optionKey.startsWith('proxy.flaresolverr.')) {
				if (optionKey === 'proxy.flaresolverr.enabled') return false;
				return !(scraperCfg?.proxy?.flaresolverr?.enabled ?? false);
			}

			return false;
		}

		return false;
	}

	// Helper to set option value in config (using snake_case keys)
	function setOptionValue(scraperName: string, optionKey: string, value: any) {
		if (!config?.scrapers) return;
		if (!config.scrapers[scraperName]) config.scrapers[scraperName] = {};
		
		// All options go to top level - no more extra map
		setNestedValue(config.scrapers[scraperName], optionKey, value);
		
		// Trigger reactivity by reassigning the config object with a deep clone
		config = JSON.parse(JSON.stringify(config));
	}

	// Update config from scraper list
	function updateConfigFromScrapers() {
		if (!config) return;
		if (!config.scrapers) config.scrapers = {};

		// Update priority order
		config.scrapers.priority = scrapers.map(s => s.name);

		// Update enabled status
		scrapers.forEach(scraper => {
			if (!config.scrapers[scraper.name]) config.scrapers[scraper.name] = {};
			config.scrapers[scraper.name].enabled = scraper.enabled;
		});
	}

	function moveScraperUp(index: number) {
		if (index === 0) return;
		[scrapers[index], scrapers[index - 1]] = [scrapers[index - 1], scrapers[index]];
		updateConfigFromScrapers();
	}

	function moveScraperDown(index: number) {
		if (index === scrapers.length - 1) return;
		[scrapers[index], scrapers[index + 1]] = [scrapers[index + 1], scrapers[index]];
		updateConfigFromScrapers();
	}

	function toggleScraper(index: number) {
		const scraper = scrapers[index];
		const wasEnabled = scraper.enabled;
		const willBeEnabled = !wasEnabled;

		// If disabling a scraper, check if it's used in any priority lists
		if (wasEnabled && !willBeEnabled) {
			const usageInfo = getScraperUsage(scraper.name);
			if (usageInfo.count > 0) {
				const confirmed = confirm(
					`${scraper.displayName} is currently used in ${usageInfo.count} field(s):\n\n${usageInfo.fields.join(', ')}\n\nDisabling this scraper will remove it from all priority lists. Continue?`
				);
				if (!confirmed) return;

				// Remove scraper from all priority lists
				removeScraperFromPriorities(scraper.name);
			}
		}

		scrapers[index].enabled = willBeEnabled;
		updateConfigFromScrapers();
	}

	function selectAllScrapers() {
		scrapers = scrapers.map((scraper) => ({ ...scraper, enabled: true }));
		updateConfigFromScrapers();
	}

	function clearAllScrapers() {
		// Check if any scraper is in use and ask for confirmation
		let totalUsage = 0;
		const usedScrapers: string[] = [];
		for (const scraper of scrapers) {
			if (scraper.enabled) {
				const usage = getScraperUsage(scraper.name);
				if (usage.count > 0) {
					totalUsage += usage.count;
					usedScrapers.push(scraper.displayName);
				}
			}
		}
		if (totalUsage > 0) {
			const confirmed = confirm(
				`The following scrapers are currently used in priority lists:\n\n${usedScrapers.join(', ')}\n\nDisabling all scrapers will remove them from all priority lists. Continue?`
			);
			if (!confirmed) return;

			// Remove all scrapers from priority lists
			for (const scraper of scrapers) {
				removeScraperFromPriorities(scraper.name);
			}
		}
		scrapers = scrapers.map((scraper) => ({ ...scraper, enabled: false }));
		updateConfigFromScrapers();
	}

	// Get scraper usage count and field names
	function getScraperUsage(scraperName: string): { count: number; fields: string[] } {
		if (!config) return { count: 0, fields: [] };

		const metadataFields = [
			{ key: 'id', label: 'Movie ID' },
			{ key: 'title', label: 'Title' },
			{ key: 'original_title', label: 'Original Title' },
			{ key: 'description', label: 'Description' },
			{ key: 'release_date', label: 'Release Date' },
			{ key: 'runtime', label: 'Runtime' },
			{ key: 'content_id', label: 'Content ID' },
			{ key: 'actress', label: 'Actresses' },
			{ key: 'genre', label: 'Genres' },
			{ key: 'director', label: 'Director' },
			{ key: 'maker', label: 'Studio/Maker' },
			{ key: 'label', label: 'Label' },
			{ key: 'series', label: 'Series' },
			{ key: 'rating', label: 'Rating' },
			{ key: 'cover_url', label: 'Cover Image' },
			{ key: 'poster_url', label: 'Poster Image' },
			{ key: 'screenshot_url', label: 'Screenshots' },
			{ key: 'trailer_url', label: 'Trailer' }
		];

		const globalPriority = config?.scrapers?.priority || [];
		const fieldsUsing: string[] = [];

		metadataFields.forEach((field) => {
			// Check if field has custom priority
			const fieldPriority = config?.metadata?.priority?.[field.key];
			const priority = fieldPriority && fieldPriority.length > 0 ? fieldPriority : globalPriority;

			if (priority.includes(scraperName)) {
				fieldsUsing.push(field.label);
			}
		});

		return { count: fieldsUsing.length, fields: fieldsUsing };
	}

	// Remove scraper from all priority lists
	function removeScraperFromPriorities(scraperName: string) {
		if (!config) return;

		// Remove from global priority
		if (config.scrapers?.priority) {
			config.scrapers.priority = config.scrapers.priority.filter((s: string) => s !== scraperName);
		}

		// Remove from all field-specific priorities
		if (config.metadata?.priority) {
			Object.keys(config.metadata.priority).forEach((fieldKey) => {
				const fieldPriority = config.metadata.priority[fieldKey];
				if (Array.isArray(fieldPriority)) {
					config.metadata.priority[fieldKey] = fieldPriority.filter((s: string) => s !== scraperName);
				}
			});
		}
	}

	async function loadConfig() {
		try {
			const response = await apiClient.getConfig();
			config = response;
			ensureProxyProfilesInitialized();
			ensureTranslationConfig();
			stripLegacyDownloadProxyFields();
			await buildScraperList();
			updateProxyConfigBaseline(); // Set baseline after loading config
			loading = false;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load configuration';
			loading = false;
		}
	}

	async function handleSave() {
		if (!config) return;
		if (saving) return;

		// Check test-before-save requirements
		if (!canSafelySave()) {
			toastStore.error('Test all modified proxy profiles before saving', 5000);
			return;
		}

		// Check for unsaved proxy test results that have expired
		for (const [name, result] of Object.entries(profileTestResults)) {
			if (isTestExpired(result)) {
				toastStore.error(`Test for profile "${name}" has expired. Please test again before saving.`, 5000);
				return;
			}
		}

		saving = true;
		error = null;
		try {
			// Build payload with verification tokens
			const payload = {
				...config,
				proxy_verification_tokens: buildVerificationTokenPayload()
			};

			await apiClient.request('/api/v1/config', {
				method: 'PUT',
				body: JSON.stringify(payload)
			});
			// Clear test results after successful save since they're now saved
			profileTestResults = {};
			globalProxyTestResult = null;
			globalFlareSolverrTestResult = null;
			verificationTokens = {}; // Clear tokens after successful save
			updateProxyConfigBaseline(); // Update baseline after successful save
			toastStore.success('Configuration saved successfully', 4000);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save configuration';
			toastStore.error(error, 5000);
		} finally {
			saving = false;
		}
	}

	async function fetchTranslationModels() {
		const provider = config?.metadata?.translation?.provider;
		const configKey = provider === 'openai-compatible' ? 'openai_compatible' : provider;
		const baseUrl = config?.metadata?.translation?.[configKey]?.base_url;
		const apiKey = config?.metadata?.translation?.[configKey]?.api_key;

		fetchingTranslationModels = true;
		try {
			const data = await apiClient.request<{ models: string[] }>('/api/v1/translation/models', {
				method: 'POST',
				body: JSON.stringify({ provider, base_url: baseUrl, api_key: apiKey })
			});
			translationModelOptions = data.models || [];
		} catch (e) {
			const msg = e instanceof Error ? e.message : 'Failed to fetch models';
			toastStore.error(msg, 5000);
			translationModelOptions = [];
		} finally {
			fetchingTranslationModels = false;
		}
	}

	async function runProxyTest(mode: 'direct' | 'flaresolverr') {
		if (!config?.scrapers?.proxy) {
			toastStore.error('Scraper proxy configuration is missing', 5000);
			return;
		}

		const proxyConfig = config.scrapers.proxy;

		if (mode === 'direct') {
			if (!proxyConfig.enabled) {
				toastStore.error('Enable scraper proxy before testing', 5000);
				return;
			}

			const defaultProfileName = proxyConfig.default_profile;
			const defaultProfile = defaultProfileName ? proxyConfig.profiles?.[defaultProfileName] : null;

			if (!defaultProfile?.url?.trim()) {
				toastStore.error('Set default proxy profile URL before testing', 5000);
				return;
			}

			testingProxy = true;
			try {
				const result = await apiClient.testProxy({
					mode: 'direct',
					proxy: {
						enabled: true,
						profile: defaultProfileName,
						profiles: proxyConfig.profiles
					}
				});

				globalProxyTestResult = {
					success: result.success,
					timestamp: Date.now(),
					message: result.message,
					configSnapshot: JSON.stringify(proxyConfig),
					verificationToken: result.verification_token,
					tokenExpiresAt: result.token_expires_at
				};

				// Store verification token for save
				if (result.verification_token) {
					verificationTokens['global'] = result.verification_token;
				}

				if (result.success) {
					toastStore.success(`Proxy test passed (${result.duration_ms}ms): ${result.message}`, 7000);
				} else {
					toastStore.error(`Proxy test failed (${result.duration_ms}ms): ${result.message}`, 7000);
				}
			} catch (e) {
				globalProxyTestResult = { success: false, timestamp: Date.now() };
				const msg = e instanceof Error ? e.message : 'Proxy test failed';
				toastStore.error(msg, 7000);
			} finally {
				testingProxy = false;
			}
		} else if (mode === 'flaresolverr') {
			if (!config.scrapers.flaresolverr?.enabled) {
				toastStore.error('Enable FlareSolverr before testing', 5000);
				return;
			}
			if (!config.scrapers.flaresolverr?.url?.trim()) {
				toastStore.error('Set FlareSolverr URL before testing', 5000);
				return;
			}

			const proxyForTest = config.scrapers.proxy?.enabled
				? {
						enabled: true,
						profile: config.scrapers.proxy.default_profile || '',
						profiles: config.scrapers.proxy.profiles || {}
				  }
				: { enabled: false };

			testingFlareSolverr = true;
			try {
				const result = await apiClient.testProxy({
					mode: 'flaresolverr',
					target_url: 'https://www.cloudflare.com/cdn-cgi/trace',
					proxy: proxyForTest,
					flaresolverr: {
						enabled: true,
						url: config.scrapers.flaresolverr.url,
						timeout: config.scrapers.flaresolverr.timeout ?? 30,
						max_retries: config.scrapers.flaresolverr.max_retries ?? 3,
						session_ttl: config.scrapers.flaresolverr.session_ttl ?? 300
					}
				});

				globalFlareSolverrTestResult = {
					success: result.success,
					timestamp: Date.now(),
					message: result.message,
					configSnapshot: JSON.stringify(config.scrapers.flaresolverr),
					verificationToken: result.verification_token,
					tokenExpiresAt: result.token_expires_at
				};

				// Store verification token for save
				if (result.verification_token) {
					verificationTokens['flaresolverr'] = result.verification_token;
				}

				if (result.success) {
					toastStore.success(`FlareSolverr test passed (${result.duration_ms}ms): ${result.message}`, 7000);
				} else {
					toastStore.error(`FlareSolverr test failed (${result.duration_ms}ms): ${result.message}`, 7000);
				}
			} catch (e) {
				globalFlareSolverrTestResult = { success: false, timestamp: Date.now() };
				const msg = e instanceof Error ? e.message : 'FlareSolverr test failed';
				toastStore.error(msg, 7000);
			} finally {
				testingFlareSolverr = false;
			}
		}
	}

	onMount(() => {
		loadConfig();
	});
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		<div class="space-y-4">
			<div class="flex items-center gap-3">
				<a href="/browse">
					<Button variant="ghost" size="icon">
						{#snippet children()}
							<ArrowLeft class="h-5 w-5" />
						{/snippet}
					</Button>
				</a>
				<div class="flex-1">
					<h1 class="text-3xl font-bold">Settings</h1>
					<p class="text-muted-foreground mt-1">
						Configure Javinizer scraping and output options
					</p>
				</div>
			</div>
			<div class="flex gap-2">
				<Button variant="outline" onclick={loadConfig} disabled={loading}>
					{#snippet children()}
						<RefreshCw class="h-4 w-4 mr-2" />
						Reload
					{/snippet}
				</Button>
				<Button onclick={handleSave} disabled={saving || loading}>
					{#snippet children()}
						<Save class="h-4 w-4 mr-2" />
						{saving ? 'Saving...' : 'Save Changes'}
					{/snippet}
				</Button>
			</div>
		</div>

		{#if error}
			<div class="bg-destructive/10 border-2 border-destructive text-destructive px-4 py-3 rounded-lg flex items-start gap-2">
				<CircleAlert class="h-5 w-5 mt-0.5 shrink-0" />
				<p>{error}</p>
			</div>
		{/if}

		{#if loading}
			<Card class="p-8 text-center">
				<RefreshCw class="h-8 w-8 animate-spin mx-auto mb-2" />
				<p class="text-muted-foreground">Loading configuration...</p>
			</Card>
		{:else if config}
			<ServerSettingsSection {config} {inputClass} />

			<SettingsSection title="Scraper Defaults" description="Default settings applied to all scrapers unless overridden per-scraper" defaultExpanded={false}>
				<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
					<div>
						<label class="block text-sm font-medium mb-2" for="scrapers-user-agent">Default User-Agent</label>
						<input
							id="scrapers-user-agent"
							type="text"
							value={config.scrapers.user_agent}
							oninput={handleScraperUserAgentInput}
							class={inputClass}
							placeholder="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
						/>
						<p class="text-xs text-muted-foreground mt-1">Custom User-Agent for scraper requests (default browser UA if empty)</p>
					</div>
					<div>
						<label class="block text-sm font-medium mb-2" for="scrapers-referer">Default Referer</label>
						<input
							id="scrapers-referer"
							type="text"
							value={config.scrapers.referer}
							oninput={handleScraperRefererInput}
							class={inputClass}
							placeholder="https://www.dmm.co.jp/"
						/>
					<p class="text-xs text-muted-foreground mt-1">Referer header for CDN compatibility (default: https://www.dmm.co.jp/)</p>
				</div>
			</div>

			<!-- Global scrape_actress toggle -->
			<div class="pt-4 border-t mt-4">
				<FormToggle
					id="global-scrape-actress"
					label="Scrape Actress Information (Global Default)"
					description="Default setting for actress scraping across all scrapers. Individual scrapers can override this in their settings."
					checked={config.scrapers?.scrape_actress ?? true}
					onchange={(val) => {
						if (!config.scrapers) config.scrapers = {};
						config.scrapers.scrape_actress = val;
					}}
				/>
			</div>
		</SettingsSection>

	<BrowserSettingsSection 
		{config} 
		{inputClass} 
		onChange={(path, value) => {
			try {
				setNestedValue(config, path, value);
				config = JSON.parse(JSON.stringify(config));  // Deep clone for proper reactivity
			} catch (err) {
				console.error('Config update failed:', path, err);
				toastStore.error(`Failed to update setting: ${err instanceof Error ? err.message : String(err)}`);
			}
		}}
	/>

			<ScraperSettingsSection
				{config}
				{scrapers}
				{inputClass}
				{scraperHasOptions}
				{onScraperRowClick}
				{onScraperRowKeydown}
				{toggleScraper}
				{toggleExpanded}
				{selectAllScrapers}
				{clearAllScrapers}
				{getScraperUsage}
				{scraperSupportsProxyOptions}
				{getScraperProxyMode}
				{setScraperProxyMode}
				{getProxyProfileNames}
				{setOptionValue}
				{getRenderableScraperOptions}
				{isOptionDisabled}
				{getOptionValue}
				{parseOptionNumber}
			/>

			<SettingsSection title="Metadata Priority" description="Configure which scraper to use for each metadata field" defaultExpanded={false}>
				<MetadataPriority config={config} onUpdate={(updatedConfig) => { config = updatedConfig; }} />
			</SettingsSection>

			<FileOperationsSettingsSection {config} />
			<OutputSettingsSection {config} {inputClass} />
			<DatabaseSettingsSection {config} {inputClass} />
			<GenreReplacementsSection />
			<TranslationSettingsSection
				{config}
				{inputClass}
				{fetchTranslationModels}
				{fetchingTranslationModels}
				{translationModelOptions}
			/>
			<NfoSettingsSection {config} />
			<ProxySettingsSection
				{config}
				{inputClass}
				{testingProxy}
				{testingFlareSolverr}
				{testingProfile}
				{savingProfile}
				{loading}
				{saving}
				{profileTestResults}
				{globalProxyTestResult}
				{globalFlareSolverrTestResult}
				{canSaveProfile}
				{isTestExpired}
				{getProxyProfileNames}
				{addProxyProfile}
				{renameProxyProfile}
				{removeProxyProfile}
				{setProxyProfileField}
				{saveProxyProfile}
				{runNamedProxyProfileTest}
				{runProxyTest}
				{invalidateGlobalProxyTest}
				{invalidateGlobalFlareSolverrTest}
			/>
			<PerformanceSettingsSection {config} {inputClass} />
			<FileMatchingSettingsSection {config} {inputClass} />
			<LoggingSettingsSection {config} {inputClass} />
			<MediaInfoSettingsSection {config} />
		{/if}
	</div>
</div>

<style>
	:global(.sortable-ghost) {
		opacity: 0.4;
		background-color: hsl(var(--primary) / 0.1);
	}

	:global(.sortable-drag) {
		opacity: 0.8;
		background-color: hsl(var(--background));
	}
</style>
