<script lang="ts">
	import { portalToBody } from '$lib/actions/portal';
	import { apiClient } from '$lib/api/client';
	import { Save, RefreshCw, CircleAlert, ArrowLeft, X, Tags, Type } from 'lucide-svelte';
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
	import ApiTokensSection from '$lib/components/settings/sections/ApiTokensSection.svelte';
	import CompletenessSettingsSection from '$lib/components/settings/sections/CompletenessSettingsSection.svelte';
	import WebUISettingsSection from '$lib/components/settings/sections/WebUISettingsSection.svelte';
	import TokenDisplayModal from '$lib/components/settings/TokenDisplayModal.svelte';
	import type { CreateTokenResponse } from '$lib/types/token';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import { createSettingsStore } from './stores/settings-store.svelte';
	import { createProxyStore } from './stores/proxy-store.svelte';
	import { createScraperStore, type ScraperItem } from './stores/scraper-store.svelte';

	const settings = createSettingsStore({
		getProfileTestResults: () => proxy.profileTestResults,
		getGlobalProxyTestResult: () => proxy.globalProxyTestResult,
		getGlobalFlareSolverrTestResult: () => proxy.globalFlareSolverrTestResult,
		getVerificationTokens: () => proxy.verificationTokens,
		clearTestResults: () => proxy.clearTestResults(),
		invalidateGlobalProxyTest: () => proxy.invalidateGlobalProxyTest(),
		invalidateGlobalFlareSolverrTest: () => proxy.invalidateGlobalFlareSolverrTest(),
		onConfigInitialized: () => {
			scraper.stripLegacyDownloadProxyFields();
			scraper.buildScraperList();
		}
	});

	const proxy = createProxyStore({
		getConfig: () => settings.config,
		setConfig: (c) => { settings.config = c; },
		getError: () => settings.error,
		setError: (e) => { settings.error = e; },
		getScrapers: () => scraper.scrapers,
		setScrapers: (s: ScraperItem[]) => { scraper.scrapers = s; },
		getScraperConfigNames: () => scraper.getScraperConfigNames(),
		ensureProxyProfilesInitialized: () => settings.ensureProxyProfilesInitialized()
	});

	const scraper = createScraperStore({
		getConfig: () => settings.config,
		setConfig: (c) => { settings.config = c; },
		getProxyProfileNames: () => proxy.getProxyProfileNames(),
		refreshLocalProxyProfileChoices: (s: ScraperItem[]) => proxy.refreshLocalProxyProfileChoices(s)
	});

	let tokenDisplayResponse = $state<CreateTokenResponse | null>(null);

	function handleTokenDisplay(response: CreateTokenResponse) {
		tokenDisplayResponse = response;
	}

	function handleCloseTokenModal() {
		tokenDisplayResponse = null;
	}
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
				<Button variant="outline" onclick={settings.reloadConfig} disabled={settings.loading}>
					{#snippet children()}
						<RefreshCw class="h-4 w-4 mr-2" />
						Reload
					{/snippet}
				</Button>
				<Button onclick={settings.handleSave} disabled={settings.saveConfigMutation.isPending || settings.loading}>
					{#snippet children()}
						<Save class="h-4 w-4 mr-2" />
						{settings.saveConfigMutation.isPending ? 'Saving...' : 'Save Changes'}
					{/snippet}
				</Button>
			</div>
		</div>

		{#if settings.error}
			<div class="bg-destructive/10 border-2 border-destructive text-destructive px-4 py-3 rounded-lg flex items-start gap-2">
				<CircleAlert class="h-5 w-5 mt-0.5 shrink-0" />
				<p>{settings.error}</p>
			</div>
		{/if}

		{#if settings.loading}
			<Card class="p-8 text-center">
				<RefreshCw class="h-8 w-8 animate-spin mx-auto mb-2" />
				<p class="text-muted-foreground">Loading configuration...</p>
			</Card>
		{:else if settings.settingsConfig}
			<ServerSettingsSection config={settings.settingsConfig} inputClass={settings.inputClass} />

			<SettingsSection title="Scraper Defaults" description="Default settings applied to all scrapers unless overridden per-scraper" defaultExpanded={false}>
				<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
					<div>
						<label class="block text-sm font-medium mb-2" for="scrapers-user-agent">Default User-Agent</label>
						<input
							id="scrapers-user-agent"
							type="text"
							value={settings.config?.scrapers?.user_agent ?? ''}
							oninput={scraper.handleScraperUserAgentInput}
							class={settings.inputClass}
							placeholder="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
						/>
						<p class="text-xs text-muted-foreground mt-1">Custom User-Agent for scraper requests (default browser UA if empty)</p>
					</div>
					<div>
						<label class="block text-sm font-medium mb-2" for="scrapers-referer">Default Referer</label>
						<input
							id="scrapers-referer"
							type="text"
							value={settings.config?.scrapers?.referer ?? ''}
							oninput={scraper.handleScraperRefererInput}
							class={settings.inputClass}
							placeholder="https://www.dmm.co.jp/"
						/>
					<p class="text-xs text-muted-foreground mt-1">Referer header for CDN compatibility (default: https://www.dmm.co.jp/)</p>
				</div>
			</div>

			<div class="pt-4 border-t mt-4">
				<FormToggle
					id="global-scrape-actress"
					label="Scrape Actress Information (Global Default)"
					description="Default setting for actress scraping across all scrapers. Individual scrapers can override this in their settings."
					checked={settings.config?.scrapers?.scrape_actress ?? true}
					onchange={(val) => {
						if (!settings.config) return;
						if (!settings.config.scrapers) settings.config.scrapers = {};
						settings.config.scrapers.scrape_actress = val;
					}}
				/>
			</div>
		</SettingsSection>

	<BrowserSettingsSection 
		config={settings.settingsConfig} 
		inputClass={settings.inputClass} 
		onChange={(path, value) => {
			try {
			scraper.setNestedValue(settings.config as Record<string, unknown>, path, value);
				settings.config = JSON.parse(JSON.stringify(settings.config));
			} catch (err) {
				toastStore.error(`Failed to update setting: ${err instanceof Error ? err.message : String(err)}`);
			}
		}}
	/>

			<ScraperSettingsSection
				config={settings.settingsConfig}
				scrapers={scraper.scrapers}
				inputClass={settings.inputClass}
				scraperHasOptions={scraper.scraperHasOptions}
				onScraperRowClick={scraper.onScraperRowClick}
				onScraperRowKeydown={scraper.onScraperRowKeydown}
				toggleScraper={scraper.toggleScraper}
				toggleExpanded={scraper.toggleExpanded}
				selectAllScrapers={scraper.selectAllScrapers}
				clearAllScrapers={scraper.clearAllScrapers}
				getScraperUsage={scraper.getScraperUsage}
				scraperSupportsProxyOptions={scraper.scraperSupportsProxyOptions}
				getScraperProxyMode={scraper.getScraperProxyMode}
				setScraperProxyMode={scraper.setScraperProxyMode}
				getProxyProfileNames={proxy.getProxyProfileNames}
				setOptionValue={scraper.setOptionValue}
				getRenderableScraperOptions={scraper.getRenderableScraperOptions}
				isOptionDisabled={scraper.isOptionDisabled}
				getOptionValue={scraper.getOptionValue}
				parseOptionNumber={scraper.parseOptionNumber}
			/>

			<SettingsSection title="Metadata Priority" description="Configure which scraper to use for each metadata field" defaultExpanded={false}>
				<MetadataPriority config={settings.settingsConfig} onUpdate={(updatedConfig) => { settings.config = updatedConfig; }} />
			</SettingsSection>

			<CompletenessSettingsSection config={settings.settingsConfig} inputClass={settings.inputClass} />

			<FileOperationsSettingsSection config={settings.settingsConfig} />
			<OutputSettingsSection config={settings.settingsConfig} inputClass={settings.inputClass} />
			<DatabaseSettingsSection config={settings.settingsConfig} inputClass={settings.inputClass} />
			<ApiTokensSection onTokenDisplay={handleTokenDisplay} />
			<SettingsSection title="Genre Replacements" description="Manage genre name replacements applied during scraping" defaultExpanded={false}>
				<div class="flex items-center justify-between">
					<p class="text-sm text-muted-foreground">
						Manage genre name replacements that are applied during scraping.
					</p>
					<a href="/genres">
						<Button variant="outline" size="sm">
							{#snippet children()}
								<Tags class="h-4 w-4 mr-1" />
								Manage Genres
							{/snippet}
						</Button>
					</a>
				</div>
			</SettingsSection>

			<SettingsSection title="Word Replacements" description="Manage word uncensor rules applied during scraping" defaultExpanded={false}>
				<div class="flex items-center justify-between">
					<p class="text-sm text-muted-foreground">
						Manage word replacements that uncensor asterisked text in scraped metadata.
					</p>
					<a href="/words">
						<Button variant="outline" size="sm">
							{#snippet children()}
								<Type class="h-4 w-4 mr-1" />
								Manage Words
							{/snippet}
						</Button>
					</a>
				</div>
			</SettingsSection>

			<TranslationSettingsSection
				config={settings.settingsConfig}
				inputClass={settings.inputClass}
				fetchTranslationModels={settings.fetchTranslationModels}
				fetchingTranslationModels={settings.fetchingTranslationModels}
				translationModelOptions={settings.translationModelOptions}
			/>
			<NfoSettingsSection config={settings.settingsConfig} />
			<ProxySettingsSection
				config={settings.settingsConfig}
				inputClass={settings.inputClass}
				testingProxy={proxy.testingProxy}
				testingFlareSolverr={proxy.testingFlareSolverr}
				testingProfile={proxy.testingProfile}
				savingProfile={proxy.savingProfile}
				loading={settings.loading}
				saving={settings.saveConfigMutation.isPending}
				profileTestResults={proxy.profileTestResults}
				globalProxyTestResult={proxy.globalProxyTestResult}
				globalFlareSolverrTestResult={proxy.globalFlareSolverrTestResult}
				canSaveProfile={proxy.canSaveProfile}
				isTestExpired={proxy.isTestExpired}
				getProxyProfileNames={proxy.getProxyProfileNames}
				addProxyProfile={proxy.addProxyProfile}
				renameProxyProfile={proxy.renameProxyProfile}
				removeProxyProfile={proxy.removeProxyProfile}
				setProxyProfileField={proxy.setProxyProfileField}
				saveProxyProfile={proxy.saveProxyProfile}
				runNamedProxyProfileTest={proxy.runNamedProxyProfileTest}
				runProxyTest={proxy.runProxyTest}
				invalidateGlobalProxyTest={proxy.invalidateGlobalProxyTest}
				invalidateGlobalFlareSolverrTest={proxy.invalidateGlobalFlareSolverrTest}
			/>
			<PerformanceSettingsSection config={settings.settingsConfig} inputClass={settings.inputClass} />
			<FileMatchingSettingsSection config={settings.settingsConfig} inputClass={settings.inputClass} />
			<LoggingSettingsSection config={settings.settingsConfig} inputClass={settings.inputClass} />
			<MediaInfoSettingsSection config={settings.settingsConfig} />
			<WebUISettingsSection config={settings.settingsConfig} inputClass={settings.inputClass} />
		{/if}
	</div>
</div>

<TokenDisplayModal tokenResponse={tokenDisplayResponse} onClose={handleCloseTokenModal} />

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
