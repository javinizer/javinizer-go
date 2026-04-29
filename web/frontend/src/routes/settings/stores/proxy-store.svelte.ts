import { apiClient } from '$lib/api/client';
import { toastStore } from '$lib/stores/toast';
import { isTestValid, type TestResult, type ScraperProxyMode } from '$lib/proxy/proxy-logic';
import type { Config, ScraperSettings, ProxyConfig } from '$lib/api/types';
import { type ScraperItem } from './scraper-store.svelte';

export interface ProxyStore {
	testingProxy: boolean;
	testingFlareSolverr: boolean;
	testingProfile: Record<string, boolean>;
	savingProfile: Record<string, boolean>;
	profileTestResults: Record<string, TestResult>;
	globalProxyTestResult: TestResult | null;
	globalFlareSolverrTestResult: TestResult | null;
	verificationTokens: Record<string, string>;
	canSaveProfile: (profileName: string) => boolean;
	canSaveGlobalProxy: () => boolean;
	canSaveGlobalFlareSolverr: () => boolean;
	isTestExpired: (result: TestResult | null | undefined) => boolean;
	hasPendingProxyTests: () => boolean;
	invalidateProfileTest: (profileName: string) => void;
	invalidateGlobalProxyTest: () => void;
	invalidateGlobalFlareSolverrTest: () => void;
	getProxyProfileNames: () => string[];
	proxyProfileChoices: () => { value: string; label: string }[];
	refreshLocalProxyProfileChoices: (scrapers: ScraperItem[]) => ScraperItem[];
	addProxyProfile: () => void;
	removeProxyProfile: (name: string) => void;
	renameProxyProfile: (oldName: string, rawNewName: string) => void;
	setProxyProfileField: (name: string, field: 'url' | 'username' | 'password', value: string) => void;
	saveProxyProfile: (profileName: string) => Promise<void>;
	runNamedProxyProfileTest: (profileName: string) => Promise<void>;
	runProxyTest: (mode: 'direct' | 'flaresolverr') => Promise<void>;
	updateScraperProfileRefs: (oldName: string, newName: string) => void;
	clearTestResults: () => void;
}

interface ProxyStoreDeps {
	getConfig: () => Config | null;
	setConfig: (config: Config | null) => void;
	getError: () => string | null;
	setError: (error: string | null) => void;
	getScrapers: () => ScraperItem[];
	setScrapers: (scrapers: ScraperItem[]) => void;
	getScraperConfigNames: () => string[];
	ensureProxyProfilesInitialized: () => void;
}

const TEST_VALIDITY_MS = 5 * 60 * 1000;

