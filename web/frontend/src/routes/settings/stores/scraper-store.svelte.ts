import { apiClient } from '$lib/api/client';
import { toastStore } from '$lib/stores/toast';
import { confirmDialog } from '$lib/stores/dialog.svelte';
import {
	getScraperProxyMode as getScraperProxyModePure,
	type ScraperProxyMode
} from '$lib/proxy/proxy-logic';
import type { Config, ScraperSettings, ScraperOption, ProxyConfig } from '$lib/api/types';

export interface ScraperItem {
	name: string;
	enabled: boolean;
	displayName: string;
	expanded: boolean;
	options: ScraperOption[];
}

export interface ScraperStore {
	scrapers: ScraperItem[];
	buildScraperList: () => Promise<void>;
	scraperHasOptions: (scraper: ScraperItem) => boolean;
	scraperSupportsProxyOptions: (scraper: ScraperItem) => boolean;
	getRenderableScraperOptions: (scraper: ScraperItem) => ScraperOption[];
	getScraperConfigNames: () => string[];
	stripLegacyDownloadProxyFields: () => void;
	getScraperProxyMode: (scraperName: string) => ScraperProxyMode;
	setScraperProxyMode: (scraperName: string, mode: ScraperProxyMode) => void;
	toggleExpanded: (index: number) => void;
	toggleScraperRow: (index: number) => void;
	onScraperRowKeydown: (event: KeyboardEvent, index: number) => void;
	isInteractiveRowTarget: (target: EventTarget | null) => boolean;
	onScraperRowClick: (event: MouseEvent, index: number) => void;
	getOptionValue: (scraperName: string, optionKey: string) => string | number | boolean | undefined;
	setOptionValue: (scraperName: string, optionKey: string, value: string | number | boolean) => void;
	getNestedValue: (obj: Record<string, unknown> | undefined, path: string) => unknown;
	setNestedValue: (obj: Record<string, unknown>, path: string, value: unknown) => void;
	parseOptionNumber: (value: string) => number | undefined;
	sanitizeHeaderValue: (value: string) => string;
	handleScraperUserAgentInput: (e: Event) => void;
	handleScraperRefererInput: (e: Event) => void;
	updateConfigFromScrapers: () => void;
	moveScraperUp: (index: number) => void;
	moveScraperDown: (index: number) => void;
	toggleScraper: (index: number) => void;
	selectAllScrapers: () => void;
	clearAllScrapers: () => void;
	getScraperUsage: (scraperName: string) => { count: number; fields: string[] };
	removeScraperFromPriorities: (scraperName: string) => void;
	isOptionDisabled: (scraperName: string, optionKey: string) => boolean;
}

interface ScraperStoreDeps {
	getConfig: () => Config | null;
	setConfig: (config: Config | null) => void;
	getProxyProfileNames: () => string[];
	refreshLocalProxyProfileChoices: (scrapers: ScraperItem[]) => ScraperItem[];
}

