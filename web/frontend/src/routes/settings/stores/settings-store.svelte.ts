import { createMutation, useQueryClient } from '@tanstack/svelte-query';
import { untrack } from 'svelte';
import { apiClient } from '$lib/api/client';
import { createConfigQuery } from '$lib/query/queries';
import { toastStore } from '$lib/stores/toast';
import { isProxyConfigDirty, isTestValid, type TestResult } from '$lib/proxy/proxy-logic';
import type {
	Config,
	SettingsConfig,
	ScraperSettings,
	ProxyConfig,
	OpenAICompatibleTranslationConfig,
	AnthropicTranslationConfig,
	TranslationConfig
} from '$lib/api/types';

export interface SettingsStore {
	config: Config | null;
	settingsConfig: SettingsConfig | null;
	configInitialized: boolean;
	loading: boolean;
	error: string | null;
	inputClass: string;
	configQuery: ReturnType<typeof createConfigQuery>;
	reloadConfig: () => Promise<void>;
	handleSave: () => void;
	saveConfigMutation: { isPending: boolean; mutate: () => void };
	fetchTranslationModels: () => Promise<void>;
	fetchingTranslationModels: boolean;
	translationModelOptions: string[];
	ensureProxyProfilesInitialized: () => void;
	ensureTranslationConfig: () => void;
	updateProxyConfigBaseline: () => void;
	checkProxyConfigDirty: () => boolean;
	canSafelySave: () => boolean;
	hasUnsavedProxyChanges: () => boolean;
	buildVerificationTokenPayload: () => Record<string, string>;
	proxyConfigBaseline: string;
	flaresolverrConfigBaseline: string;
}

interface SettingsStoreDeps {
	getProfileTestResults: () => Record<string, TestResult>;
	getGlobalProxyTestResult: () => TestResult | null;
	getGlobalFlareSolverrTestResult: () => TestResult | null;
	getVerificationTokens: () => Record<string, string>;
	clearTestResults: () => void;
	invalidateGlobalProxyTest: () => void;
	invalidateGlobalFlareSolverrTest: () => void;
	onConfigInitialized: () => void;
}