export function createProxyStore(deps: ProxyStoreDeps): ProxyStore {
	let testingProxy = $state(false);
	let testingFlareSolverr = $state(false);
	let testingProfile = $state<Record<string, boolean>>({});
	let savingProfile = $state<Record<string, boolean>>({});
	let profileTestResults = $state<Record<string, TestResult>>({});
	let globalProxyTestResult = $state<TestResult | null>(null);
	let globalFlareSolverrTestResult = $state<TestResult | null>(null);
	let verificationTokens = $state<Record<string, string>>({});

	function canSaveProfile(profileName: string): boolean {
		const result = profileTestResults[profileName];
		const currentProfile = deps.getConfig()?.scrapers?.proxy?.profiles?.[profileName];
		return isTestValid(result, currentProfile, TEST_VALIDITY_MS);
	}

	function canSaveGlobalProxy(): boolean {
		const currentProxy = deps.getConfig()?.scrapers?.proxy;
		return isTestValid(globalProxyTestResult, currentProxy, TEST_VALIDITY_MS);
	}

	function canSaveGlobalFlareSolverr(): boolean {
		const currentFlaresolverr = deps.getConfig()?.scrapers?.flaresolverr;
		return isTestValid(globalFlareSolverrTestResult, currentFlaresolverr, TEST_VALIDITY_MS);
	}

	function isTestExpired(result: TestResult | null | undefined): boolean {
		return !isTestValid(result, undefined, TEST_VALIDITY_MS);
	}

	function hasPendingProxyTests(): boolean {
		const config = deps.getConfig();
		const globalProxyEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const flaresolverrEnabled = config?.scrapers?.flaresolverr?.enabled ?? false;

		if (!globalProxyEnabled && !flaresolverrEnabled && Object.keys(profileTestResults).length === 0) {
			return false;
		}

		if (globalProxyEnabled && !canSaveGlobalProxy()) return true;
		if (flaresolverrEnabled && !canSaveGlobalFlareSolverr()) return true;
		for (const name of Object.keys(profileTestResults)) {
			if (!canSaveProfile(name)) return true;
		}

		return false;
	}

	function invalidateProfileTest(profileName: string): void {
		if (profileTestResults[profileName]) {
			const next = { ...profileTestResults };
			delete next[profileName];
			profileTestResults = next;
		}
	}

	function invalidateGlobalProxyTest(): void {
		globalProxyTestResult = null;
		const next = { ...verificationTokens };
		delete next['global'];
		verificationTokens = next;
	}

	function invalidateGlobalFlareSolverrTest(): void {
		globalFlareSolverrTestResult = null;
		const next = { ...verificationTokens };
		delete next['flaresolverr'];
		verificationTokens = next;
	}

	function getProxyProfileNames(): string[] {
		if (!deps.getConfig()?.scrapers?.proxy?.profiles) return [];
		return Object.keys(deps.getConfig()!.scrapers!.proxy!.profiles!).sort();
	}

	function proxyProfileChoices() {
		return [
			{ value: '', label: 'Inherit Default' },
			...getProxyProfileNames().map((name) => ({ value: name, label: name }))
		];
	}

	function refreshLocalProxyProfileChoices(scrapers: ScraperItem[]): ScraperItem[] {
		const choices = proxyProfileChoices();
		return scrapers.map((scraper) => ({
			...scraper,
			options: (scraper.options || []).map((option) => {
				if (option.key === 'proxy.profile') {
					return { ...option, choices };
				}
				return option;
			})
		}));
	}

	function updateScraperProfileRefs(oldName: string, newName: string): void {
		const config = deps.getConfig();
		if (!config?.scrapers) return;
		const sc = config.scrapers;
		deps.getScraperConfigNames().forEach((scraperName: string) => {
			const scraperCfg = sc[scraperName] as ScraperSettings | undefined;
			if (scraperCfg?.proxy?.profile === oldName) scraperCfg.proxy.profile = newName;
		});
	}

	function renameProxyProfile(oldName: string, rawNewName: string): void {
		const config = deps.getConfig();
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
		deps.setScrapers(refreshLocalProxyProfileChoices(deps.getScrapers()));

		if (profileTestResults[oldName]) {
			const nextResults = { ...profileTestResults };
			nextResults[newName] = {
				...nextResults[oldName],
				configSnapshot: JSON.stringify(profileData)
			};
			delete nextResults[oldName];
			profileTestResults = nextResults;
		}

		invalidateGlobalProxyTest();
	}

	function addProxyProfile(): void {
		const config = deps.getConfig();
		if (!config) return;
		deps.ensureProxyProfilesInitialized();
		const sc = config.scrapers;
		if (!sc) return;
		let idx = 1;
		let name = `profile-${idx}`;
		while (sc.proxy?.profiles?.[name]) {
			idx += 1;
			name = `profile-${idx}`;
		}

		sc.proxy!.profiles![name] = {
			url: '',
			username: '',
			password: ''
		};

		if (!sc.proxy!.default_profile) {
			sc.proxy!.default_profile = name;
		}
		sc.proxy!.profiles = { ...sc.proxy!.profiles };
		deps.setScrapers(refreshLocalProxyProfileChoices(deps.getScrapers()));
		invalidateGlobalProxyTest();
	}

	function removeProxyProfile(name: string): void {
		const config = deps.getConfig();
		if (!config?.scrapers?.proxy?.profiles?.[name]) return;
		delete config.scrapers.proxy.profiles[name];
		updateScraperProfileRefs(name, '');

		const names = getProxyProfileNames();
		if (config.scrapers.proxy.default_profile === name) {
			config.scrapers.proxy.default_profile = names[0] ?? '';
		}
		config.scrapers.proxy.profiles = { ...config.scrapers.proxy.profiles };
		deps.setScrapers(refreshLocalProxyProfileChoices(deps.getScrapers()));

		if (profileTestResults[name]) {
			const next = { ...profileTestResults };
			delete next[name];
			profileTestResults = next;
		}

		invalidateGlobalProxyTest();
	}

	function setProxyProfileField(
		name: string,
		field: 'url' | 'username' | 'password',
		value: string
	): void {
		const config = deps.getConfig();
		if (!config?.scrapers?.proxy?.profiles?.[name]) return;
		config.scrapers.proxy.profiles[name][field] = value;
		invalidateProfileTest(name);
		invalidateGlobalProxyTest();
	}

	async function saveProxyProfile(profileName: string): Promise<void> {
		const config = deps.getConfig();
		if (!config?.scrapers?.proxy?.profiles?.[profileName]) return;
		if (savingProfile[profileName]) return;

		savingProfile[profileName] = true;
		deps.setError(null);
		config.scrapers.proxy.profiles = { ...config.scrapers.proxy.profiles };
		deps.setScrapers(refreshLocalProxyProfileChoices(deps.getScrapers()));
		try {
			await apiClient.request('/api/v1/config', {
				method: 'PUT',
				body: JSON.stringify(config)
			});
			toastStore.success(`Profile "${profileName}" saved successfully.`, 4000);
		} catch (e) {
			const errMsg = e instanceof Error ? e.message : 'Failed to save profile';
			deps.setError(errMsg);
			toastStore.error(errMsg, 5000);
		} finally {
			savingProfile[profileName] = false;
		}
	}

	async function runNamedProxyProfileTest(profileName: string) {
		const config = deps.getConfig();
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
			const shouldAlsoValidateGlobalProxy =
				(config?.scrapers?.proxy?.enabled ?? false) && profileName === defaultProfileName;

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

			profileTestResults[profileName] = {
				success: result.success,
				timestamp: Date.now(),
				message: result.message,
				configSnapshot: JSON.stringify(profile)
			};

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
					const next = { ...verificationTokens };
					delete next['global'];
					verificationTokens = next;
				}
			} else if (
				result.success &&
				result.verification_token &&
				(config?.scrapers?.proxy?.enabled ?? false)
			) {
				verificationTokens['global'] = result.verification_token;
			}

			if (result.success) {
				toastStore.success(
					`Profile "${profileName}" test passed (${result.duration_ms}ms): ${result.message}`,
					7000
				);
			} else {
				toastStore.error(
					`Profile "${profileName}" test failed (${result.duration_ms}ms): ${result.message}`,
					7000
				);
			}
		} catch (e) {
			profileTestResults[profileName] = { success: false, timestamp: Date.now() };
			const msg = e instanceof Error ? e.message : 'Profile proxy test failed';
			toastStore.error(msg, 7000);
		} finally {
			testingProfile[profileName] = false;
		}
	}

	async function runProxyTest(mode: 'direct' | 'flaresolverr') {
		const config = deps.getConfig();
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
			const defaultProfile = defaultProfileName
				? proxyConfig.profiles?.[defaultProfileName]
				: null;

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

				if (result.verification_token) {
					verificationTokens['global'] = result.verification_token;
				}

				if (result.success) {
					toastStore.success(
						`Proxy test passed (${result.duration_ms}ms): ${result.message}`,
						7000
					);
				} else {
					toastStore.error(
						`Proxy test failed (${result.duration_ms}ms): ${result.message}`,
						7000
					);
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

				if (result.verification_token) {
					verificationTokens['flaresolverr'] = result.verification_token;
				}

				if (result.success) {
					toastStore.success(
						`FlareSolverr test passed (${result.duration_ms}ms): ${result.message}`,
						7000
					);
				} else {
					toastStore.error(
						`FlareSolverr test failed (${result.duration_ms}ms): ${result.message}`,
						7000
					);
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

	function clearTestResults(): void {
		profileTestResults = {};
		globalProxyTestResult = null;
		globalFlareSolverrTestResult = null;
		verificationTokens = {};
	}

	return {
		get testingProxy() { return testingProxy; },
		get testingFlareSolverr() { return testingFlareSolverr; },
		get testingProfile() { return testingProfile; },
		get savingProfile() { return savingProfile; },
		get profileTestResults() { return profileTestResults; },
		get globalProxyTestResult() { return globalProxyTestResult; },
		get globalFlareSolverrTestResult() { return globalFlareSolverrTestResult; },
		get verificationTokens() { return verificationTokens; },
		canSaveProfile,
		canSaveGlobalProxy,
		canSaveGlobalFlareSolverr,
		isTestExpired,
		hasPendingProxyTests,
		invalidateProfileTest,
		invalidateGlobalProxyTest,
		invalidateGlobalFlareSolverrTest,
		getProxyProfileNames,
		proxyProfileChoices,
		refreshLocalProxyProfileChoices,
		addProxyProfile,
		removeProxyProfile,
		renameProxyProfile,
		setProxyProfileField,
		saveProxyProfile,
		runNamedProxyProfileTest,
		runProxyTest,
		updateScraperProfileRefs,
		clearTestResults
	};
}