export function createScraperStore(deps: ScraperStoreDeps): ScraperStore {
	let scrapers = $state<ScraperItem[]>([]);

	async function buildScraperList() {
		const config = deps.getConfig();
		if (!config) return;
		const cfg = config;
		if (!cfg.scrapers) cfg.scrapers = {};
		const sc = cfg.scrapers;
		if (!Array.isArray(sc.priority)) sc.priority = [];

		try {
			const response = await apiClient.getAvailableScrapers();

			const scraperDisplayNames: Record<string, string> = {};
			const scraperOptionsMap: Record<string, ScraperOption[]> = {};
			const scraperEnabledMap: Record<string, boolean> = {};

			response.scrapers.forEach((scraper) => {
				scraperDisplayNames[scraper.name] = scraper.display_title;
				scraperOptionsMap[scraper.name] = scraper.options || [];
				scraperEnabledMap[scraper.name] = scraper.enabled;
			});

			const mergedOrder: string[] = [];
			const seen = new Set<string>();

			(sc.priority || []).forEach((name: string) => {
				if (!seen.has(name)) {
					mergedOrder.push(name);
					seen.add(name);
				}
			});

			response.scrapers.forEach((scraper) => {
				if (!seen.has(scraper.name)) {
					mergedOrder.push(scraper.name);
					seen.add(scraper.name);
				}
			});

			scrapers = mergedOrder.map((name: string) => {
				if (!sc[name]) {
					sc[name] = { enabled: scraperEnabledMap[name] ?? false } as ScraperSettings;
				} else if (
					(sc[name] as ScraperSettings).enabled === undefined &&
					scraperEnabledMap[name] !== undefined
				) {
					(sc[name] as ScraperSettings).enabled = scraperEnabledMap[name];
				}

				return {
					name,
					enabled: (sc[name] as ScraperSettings)?.enabled ?? false,
					displayName: scraperDisplayNames[name] || name,
					expanded: false,
					options: scraperOptionsMap[name] || []
				};
			});
			scrapers = deps.refreshLocalProxyProfileChoices(scrapers);
		} catch (e) {
			toastStore.error('Failed to fetch scrapers from API');
			const mergedOrder: string[] = [];
			const seen = new Set<string>();

			(sc.priority || []).forEach((name: string) => {
				if (!seen.has(name)) {
					mergedOrder.push(name);
					seen.add(name);
				}
			});

			Object.keys(sc)
				.filter((name: string) => name !== 'priority' && name !== 'proxy')
				.forEach((name: string) => {
					if (!seen.has(name)) {
						mergedOrder.push(name);
						seen.add(name);
					}
				});

			scrapers = mergedOrder.map((name: string) => ({
				name,
				enabled: (sc[name] as ScraperSettings)?.enabled ?? false,
				displayName: name,
				expanded: false,
				options: []
			}));
			scrapers = deps.refreshLocalProxyProfileChoices(scrapers);
		}
	}

	function scraperHasOptions(scraper: ScraperItem): boolean {
		return scraperSupportsProxyOptions(scraper) || getRenderableScraperOptions(scraper).length > 0;
	}

	function scraperSupportsProxyOptions(scraper: ScraperItem): boolean {
		return (scraper.options || []).some((option) => option.key.startsWith('proxy.'));
	}

	function getRenderableScraperOptions(scraper: ScraperItem): ScraperOption[] {
		return (scraper.options || []).filter(
			(option) => !option.key.startsWith('proxy.') && !option.key.startsWith('download_proxy.')
		);
	}

	function getScraperConfigNames(): string[] {
		const config = deps.getConfig();
		if (!config?.scrapers) return [];
		return Object.keys(config.scrapers).filter(
			(name: string) =>
				!['priority', 'proxy', 'user_agent', 'referer', 'timeout_seconds', 'request_timeout_seconds'].includes(name)
		);
	}

	function stripLegacyDownloadProxyFields(): void {
		const config = deps.getConfig();
		for (const scraperName of getScraperConfigNames()) {
			const scraperCfg = config?.scrapers?.[scraperName] as ScraperSettings | undefined;
			if (scraperCfg?.download_proxy !== undefined) {
				delete scraperCfg.download_proxy;
			}
		}
	}

	function getScraperProxyMode(scraperName: string): ScraperProxyMode {
		const config = deps.getConfig();
		const globalEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const override = (config?.scrapers?.[scraperName] as ScraperSettings | undefined)?.proxy;
		return getScraperProxyModePure(globalEnabled, override);
	}

	function setScraperProxyMode(scraperName: string, mode: ScraperProxyMode): void {
		const config = deps.getConfig();
		if (!config?.scrapers) return;
		if (!config.scrapers[scraperName]) config.scrapers[scraperName] = {} as ScraperSettings;
		const scraperCfg = config.scrapers[scraperName] as ScraperSettings;
		if (!scraperCfg.proxy || typeof scraperCfg.proxy !== 'object') {
			scraperCfg.proxy = { enabled: false } as ProxyConfig;
		}

		const proxyCfg = scraperCfg.proxy;

		switch (mode) {
			case 'direct':
				proxyCfg.enabled = false;
				proxyCfg.profile = '';
				break;
			case 'inherit':
				proxyCfg.enabled = true;
				proxyCfg.profile = '';
				break;
			case 'specific':
				proxyCfg.enabled = true;
				if (!(proxyCfg.profile ?? '').trim()) {
					const defaultProfile = config.scrapers.proxy?.default_profile ?? '';
					const firstProfile = deps.getProxyProfileNames()[0] ?? '';
					proxyCfg.profile = defaultProfile || firstProfile;
				}
				break;
		}

		deps.setConfig(JSON.parse(JSON.stringify(config)));
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

	function getNestedValue(obj: Record<string, unknown> | undefined, path: string): unknown {
		if (!obj) return undefined;
		return path.split('.').reduce((acc: unknown, key: string) => {
			if (acc && typeof acc === 'object') return (acc as Record<string, unknown>)[key];
			return undefined;
		}, obj);
	}

	function setNestedValue(obj: Record<string, unknown>, path: string, value: unknown): void {
		const keys = path.split('.');
		let current: Record<string, unknown> = obj;
		for (let i = 0; i < keys.length - 1; i++) {
			const key = keys[i];
			if (!current[key] || typeof current[key] !== 'object') {
				current[key] = {};
			}
			current = current[key] as Record<string, unknown>;
		}
		current[keys[keys.length - 1]] = value;
	}

	function getOptionValue(
		scraperName: string,
		optionKey: string
	): string | number | boolean | undefined {
		const config = deps.getConfig();
		if (optionKey === 'download_proxy.enabled') {
			const downloadProxy = getNestedValue(
				config?.scrapers?.[scraperName] as Record<string, unknown> | undefined,
				'download_proxy'
			) as Record<string, unknown> | undefined;
			if (!downloadProxy || typeof downloadProxy !== 'object') return false;
			if (downloadProxy.enabled !== undefined) return !!downloadProxy.enabled;
			return !!(
				downloadProxy.profile ||
				downloadProxy.url ||
				downloadProxy.username ||
				downloadProxy.password ||
				downloadProxy.use_main_proxy
			);
		}

		const scraper = scrapers.find((s) => s.name === scraperName);
		const option = scraper?.options?.find((o) => o.key === optionKey);

		const currentValue = getNestedValue(
			config?.scrapers?.[scraperName] as Record<string, unknown> | undefined,
			optionKey
		);

		if (currentValue === undefined || currentValue === null || currentValue === '') {
			return option?.default ?? (currentValue === null ? undefined : currentValue);
		}

		if (
			typeof currentValue === 'string' ||
			typeof currentValue === 'number' ||
			typeof currentValue === 'boolean'
		) {
			return currentValue;
		}
		return undefined;
	}

	function setOptionValue(
		scraperName: string,
		optionKey: string,
		value: string | number | boolean
	) {
		const config = deps.getConfig();
		if (!config?.scrapers) return;
		if (!config.scrapers[scraperName]) config.scrapers[scraperName] = {} as ScraperSettings;

		setNestedValue(config.scrapers[scraperName] as Record<string, unknown>, optionKey, value);

		deps.setConfig(JSON.parse(JSON.stringify(config)));
	}

	function parseOptionNumber(value: string): number | undefined {
		const parsed = parseInt(value, 10);
		return Number.isNaN(parsed) ? undefined : parsed;
	}

	function sanitizeHeaderValue(value: string): string {
		return value.replace(/[\r\n\x00-\x1F\x7F]/g, '');
	}

	function handleScraperUserAgentInput(e: Event) {
		const config = deps.getConfig();
		if (!config) return;
		if (!config.scrapers) config.scrapers = {};
		const target = e.target as HTMLInputElement;
		config.scrapers.user_agent = sanitizeHeaderValue(target.value);
	}

	function handleScraperRefererInput(e: Event) {
		const config = deps.getConfig();
		if (!config) return;
		if (!config.scrapers) config.scrapers = {};
		const target = e.target as HTMLInputElement;
		config.scrapers.referer = sanitizeHeaderValue(target.value);
	}

	function updateConfigFromScrapers() {
		const config = deps.getConfig();
		if (!config) return;
		if (!config.scrapers) config.scrapers = {};
		const sc = config.scrapers;

		sc.priority = scrapers.map((s) => s.name);

		scrapers.forEach((scraper) => {
			if (!sc[scraper.name]) sc[scraper.name] = {} as ScraperSettings;
			(sc[scraper.name] as ScraperSettings).enabled = scraper.enabled;
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

	async function toggleScraper(index: number) {
		const scraper = scrapers[index];
		const wasEnabled = scraper.enabled;
		const willBeEnabled = !wasEnabled;

		if (wasEnabled && !willBeEnabled) {
			const usageInfo = getScraperUsage(scraper.name);
			if (usageInfo.count > 0) {
				if (!(await confirmDialog(
					'Disable Scraper',
					`${scraper.displayName} is currently used in ${usageInfo.count} field(s):\n\n${usageInfo.fields.join(', ')}\n\nDisabling this scraper will remove it from all priority lists. Continue?`,
					{ variant: 'danger', confirmLabel: 'Disable' }
				))) return;

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

	async function clearAllScrapers() {
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
			if (!(await confirmDialog(
				'Disable All Scrapers',
				`The following scrapers are currently used in priority lists:\n\n${usedScrapers.join(', ')}\n\nDisabling all scrapers will remove them from all priority lists. Continue?`,
				{ variant: 'danger', confirmLabel: 'Disable All' }
			))) return;

			for (const scraper of scrapers) {
				removeScraperFromPriorities(scraper.name);
			}
		}
		scrapers = scrapers.map((scraper) => ({ ...scraper, enabled: false }));
		updateConfigFromScrapers();
	}

	function getScraperUsage(scraperName: string): { count: number; fields: string[] } {
		const config = deps.getConfig();
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
			const fieldPriority = config?.metadata?.priority?.[field.key];
			const priority = fieldPriority && fieldPriority.length > 0 ? fieldPriority : globalPriority;

			if (priority.includes(scraperName)) {
				fieldsUsing.push(field.label);
			}
		});

		return { count: fieldsUsing.length, fields: fieldsUsing };
	}

	function removeScraperFromPriorities(scraperName: string) {
		const config = deps.getConfig();
		if (!config) return;
		const cfg = config;

		if (cfg.scrapers?.priority) {
			cfg.scrapers.priority = cfg.scrapers.priority.filter((s: string) => s !== scraperName);
		}

		if (cfg.metadata?.priority) {
			const md = cfg.metadata;
			const priority = md.priority!;
			Object.keys(priority).forEach((fieldKey) => {
				const fieldPriority = priority[fieldKey];
				if (Array.isArray(fieldPriority)) {
					priority[fieldKey] = fieldPriority.filter((s: string) => s !== scraperName);
				}
			});
		}
	}

	function isOptionDisabled(scraperName: string, optionKey: string): boolean {
		const config = deps.getConfig();
		const globalProxyEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const globalFlareSolverrEnabled = config?.scrapers?.flaresolverr?.enabled ?? false;
		const globalBrowserEnabled = config?.scrapers?.browser?.enabled ?? false;
		const globalScrapeActress = config?.scrapers?.scrape_actress ?? true;
		const scraperCfg = (config?.scrapers?.[scraperName] ?? {}) as Partial<ScraperSettings>;

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
				return !(config?.scrapers?.flaresolverr?.enabled ?? false);
			}

			return false;
		}

		return false;
	}

	return {
		get scrapers() { return scrapers; },
		set scrapers(v) { scrapers = v; },
		buildScraperList,
		scraperHasOptions,
		scraperSupportsProxyOptions,
		getRenderableScraperOptions,
		getScraperConfigNames,
		stripLegacyDownloadProxyFields,
		getScraperProxyMode,
		setScraperProxyMode,
		toggleExpanded,
		toggleScraperRow,
		onScraperRowKeydown,
		isInteractiveRowTarget,
		onScraperRowClick,
		getOptionValue,
		setOptionValue,
		getNestedValue,
		setNestedValue,
		parseOptionNumber,
		sanitizeHeaderValue,
		handleScraperUserAgentInput,
		handleScraperRefererInput,
		updateConfigFromScrapers,
		moveScraperUp,
		moveScraperDown,
		toggleScraper,
		selectAllScrapers,
		clearAllScrapers,
		getScraperUsage,
		removeScraperFromPriorities,
		isOptionDisabled
	};
}