export function createSettingsStore(deps: SettingsStoreDeps): SettingsStore {
	let config = $state<Config | null>(null);
	let settingsConfig = $derived(config as SettingsConfig | null);
	let configInitialized = $state(false);
	const queryClient = useQueryClient();
	const configQuery = createConfigQuery();
	let loading = $derived(configQuery.isPending && !configQuery.data);
	let error = $state<string | null>(null);
	let fetchingTranslationModels = $state(false);
	let translationModelOptions = $state<string[]>([]);
	let proxyConfigBaseline = $state<string>('');
	let flaresolverrConfigBaseline = $state<string>('');

	const inputClass =
		'w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background';

	const TEST_VALIDITY_MS = 5 * 60 * 1000;

	function updateProxyConfigBaseline(): void {
		proxyConfigBaseline = JSON.stringify(config?.scrapers?.proxy);
		flaresolverrConfigBaseline = JSON.stringify(config?.scrapers?.flaresolverr);
	}

	function checkProxyConfigDirty(): boolean {
		const currentProxy = config?.scrapers?.proxy;
		const currentFlaresolverr = config?.scrapers?.flaresolverr;
		return (
			isProxyConfigDirty(currentProxy, proxyConfigBaseline) ||
			isProxyConfigDirty(currentFlaresolverr, flaresolverrConfigBaseline)
		);
	}

	function hasUnsavedProxyChanges(): boolean {
		return (
			Object.keys(deps.getProfileTestResults()).length > 0 ||
			deps.getGlobalProxyTestResult() !== null ||
			deps.getGlobalFlareSolverrTestResult() !== null
		);
	}

	function canSafelySave(): boolean {
		if (!checkProxyConfigDirty()) return true;

		const globalProxyEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const flaresolverrEnabled = config?.scrapers?.flaresolverr?.enabled ?? false;

		if (globalProxyEnabled && !isTestValid(deps.getGlobalProxyTestResult(), config?.scrapers?.proxy, TEST_VALIDITY_MS))
			return false;
		if (flaresolverrEnabled && !isTestValid(deps.getGlobalFlareSolverrTestResult(), config?.scrapers?.flaresolverr, TEST_VALIDITY_MS))
			return false;

		for (const name of Object.keys(deps.getProfileTestResults())) {
			const result = deps.getProfileTestResults()[name];
			const currentProfile = config?.scrapers?.proxy?.profiles?.[name];
			if (!isTestValid(result, currentProfile, TEST_VALIDITY_MS)) return false;
		}

		return true;
	}

	function buildVerificationTokenPayload(): Record<string, string> {
		const tokens: Record<string, string> = {};
		const vt = deps.getVerificationTokens();
		if (vt['global']) tokens['global'] = vt['global'];
		if (vt['flaresolverr']) tokens['flaresolverr'] = vt['flaresolverr'];
		return tokens;
	}

	function ensureProxyProfilesInitialized(): void {
		if (!config) return;
		const cfg = config;
		if (!cfg.scrapers) cfg.scrapers = {};
		if (!cfg.scrapers.proxy) cfg.scrapers.proxy = { enabled: false } as ProxyConfig;
		if (
			!cfg.scrapers.proxy.profiles ||
			typeof cfg.scrapers.proxy.profiles !== 'object' ||
			Array.isArray(cfg.scrapers.proxy.profiles)
		) {
			cfg.scrapers.proxy.profiles = {};
		}

		const profiles = cfg.scrapers.proxy.profiles;
		if (Object.keys(profiles).length === 0) {
			profiles.main = {
				url: cfg.scrapers.proxy?.default_profile ?? '',
				username: '',
				password: ''
			};
		}

		const defaultProfile = cfg.scrapers.proxy.default_profile;
		if (!defaultProfile || !profiles[defaultProfile]) {
			const names = Object.keys(profiles).sort();
			cfg.scrapers.proxy.default_profile = names.includes('main') ? 'main' : (names[0] ?? '');
		}
	}

	function ensureTranslationConfig(): void {
		if (!config) return;
		const cfg = config;
		if (!cfg.metadata) cfg.metadata = {};
		if (!cfg.metadata.translation || typeof cfg.metadata.translation !== 'object') {
			cfg.metadata.translation = {};
		}

		const translation = cfg.metadata.translation;
		if (translation.enabled === undefined) translation.enabled = false;
		if (!translation.provider) translation.provider = 'openai';
		if (!translation.source_language) translation.source_language = 'en';
		if (!translation.target_language) translation.target_language = 'ja';
		if (!translation.timeout_seconds) translation.timeout_seconds = 60;
		if (translation.apply_to_primary === undefined) translation.apply_to_primary = true;
		if (translation.overwrite_existing_target === undefined)
			translation.overwrite_existing_target = true;

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

		if (!translation.openai_compatible || typeof translation.openai_compatible !== 'object')
			translation.openai_compatible = {} as OpenAICompatibleTranslationConfig;
		if (!translation.openai_compatible.base_url)
			translation.openai_compatible.base_url = 'http://localhost:11434/v1';
		if (!translation.openai_compatible.model) translation.openai_compatible.model = '';
		if (!translation.openai_compatible.api_key) translation.openai_compatible.api_key = '';

		if (!translation.anthropic || typeof translation.anthropic !== 'object')
			translation.anthropic = {} as AnthropicTranslationConfig;
		if (!translation.anthropic.base_url) translation.anthropic.base_url = 'https://api.anthropic.com';
		if (!translation.anthropic.model) translation.anthropic.model = '';
		if (!translation.anthropic.api_key) translation.anthropic.api_key = '';
	}

	$effect(() => {
		const data = configQuery.data;
		if (data && !configInitialized) {
			untrack(() => {
				configInitialized = true;
				config = JSON.parse(JSON.stringify(data));
				ensureProxyProfilesInitialized();
				ensureTranslationConfig();
				deps.onConfigInitialized();
				updateProxyConfigBaseline();
			});
		}
	});

	async function reloadConfig() {
		configInitialized = false;
		await queryClient.refetchQueries({ queryKey: ['config'] });
	}

	const saveConfigMutation = createMutation(() => ({
		mutationFn: async () => {
			if (!canSafelySave()) {
				throw new Error('Test all modified proxy profiles before saving');
			}
			const payload = {
				...config,
				proxy_verification_tokens: buildVerificationTokenPayload()
			};
			await apiClient.request('/api/v1/config', {
				method: 'PUT',
				body: JSON.stringify(payload)
			});
		},
		onSuccess: () => {
			deps.clearTestResults();
			updateProxyConfigBaseline();
			toastStore.success('Configuration saved successfully', 4000);
			void queryClient.invalidateQueries({ queryKey: ['config'] });
		},
		onError: (err: Error) => {
			error = err.message;
			toastStore.error(err.message, 5000);
		}
	}));

	function handleSave() {
		if (!config) return;
		saveConfigMutation.mutate();
	}

	async function fetchTranslationModels() {
		const provider = config?.metadata?.translation?.provider;
		const configKey = provider === 'openai-compatible' ? 'openai_compatible' : provider;
		const translationSection = configKey
			? config?.metadata?.translation?.[configKey as keyof TranslationConfig]
			: undefined;
		const baseUrl = (translationSection as Record<string, string> | undefined)?.base_url;
		const apiKey = (translationSection as Record<string, string> | undefined)?.api_key;

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

	return {
		get config() { return config; },
		set config(v) { config = v; },
		get settingsConfig() { return settingsConfig; },
		get configInitialized() { return configInitialized; },
		set configInitialized(v) { configInitialized = v; },
		get loading() { return loading; },
		get error() { return error; },
		set error(v) { error = v; },
		inputClass,
		get configQuery() { return configQuery; },
		reloadConfig,
		handleSave,
		get saveConfigMutation() { return saveConfigMutation; },
		fetchTranslationModels,
		get fetchingTranslationModels() { return fetchingTranslationModels; },
		get translationModelOptions() { return translationModelOptions; },
		ensureProxyProfilesInitialized,
		ensureTranslationConfig,
		updateProxyConfigBaseline,
		checkProxyConfigDirty,
		canSafelySave,
		hasUnsavedProxyChanges,
		buildVerificationTokenPayload,
		get proxyConfigBaseline() { return proxyConfigBaseline; },
		get flaresolverrConfigBaseline() { return flaresolverrConfigBaseline; }
	};
}
