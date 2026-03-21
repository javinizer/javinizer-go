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
				scraperDisplayNames[scraper.name] = scraper.display_name;
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

	type ScraperProxyMode = 'direct' | 'inherit' | 'specific';

	function getScraperProxyMode(scraperName: string): ScraperProxyMode {
		const proxyCfg = config?.scrapers?.[scraperName]?.proxy;
		if (!proxyCfg?.enabled) return 'direct';
		if ((proxyCfg.profile ?? '').trim() !== '') return 'specific';
		return 'inherit';
	}

	function setScraperProxyMode(scraperName: string, mode: ScraperProxyMode): void {
		if (!config?.scrapers) return;
		if (!config.scrapers[scraperName]) config.scrapers[scraperName] = {};
		if (!config.scrapers[scraperName].proxy || typeof config.scrapers[scraperName].proxy !== 'object') {
			config.scrapers[scraperName].proxy = {};
		}

		const proxyCfg = config.scrapers[scraperName].proxy;
		if (mode === 'direct') {
			proxyCfg.enabled = false;
		} else if (mode === 'inherit') {
			proxyCfg.enabled = true;
			proxyCfg.profile = '';
		} else {
			proxyCfg.enabled = true;
			if (!(proxyCfg.profile ?? '').trim()) {
				const defaultProfile = config.scrapers.proxy?.default_profile ?? '';
				const firstProfile = getProxyProfileNames()[0] ?? '';
				proxyCfg.profile = defaultProfile || firstProfile;
			}
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
		return getNestedValue(config?.scrapers?.[scraperName], optionKey);
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
	}

	function setProxyProfileField(name: string, field: 'url' | 'username' | 'password', value: string): void {
		if (!config?.scrapers?.proxy?.profiles?.[name]) return;
		config.scrapers.proxy.profiles[name][field] = value;
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
		if (!profile.url) {
			toastStore.error(`Profile "${profileName}" needs a proxy URL before testing`, 5000);
			return;
		}

		testingProfile[profileName] = true;
		try {
			const result = await apiClient.testProxy({
				mode: 'direct',
				proxy: {
					enabled: true,
					url: profile.url,
					username: profile.username ?? '',
					password: profile.password ?? ''
				}
			});

			if (result.success) {
				toastStore.success(`Profile "${profileName}" test passed (${result.duration_ms}ms): ${result.message}`, 7000);
			} else {
				toastStore.error(`Profile "${profileName}" test failed (${result.duration_ms}ms): ${result.message}`, 7000);
			}
		} catch (e) {
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

	function isZeroFlaresolverrConfig(fs: any): boolean {
		if (!fs || typeof fs !== 'object') return true;
		return !fs.enabled && !fs.url && !fs.timeout && !fs.max_retries && !fs.session_ttl;
	}

	function resolveGlobalProxyForTesting() {
		if (!config?.scrapers?.proxy) return null;
		const proxyConfig = JSON.parse(JSON.stringify(config.scrapers.proxy));
		const profiles = proxyConfig.profiles ?? {};
		const defaultProfile = proxyConfig.default_profile;
		if (defaultProfile && profiles[defaultProfile]) {
			const profile = profiles[defaultProfile];
			if (profile.url) proxyConfig.url = profile.url;
			if (profile.username !== undefined) proxyConfig.username = profile.username;
			if (profile.password !== undefined) proxyConfig.password = profile.password;
			if (!isZeroFlaresolverrConfig(profile.flaresolverr)) {
				proxyConfig.flaresolverr = profile.flaresolverr;
			}
		}
		return proxyConfig;
	}

	function isOptionDisabled(scraperName: string, optionKey: string): boolean {
		const globalProxyEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const globalFlareSolverrEnabled = config?.scrapers?.proxy?.flaresolverr?.enabled ?? false;
		const scraperCfg = config?.scrapers?.[scraperName] ?? {};

		if (optionKey === 'use_flaresolverr') {
			return !globalProxyEnabled || !globalFlareSolverrEnabled;
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

		if (optionKey === 'fake_user_agent') {
			return !(scraperCfg?.use_fake_user_agent ?? false);
		}

		return false;
	}

	// Helper to set option value in config (using snake_case keys)
	function setOptionValue(scraperName: string, optionKey: string, value: any) {
		if (!config?.scrapers) return;
		if (!config.scrapers[scraperName]) config.scrapers[scraperName] = {};
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
				config.metadata.priority[fieldKey] = config.metadata.priority[fieldKey].filter(
					(s: string) => s !== scraperName
				);

				// Clean up empty arrays
				if (config.metadata.priority[fieldKey].length === 0) {
					delete config.metadata.priority[fieldKey];
				}
			});
		}

		// Trigger reactivity
		config = { ...config };
	}

	onMount(async () => {
		await loadConfig();
	});

	async function loadConfig() {
		loading = true;
		error = null;
		try {
			config = await apiClient.request('/api/v1/config');
			ensureProxyProfilesInitialized();
			ensureTranslationConfig();
			stripLegacyDownloadProxyFields();
			buildScraperList();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load configuration';
		} finally {
			loading = false;
		}
	}

	function promptSaveConfig() {
		showConfirmModal = true;
	}

	async function confirmSaveConfig() {
		showConfirmModal = false;
		saving = true;
		error = null;
		try {
			stripLegacyDownloadProxyFields();
			await apiClient.request('/api/v1/config', {
				method: 'PUT',
				body: JSON.stringify(config)
			});
			toastStore.success('Configuration saved successfully!', 5000);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save configuration';
			toastStore.error(error, 5000);
		} finally {
			saving = false;
		}
	}

	function cancelSave() {
		showConfirmModal = false;
	}

	async function fetchTranslationModels(): Promise<void> {
		const baseURL = config?.metadata?.translation?.openai?.base_url?.trim?.() ?? '';
		const apiKey = config?.metadata?.translation?.openai?.api_key?.trim?.() ?? '';
		if (!baseURL) {
			toastStore.error('Set OpenAI-compatible base URL before fetching models', 5000);
			return;
		}
		if (!apiKey) {
			toastStore.error('Set API key before fetching models', 5000);
			return;
		}

		fetchingTranslationModels = true;
		try {
			const response = await apiClient.getTranslationModels({
				provider: 'openai',
				base_url: baseURL,
				api_key: apiKey
			});
			translationModelOptions = response.models || [];
			if (translationModelOptions.length > 0) {
				const current = config.metadata.translation.openai.model?.trim?.() ?? '';
				if (!current || !translationModelOptions.includes(current)) {
					config.metadata.translation.openai.model = translationModelOptions[0];
				}
				toastStore.success(`Loaded ${translationModelOptions.length} model(s)`, 4000);
			} else {
				toastStore.error('No models returned from provider', 5000);
			}
		} catch (e) {
			const msg = e instanceof Error ? e.message : 'Failed to fetch models';
			toastStore.error(msg, 5000);
		} finally {
			fetchingTranslationModels = false;
		}
	}

	async function runProxyTest(mode: 'direct' | 'flaresolverr') {
		if (!config?.scrapers?.proxy) {
			toastStore.error('Scraper proxy configuration is missing', 5000);
			return;
		}

		const proxyConfig = resolveGlobalProxyForTesting();
		if (!proxyConfig) {
			toastStore.error('Scraper proxy configuration is missing', 5000);
			return;
		}

		if (mode === 'direct' && (!proxyConfig.enabled || !proxyConfig.url)) {
			toastStore.error('Enable scraper proxy and set proxy URL before testing', 5000);
			return;
		}
		if (mode === 'flaresolverr' && (!proxyConfig.flaresolverr?.enabled || !proxyConfig.flaresolverr?.url)) {
			toastStore.error('Enable FlareSolverr and set FlareSolverr URL before testing', 5000);
			return;
		}

		if (mode === 'direct') {
			testingProxy = true;
		} else {
			testingFlareSolverr = true;
		}

		try {
			const result = await apiClient.testProxy({
				mode,
				proxy: proxyConfig
			});

			if (result.success) {
				toastStore.success(`${mode === 'direct' ? 'Proxy' : 'FlareSolverr'} test passed (${result.duration_ms}ms): ${result.message}`, 7000);
			} else {
				toastStore.error(`${mode === 'direct' ? 'Proxy' : 'FlareSolverr'} test failed (${result.duration_ms}ms): ${result.message}`, 7000);
			}
		} catch (e) {
			const msg = e instanceof Error ? e.message : 'Proxy test failed';
			toastStore.error(msg, 7000);
		} finally {
			if (mode === 'direct') {
				testingProxy = false;
			} else {
				testingFlareSolverr = false;
			}
		}
	}
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		<!-- Header -->
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
				<Button onclick={promptSaveConfig} disabled={saving || loading}>
					{#snippet children()}
						<Save class="h-4 w-4 mr-2" />
						{saving ? 'Saving...' : 'Save Changes'}
					{/snippet}
				</Button>
			</div>
		</div>


		<!-- Error Message -->
		{#if error}
			<div
				class="bg-destructive/10 border-2 border-destructive text-destructive px-4 py-3 rounded-lg flex items-start gap-2"
			>
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
			<ServerSettingsSection config={config} {inputClass} />

			<ScraperSettingsSection
				{config}
				{scrapers}
				{inputClass}
				{scraperHasOptions}
				{onScraperRowClick}
				{onScraperRowKeydown}
				{toggleScraper}
				{toggleExpanded}
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
				{getProxyProfileNames}
				{addProxyProfile}
				{renameProxyProfile}
				{removeProxyProfile}
				{setProxyProfileField}
				{saveProxyProfile}
				{runNamedProxyProfileTest}
				{runProxyTest}
			/>
			<PerformanceSettingsSection {config} {inputClass} />
			<FileMatchingSettingsSection {config} {inputClass} />
			<LoggingSettingsSection {config} {inputClass} />
			<MediaInfoSettingsSection {config} />
		{/if}
	</div>
</div>

<!-- Confirmation Modal -->
{#if showConfirmModal}
	<div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4 animate-fade-in" use:portalToBody>
		<Card class="w-full max-w-md animate-scale-in">
			<div class="p-6 space-y-4">
				<!-- Header -->
				<div class="flex items-start justify-between">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 bg-primary/10 rounded-full flex items-center justify-center">
							<CircleAlert class="h-5 w-5 text-primary" />
						</div>
						<div>
							<h3 class="text-lg font-semibold">Save Configuration?</h3>
							<p class="text-sm text-muted-foreground mt-1">
								This will overwrite your config.yaml file
							</p>
						</div>
					</div>
					<Button variant="ghost" size="icon" onclick={cancelSave}>
						{#snippet children()}
							<X class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>

				<!-- Content -->
				<div class="bg-accent/50 rounded-lg p-4 space-y-2">
					<p class="text-sm font-medium">Changes will be written to:</p>
					<p class="text-xs font-mono bg-background px-2 py-1 rounded">
						configs/config.yaml
					</p>
					<p class="text-xs text-muted-foreground mt-2">
						Make sure you have a backup if needed. The server may need to restart for some changes to take effect.
					</p>
				</div>

				<!-- Actions -->
				<div class="flex items-center gap-3 justify-end">
					<Button variant="outline" onclick={cancelSave} disabled={saving}>
						{#snippet children()}
							Cancel
						{/snippet}
					</Button>
					<Button onclick={confirmSaveConfig} disabled={saving}>
						{#snippet children()}
							<Save class="h-4 w-4 mr-2" />
							{saving ? 'Saving...' : 'Save Configuration'}
						{/snippet}
					</Button>
				</div>
			</div>
		</Card>
	</div>
{/if}

<style>
	@keyframes fade-in {
		from {
			opacity: 0;
		}
		to {
			opacity: 1;
		}
	}

	@keyframes scale-in {
		from {
			transform: scale(0.95);
			opacity: 0;
		}
		to {
			transform: scale(1);
			opacity: 1;
		}
	}

	.animate-fade-in {
		animation: fade-in 0.2s ease-out;
	}

	:global(.animate-scale-in) {
		animation: scale-in 0.3s ease-out;
	}
</style>
