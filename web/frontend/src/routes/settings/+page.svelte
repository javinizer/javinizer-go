<script lang="ts">
	import { onMount } from 'svelte';
	import { apiClient } from '$lib/api/client';
	import type { ScraperOption } from '$lib/api/types';
	import { Save, RefreshCw, AlertCircle, ArrowLeft, CheckCircle2, X, GripVertical, ChevronUp, ChevronDown, ChevronRight } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { toastStore } from '$lib/stores/toast';
	import MetadataPriority from '$lib/components/priority/MetadataPriority.svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import FormPasswordInput from '$lib/components/settings/FormPasswordInput.svelte';
	import FormTemplateInput from '$lib/components/settings/FormTemplateInput.svelte';

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
		}
	}

	// Check if scraper has options to show
	function scraperHasOptions(scraper: ScraperItem): boolean {
		return scraper.options && scraper.options.length > 0;
	}

	function toggleExpanded(index: number) {
		scrapers[index].expanded = !scrapers[index].expanded;
	}

	// Helper to get option value from config (using snake_case keys)
	function getOptionValue(scraperName: string, optionKey: string): any {
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

	function isOptionDisabled(scraperName: string, optionKey: string): boolean {
		const globalProxyEnabled = config?.scrapers?.proxy?.enabled ?? false;
		const scraperCfg = config?.scrapers?.[scraperName] ?? {};

		if (optionKey.startsWith('proxy.')) {
			if (!globalProxyEnabled) return true;

			const scraperProxyEnabled = scraperCfg?.proxy?.enabled ?? false;
			if (optionKey === 'proxy.enabled') return false;
			if (!scraperProxyEnabled) return true;

			if (
				(optionKey === 'proxy.url' || optionKey === 'proxy.username' || optionKey === 'proxy.password') &&
				(scraperCfg?.proxy?.use_main_proxy ?? false)
			) {
				return true;
			}

			if (optionKey.startsWith('proxy.flaresolverr.')) {
				if (optionKey === 'proxy.flaresolverr.enabled') return false;
				return !(scraperCfg?.proxy?.flaresolverr?.enabled ?? false);
			}

			return false;
		}

		if (optionKey.startsWith('download_proxy.')) {
			if (!globalProxyEnabled) return true;

			const downloadProxyEnabled = scraperCfg?.download_proxy?.enabled ?? false;
			if (optionKey === 'download_proxy.enabled') return false;
			if (!downloadProxyEnabled) return true;

			if (
				(optionKey === 'download_proxy.url' ||
					optionKey === 'download_proxy.username' ||
					optionKey === 'download_proxy.password') &&
				(scraperCfg?.download_proxy?.use_main_proxy ?? false)
			) {
				return true;
			}

			return false;
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

	async function runProxyTest(mode: 'direct' | 'flaresolverr') {
		if (!config?.scrapers?.proxy) {
			toastStore.error('Scraper proxy configuration is missing', 5000);
			return;
		}

		const proxyConfig = JSON.parse(JSON.stringify(config.scrapers.proxy));

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
	<div class="max-w-4xl mx-auto space-y-6">
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
				<AlertCircle class="h-5 w-5 mt-0.5 flex-shrink-0" />
				<p>{error}</p>
			</div>
		{/if}

		{#if loading}
			<Card class="p-8 text-center">
				<RefreshCw class="h-8 w-8 animate-spin mx-auto mb-2" />
				<p class="text-muted-foreground">Loading configuration...</p>
			</Card>
		{:else if config}
			<!-- Server Settings -->
			<SettingsSection title="Server Settings" description="Configure API server host and port" defaultExpanded={false}>
				<div class="grid grid-cols-2 gap-4">
					<div>
						<label class="block text-sm font-medium mb-2" for="server-host">Host</label>
						<input id="server-host" type="text" bind:value={config.server.host} class={inputClass} placeholder="localhost" />
					</div>
					<div>
						<label class="block text-sm font-medium mb-2" for="server-port">Port</label>
						<input id="server-port" type="number" bind:value={config.server.port} class={inputClass} placeholder="8080" />
					</div>
				</div>
			</SettingsSection>

			<!-- Scraper Settings -->
			<SettingsSection title="Scraper Settings" description="Enable/disable scrapers and configure user agent. Scraper priority is managed in Metadata Priority section." defaultExpanded={false}>
				<div class="space-y-4">
					<div>
						<span class="block text-sm font-medium mb-2">Available Scrapers</span>
						<div class="space-y-2">
							{#each scrapers as scraper, index}
								<div class="rounded-lg border {scraper.enabled ? 'bg-background' : 'bg-muted/30'}">
									<!-- Main scraper row -->
									<div class="flex items-center gap-3 p-3">
										<!-- Checkbox -->
										<input
											type="checkbox"
											checked={scraper.enabled}
											onchange={() => toggleScraper(index)}
											class="rounded"
										/>

										<!-- Scraper Name -->
										<div class="flex-1 font-medium {scraper.enabled ? '' : 'text-muted-foreground'}">
											{scraper.displayName}
											{#if scraper.enabled}
												{@const usage = getScraperUsage(scraper.name)}
												{#if usage.count > 0}
													<span class="ml-2 text-xs font-normal text-muted-foreground">
														(used in {usage.count} field{usage.count !== 1 ? 's' : ''})
													</span>
												{/if}
											{/if}
										</div>

										<!-- Expand button (only if scraper has options and is enabled) -->
										{#if scraper.enabled && scraperHasOptions(scraper)}
											<Button
												variant="ghost"
												size="icon"
												onclick={() => toggleExpanded(index)}
												class="h-8 w-8"
											>
												{#snippet children()}
													{#if scraper.expanded}
														<ChevronDown class="h-4 w-4" />
													{:else}
														<ChevronRight class="h-4 w-4" />
													{/if}
												{/snippet}
											</Button>
										{/if}
									</div>

									<!-- Collapsible options section - dynamically rendered -->
									{#if scraper.enabled && scraper.expanded && scraper.options.length > 0}
										<div class="px-3 pb-3 pt-0 border-t bg-muted/20">
											<div class="pl-8 py-3 space-y-3">
												<h4 class="text-sm font-medium">{scraper.displayName} Options</h4>
												{#each scraper.options as option}
													<div class="space-y-1">
														{#if option.type === 'boolean'}
															<label class="flex items-center gap-2">
																<input
																	type="checkbox"
																	checked={getOptionValue(scraper.name, option.key)}
																	disabled={isOptionDisabled(scraper.name, option.key)}
																	onchange={(e) => setOptionValue(scraper.name, option.key, e.currentTarget.checked)}
																	class="rounded"
																/>
																<span class="text-sm {isOptionDisabled(scraper.name, option.key) ? 'text-muted-foreground' : ''}">{option.label}</span>
															</label>
															<p class="text-xs text-muted-foreground ml-6">
																{option.description}
															</p>
														{:else if option.type === 'select'}
															<div>
																<label class="block text-sm font-medium mb-1" for="option-{scraper.name}-{option.key}">{option.label}</label>
																<select
																	id="option-{scraper.name}-{option.key}"
																	value={getOptionValue(scraper.name, option.key) ?? ''}
																	disabled={isOptionDisabled(scraper.name, option.key)}
																	onchange={(e) => setOptionValue(scraper.name, option.key, e.currentTarget.value)}
																	class="w-48 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
																>
																	{#each option.choices ?? [] as choice}
																		<option value={choice.value}>{choice.label}</option>
																	{/each}
																</select>
																<p class="text-xs text-muted-foreground mt-1">
																	{option.description}
																</p>
															</div>
														{:else if option.type === 'string' || option.type === 'password'}
															<div>
																<label class="block text-sm font-medium mb-1" for="option-{scraper.name}-{option.key}">{option.label}</label>
																<input
																	id="option-{scraper.name}-{option.key}"
																	type={option.type === 'password' ? 'password' : 'text'}
																	value={getOptionValue(scraper.name, option.key) ?? ''}
																	disabled={isOptionDisabled(scraper.name, option.key)}
																	oninput={(e) => setOptionValue(scraper.name, option.key, e.currentTarget.value)}
																	class="w-full max-w-md px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
																/>
																<p class="text-xs text-muted-foreground mt-1">
																	{option.description}
																</p>
															</div>
														{:else if option.type === 'number'}
															<div>
																<label class="block text-sm font-medium mb-1" for="option-{scraper.name}-{option.key}">{option.label}</label>
																<div class="flex items-center gap-2">
																	<input
																		id="option-{scraper.name}-{option.key}"
																		type="number"
																		value={getOptionValue(scraper.name, option.key) ?? ''}
																		disabled={isOptionDisabled(scraper.name, option.key)}
																		oninput={(e) => setOptionValue(scraper.name, option.key, parseOptionNumber(e.currentTarget.value))}
																		min={option.min || 0}
																		max={option.max || 999}
																		class="w-32 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
																	/>
																	{#if option.unit}
																		<span class="text-sm text-muted-foreground">{option.unit}</span>
																	{/if}
																</div>
																<p class="text-xs text-muted-foreground mt-1">
																	{option.description}
																</p>
															</div>
														{/if}
													</div>
												{/each}
											</div>
										</div>
									{/if}
								</div>
							{/each}
						</div>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="user-agent">User Agent</label>
						<input id="user-agent" type="text" bind:value={config.scrapers.user_agent} class={inputClass} />
					</div>
				</div>
			</SettingsSection>

			<!-- Metadata Priority Settings -->
			<SettingsSection title="Metadata Priority" description="Configure which scraper to use for each metadata field" defaultExpanded={false}>
				<MetadataPriority config={config} onUpdate={(updatedConfig) => { config = updatedConfig; }} />
			</SettingsSection>

			<!-- File Operations Section -->
			<SettingsSection title="File Operations" description="Control how Javinizer organizes and moves your files" defaultExpanded={false}>
				<FormToggle
					label="Move to folder"
					description="Create a dedicated folder for each movie and move files into it"
					checked={config.output.move_to_folder ?? true}
					onchange={(val) => { config.output.move_to_folder = val; }}
				/>

				<FormToggle
					label="Rename file"
					description="Rename video files according to the file naming template"
					checked={config.output.rename_file ?? true}
					onchange={(val) => { config.output.rename_file = val; }}
				/>

				<FormToggle
					label="Rename folder in place"
					description="Rename the parent folder without moving files to a new location"
					checked={config.output.rename_folder_in_place ?? false}
					onchange={(val) => { config.output.rename_folder_in_place = val; }}
				/>

				<SettingsSubsection title="Subtitle Handling">
					<FormToggle
						label="Move subtitles"
						description="Automatically move subtitle files (.srt, .ass, etc.) with video files"
						checked={config.output.move_subtitles ?? false}
						onchange={(val) => { config.output.move_subtitles = val; }}
					/>

					<FormTextInput
						label="Subtitle extensions"
						description="Comma-separated list of subtitle file extensions to move with videos"
						value={config.output.subtitle_extensions?.join(', ') ?? ".srt, .ass, .ssa, .sub, .vtt"}
						placeholder=".srt, .ass, .ssa, .sub, .vtt"
						onchange={(val) => {
							config.output.subtitle_extensions = val.split(',').map(s => s.trim()).filter(s => s.length > 0);
						}}
					/>
				</SettingsSubsection>
			</SettingsSection>

			<!-- Output Settings -->
			<SettingsSection title="Output Settings" description="Configure output paths, templates, and download options" defaultExpanded={false}>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2" for="destination-path">Destination Path</label>
						<p class="text-sm text-muted-foreground mb-2">
							Note: Destination path is currently not configurable via API
						</p>
						<input
							id="destination-path"
							type="text"
							value="Configured in config.yaml"
							class={inputClass}
							disabled
							placeholder="/path/to/output"
						/>
					</div>

					<SettingsSubsection title="Template Options">
						<FormNumberInput
							label="Max title length"
							description="Maximum characters for movie titles in folder names. Longer titles will be intelligently truncated."
							value={config.output.max_title_length ?? 100}
							min={10}
							max={500}
							unit="characters"
							onchange={(val) => { config.output.max_title_length = val; }}
						/>

						<FormNumberInput
							label="Max path length"
							description="Maximum total path length to prevent Windows path errors (MAX_PATH = 260)"
							value={config.output.max_path_length ?? 240}
							min={100}
							max={250}
							unit="characters"
							onchange={(val) => { config.output.max_path_length = val; }}
						/>

						<FormToggle
							label="Group actress"
							description="Group actress names with @ prefix (e.g., '@GroupName')"
							checked={config.output.group_actress ?? false}
							onchange={(val) => { config.output.group_actress = val; }}
						/>

						<div class="py-4 border-b border-border">
							<label class="block text-sm font-medium mb-2" for="delimiter">Delimiter</label>
							<input
								id="delimiter"
								type="text"
								bind:value={config.output.delimiter}
								class={inputClass}
								placeholder=", "
							/>
							<p class="text-xs text-muted-foreground mt-1">
								Character(s) used to separate multiple values (e.g., actresses, genres)
							</p>
						</div>
					</SettingsSubsection>

					<div>
						<label class="block text-sm font-medium mb-2" for="subfolder-format">Subfolder Format</label>
						<input
							id="subfolder-format"
							type="text"
							value={config.output.subfolder_format.join(', ')}
							onchange={(e) => {
								config.output.subfolder_format = e.currentTarget.value
									.split(',')
									.map((s) => s.trim())
									.filter((s) => s.length > 0);
							}}
							class={inputClass}
							placeholder="Leave empty for no subfolders"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Comma-separated list of subfolder names or templates
						</p>
					</div>

					<div class="space-y-3">
						<h3 class="font-medium">Download Options</h3>
						<label class="flex items-center gap-2">
							<input type="checkbox" bind:checked={config.output.download_poster} class="rounded" />
							<span>Download Poster</span>
						</label>
						<label class="flex items-center gap-2">
							<input type="checkbox" bind:checked={config.output.download_cover} class="rounded" />
							<span>Download Cover</span>
						</label>
						<label class="flex items-center gap-2">
							<input
								type="checkbox"
								bind:checked={config.output.download_extrafanart}
								class="rounded"
							/>
							<span>Download Extrafanart</span>
						</label>
						<label class="flex items-center gap-2">
							<input type="checkbox" bind:checked={config.output.download_trailer} class="rounded" />
							<span>Download Trailer</span>
						</label>
						<label class="flex items-center gap-2">
							<input type="checkbox" bind:checked={config.output.download_actress} class="rounded" />
							<span>Download Actress Images</span>
						</label>
					</div>

					<FormNumberInput
						label="Download timeout"
						description="Maximum time to wait for image/video downloads to complete"
						value={config.output.download_timeout ?? 60}
						min={5}
						max={600}
						unit="seconds"
						onchange={(val) => { config.output.download_timeout = val; }}
					/>

					<div>
						<label class="block text-sm font-medium mb-2" for="folder-format">Folder Naming Template</label>
						<input
							id="folder-format"
							type="text"
							bind:value={config.output.folder_format}
							class="{inputClass} font-mono text-sm"
							placeholder="<ID> - <TITLE>"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Available tags: &lt;ID&gt;, &lt;TITLE&gt;, &lt;STUDIO&gt;, &lt;YEAR&gt;, &lt;ACTRESS&gt;
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="file-format">File Naming Template</label>
						<input
							id="file-format"
							type="text"
							bind:value={config.output.file_format}
							class="{inputClass} font-mono text-sm"
							placeholder="<ID><PARTSUFFIX>"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Multi-part support: &lt;PART&gt; (part number), &lt;PARTSUFFIX&gt; (original suffix), &lt;IF:MULTIPART&gt;...&lt;/IF&gt;
						</p>
						<p class="text-xs text-muted-foreground">
							Examples: &lt;ID&gt;&lt;PARTSUFFIX&gt; or &lt;ID&gt;-CD&lt;PART:2&gt; or &lt;ID&gt;&lt;IF:MULTIPART&gt;-pt&lt;PART&gt;&lt;/IF&gt;
						</p>
					</div>

					<SettingsSubsection title="Media File Naming">
						<FormTemplateInput
							label="Poster format"
							description="Naming template for poster images"
							value={config.output.poster_format ?? "<ID>-poster.jpg"}
							placeholder="<ID>-poster.jpg"
							showTagList={true}
							onchange={(val) => { config.output.poster_format = val; }}
						/>

						<FormTemplateInput
							label="Fanart format"
							description="Naming template for fanart/cover images"
							value={config.output.fanart_format ?? "<ID>-fanart.jpg"}
							placeholder="<ID>-fanart.jpg"
							onchange={(val) => { config.output.fanart_format = val; }}
						/>

						<FormTemplateInput
							label="Trailer format"
							description="Naming template for trailer videos"
							value={config.output.trailer_format ?? "<ID>-trailer.mp4"}
							placeholder="<ID>-trailer.mp4"
							onchange={(val) => { config.output.trailer_format = val; }}
						/>

						<FormTemplateInput
							label="Screenshot format"
							description="Naming template for screenshot images"
							value={config.output.screenshot_format ?? "fanart"}
							placeholder="fanart"
							onchange={(val) => { config.output.screenshot_format = val; }}
						/>

						<FormTextInput
							label="Screenshot folder"
							description="Folder name for storing screenshot images"
							value={config.output.screenshot_folder ?? "extrafanart"}
							placeholder="extrafanart"
							onchange={(val) => { config.output.screenshot_folder = val; }}
						/>

						<FormNumberInput
							label="Screenshot padding"
							description="Zero-padding for screenshot numbers (e.g., 01, 02, 03)"
							value={config.output.screenshot_padding ?? 1}
							min={1}
							max={5}
							unit="digits"
							onchange={(val) => { config.output.screenshot_padding = val; }}
						/>

						<FormTextInput
							label="Actress folder"
							description="Folder name for storing actress images"
							value={config.output.actress_folder ?? ".actors"}
							placeholder=".actors"
							onchange={(val) => { config.output.actress_folder = val; }}
						/>

					<FormTemplateInput
						label="Actress format"
						description="Naming template for actress image files"
						value={config.output.actress_format ?? "<ACTORNAME>.jpg"}
						placeholder="<ACTORNAME>.jpg"
						onchange={(val) => { config.output.actress_format = val; }}
					/>
					</SettingsSubsection>
				</div>
			</SettingsSection>

			<!-- Database Settings -->
			<SettingsSection title="Database Settings" description="Configure database options and behavior" defaultExpanded={false}>
				<div class="mb-4">
					<label class="block text-sm font-medium mb-2" for="database-type">Database Type</label>
					<select id="database-type" bind:value={config.database.type} class={inputClass}>
						<option value="sqlite">SQLite</option>
						<option value="postgres">PostgreSQL</option>
						<option value="mysql">MySQL</option>
					</select>
					<p class="text-xs text-muted-foreground mt-1">
						Database engine to use (SQLite recommended for most users)
					</p>
				</div>

				<div class="mb-4">
					<label class="block text-sm font-medium mb-2" for="database-dsn">Database Path (DSN)</label>
					<input
						id="database-dsn"
						type="text"
						bind:value={config.database.dsn}
						class={inputClass}
						placeholder="data/javinizer.db"
					/>
				</div>

				<SettingsSubsection title="Actress Database">
					<FormToggle
						label="Auto-add actresses"
						description="Automatically add new actresses to the database when encountered"
						checked={config.metadata.actress_database?.auto_add ?? false}
						onchange={(val) => {
							if (!config.metadata.actress_database) config.metadata.actress_database = {};
							config.metadata.actress_database.auto_add = val;
						}}
					/>

					<FormToggle
						label="Convert aliases"
						description="Use actress aliases from the database when generating metadata"
						checked={config.metadata.actress_database?.convert_alias ?? false}
						onchange={(val) => {
							if (!config.metadata.actress_database) config.metadata.actress_database = {};
							config.metadata.actress_database.convert_alias = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Genre Replacement">
					<FormToggle
						label="Auto-add genres"
						description="Automatically add new genre replacements to the database"
						checked={config.metadata.genre_replacement?.auto_add ?? false}
						onchange={(val) => {
							if (!config.metadata.genre_replacement) config.metadata.genre_replacement = {};
							config.metadata.genre_replacement.auto_add = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Tag Database">
					<FormToggle
						label="Enable tag database"
						description="Enable per-movie tag lookup from database"
						checked={config.metadata.tag_database?.enabled ?? false}
						onchange={(val) => {
							if (!config.metadata.tag_database) config.metadata.tag_database = {};
							config.metadata.tag_database.enabled = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Advanced Metadata Options">
					<FormTextInput
						label="Ignore genres"
						description="Comma-separated list of genres to exclude from metadata"
						value={config.metadata.ignore_genres?.join(', ') ?? ""}
						placeholder="e.g., Sample, Trailer"
						onchange={(val) => {
							config.metadata.ignore_genres = val.split(',').map(s => s.trim()).filter(s => s.length > 0);
						}}
					/>

					<FormTextInput
						label="Required fields"
						description="Comma-separated list of required metadata fields (scraping fails if missing)"
						value={config.metadata.required_fields?.join(', ') ?? ""}
						placeholder="e.g., title, actress, studio"
						onchange={(val) => {
							config.metadata.required_fields = val.split(',').map(s => s.trim()).filter(s => s.length > 0);
						}}
					/>
				</SettingsSubsection>
			</SettingsSection>

			<!-- NFO Settings -->
			<SettingsSection title="NFO Settings" description="Configure NFO metadata file generation for Kodi/Plex compatibility" defaultExpanded={false}>
				<SettingsSubsection title="Basic NFO Options">
					<FormToggle
						label="Enable NFO generation"
						description="Generate .nfo metadata files for use with media servers like Kodi and Plex"
						checked={config.metadata.nfo?.enabled ?? true}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.enabled = val;
						}}
					/>

					<FormToggle
						label="NFO per file"
						description="Create separate NFO files for each video file (useful for multi-part movies)"
						checked={config.metadata.nfo?.per_file ?? false}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.per_file = val;
						}}
					/>

					<FormTemplateInput
						label="Display name template"
						description="Template for the <title> field in NFO files"
						value={config.metadata.nfo?.display_name ?? "[<ID>] <TITLE>"}
						placeholder="[<ID>] <TITLE>"
						showTagList={true}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.display_name = val;
						}}
					/>

					<FormTemplateInput
						label="Filename template"
						description="Template for NFO filenames"
						value={config.metadata.nfo?.filename_template ?? "<ID>"}
						placeholder="<ID>"
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.filename_template = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Actress Settings">
					<FormToggle
						label="First name order"
						description="Use first-name-first order for actress names (Western style)"
						checked={config.metadata.nfo?.first_name_order ?? false}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.first_name_order = val;
						}}
					/>

					<FormToggle
						label="Japanese actress names"
						description="Use Japanese names for actresses in NFO files"
						checked={config.metadata.nfo?.actress_language_ja ?? false}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.actress_language_ja = val;
						}}
					/>

					<FormTextInput
						label="Unknown actress text"
						description="Text to display when actress information is unavailable"
						value={config.metadata.nfo?.unknown_actress_text ?? "Unknown"}
						placeholder="Unknown"
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.unknown_actress_text = val;
						}}
					/>

					<FormToggle
						label="Actress as tag"
						description="Include actress names in the <tag> field"
						checked={config.metadata.nfo?.actress_as_tag ?? false}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.actress_as_tag = val;
						}}
					/>

					<FormToggle
						label="Add generic role"
						description="Add 'Actress' as a generic role for all performers"
						checked={config.metadata.nfo?.add_generic_role ?? false}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.add_generic_role = val;
						}}
					/>

					<FormToggle
						label="Use alternate name for role"
						description="Use actress alternate names in <role> field"
						checked={config.metadata.nfo?.alt_name_role ?? false}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.alt_name_role = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Media Information">
					<FormToggle
						label="Include stream details"
						description="Include video/audio codec information from MediaInfo analysis"
						checked={config.metadata.nfo?.include_stream_details ?? false}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.include_stream_details = val;
						}}
					/>

					<FormToggle
						label="Include fanart"
						description="Include fanart/cover image reference in NFO"
						checked={config.metadata.nfo?.include_fanart ?? true}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.include_fanart = val;
						}}
					/>

					<FormToggle
						label="Include trailer"
						description="Include trailer video reference in NFO"
						checked={config.metadata.nfo?.include_trailer ?? true}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.include_trailer = val;
						}}
					/>

					<FormTextInput
						label="Rating source"
						description="Source name for movie ratings (e.g., 'r18dev', 'dmm')"
						value={config.metadata.nfo?.rating_source ?? "r18dev"}
						placeholder="r18dev"
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.rating_source = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Advanced NFO Options">
					<FormToggle
						label="Include original path"
						description="Include the original file path in NFO metadata"
						checked={config.metadata.nfo?.include_originalpath ?? false}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.include_originalpath = val;
						}}
					/>

					<FormTemplateInput
						label="Tag template"
						description="Template for custom tags in NFO files"
						value={config.metadata.nfo?.tag ?? "<SET>"}
						placeholder="<SET>"
						showTagList={true}
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.tag = val;
						}}
					/>

					<FormTemplateInput
						label="Tagline template"
						description="Template for the tagline field in NFO files"
						value={config.metadata.nfo?.tagline ?? ""}
						placeholder=""
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.tagline = val;
						}}
					/>

					<FormTextInput
						label="Credits"
						description="Credits to include in NFO (comma-separated)"
						value={config.metadata.nfo?.credits?.join(', ') ?? ""}
						placeholder="Director Name, Studio Name"
						onchange={(val) => {
							if (!config.metadata.nfo) config.metadata.nfo = {};
							config.metadata.nfo.credits = val.split(',').map(s => s.trim()).filter(s => s.length > 0);
						}}
					/>
				</SettingsSubsection>
			</SettingsSection>

			<!-- Proxy Settings -->
			<SettingsSection title="Proxy Settings" description="Configure HTTP/SOCKS5 proxies for scraper requests and downloads" defaultExpanded={false}>
				<SettingsSubsection title="Scraper Proxy">
					<FormToggle
						label="Enable scraper proxy"
						description="Route all scraper requests through a proxy server"
						checked={config.scrapers.proxy?.enabled ?? false}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							config.scrapers.proxy.enabled = val;
						}}
					/>

					<FormTextInput
						label="Proxy URL"
						description="Proxy server URL (e.g., http://proxy.example.com:8080 or socks5://localhost:1080)"
						value={config.scrapers.proxy?.url ?? ""}
						placeholder="http://proxy.example.com:8080"
						disabled={!(config.scrapers.proxy?.enabled ?? false)}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							config.scrapers.proxy.url = val;
						}}
					/>

					<FormTextInput
						label="Proxy username"
						description="Username for authenticated proxy (optional)"
						value={config.scrapers.proxy?.username ?? ""}
						placeholder=""
						disabled={!(config.scrapers.proxy?.enabled ?? false)}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							config.scrapers.proxy.username = val;
						}}
					/>

					<FormPasswordInput
						label="Proxy password"
						description="Password for authenticated proxy (optional)"
						value={config.scrapers.proxy?.password ?? ""}
						disabled={!(config.scrapers.proxy?.enabled ?? false)}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							config.scrapers.proxy.password = val;
						}}
					/>

					<div class="pt-2">
						<Button variant="outline" size="sm" onclick={() => runProxyTest('direct')} disabled={testingProxy || loading || saving || !(config.scrapers.proxy?.enabled ?? false)}>
							{#snippet children()}
								<RefreshCw class={`h-4 w-4 mr-2 ${testingProxy ? 'animate-spin' : ''}`} />
								{testingProxy ? 'Testing Proxy...' : 'Test Scraper Proxy'}
							{/snippet}
						</Button>
					</div>
				</SettingsSubsection>

				<SettingsSubsection title="FlareSolverr">
					<FormToggle
						label="Enable FlareSolverr"
						description="Use FlareSolverr to bypass Cloudflare protection (required for JavLibrary). Run FlareSolverr via Docker: docker run -p 8191:8191 ghcr.io/flaresolverr/flaresolverr:latest"
						checked={config.scrapers.proxy?.flaresolverr?.enabled ?? false}
						disabled={!(config.scrapers.proxy?.enabled ?? false)}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							if (!config.scrapers.proxy.flaresolverr) config.scrapers.proxy.flaresolverr = {};
							config.scrapers.proxy.flaresolverr.enabled = val;
						}}
					/>

					<FormTextInput
						label="FlareSolverr URL"
						description="FlareSolverr API endpoint"
						value={config.scrapers.proxy?.flaresolverr?.url ?? "http://localhost:8191/v1"}
						placeholder="http://localhost:8191/v1"
						disabled={!(config.scrapers.proxy?.enabled ?? false) || !(config.scrapers.proxy?.flaresolverr?.enabled ?? false)}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							if (!config.scrapers.proxy.flaresolverr) config.scrapers.proxy.flaresolverr = {};
							config.scrapers.proxy.flaresolverr.url = val;
						}}
					/>

					<FormNumberInput
						label="Timeout"
						description="Maximum time to wait for FlareSolverr to solve challenges"
						value={config.scrapers.proxy?.flaresolverr?.timeout ?? 30}
						min={5}
						max={300}
						unit="seconds"
						disabled={!(config.scrapers.proxy?.enabled ?? false) || !(config.scrapers.proxy?.flaresolverr?.enabled ?? false)}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							if (!config.scrapers.proxy.flaresolverr) config.scrapers.proxy.flaresolverr = {};
							config.scrapers.proxy.flaresolverr.timeout = val;
						}}
					/>

					<FormNumberInput
						label="Max retries"
						description="Number of retry attempts for failed FlareSolverr requests"
						value={config.scrapers.proxy?.flaresolverr?.max_retries ?? 3}
						min={0}
						max={10}
						disabled={!(config.scrapers.proxy?.enabled ?? false) || !(config.scrapers.proxy?.flaresolverr?.enabled ?? false)}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							if (!config.scrapers.proxy.flaresolverr) config.scrapers.proxy.flaresolverr = {};
							config.scrapers.proxy.flaresolverr.max_retries = val;
						}}
					/>

					<FormNumberInput
						label="Session TTL"
						description="How long to keep FlareSolverr browser sessions alive"
						value={config.scrapers.proxy?.flaresolverr?.session_ttl ?? 300}
						min={60}
						max={3600}
						unit="seconds"
						disabled={!(config.scrapers.proxy?.enabled ?? false) || !(config.scrapers.proxy?.flaresolverr?.enabled ?? false)}
						onchange={(val) => {
							if (!config.scrapers.proxy) config.scrapers.proxy = {};
							if (!config.scrapers.proxy.flaresolverr) config.scrapers.proxy.flaresolverr = {};
							config.scrapers.proxy.flaresolverr.session_ttl = val;
						}}
					/>

					<div class="pt-2">
						<Button variant="outline" size="sm" onclick={() => runProxyTest('flaresolverr')} disabled={testingFlareSolverr || loading || saving || !(config.scrapers.proxy?.enabled ?? false) || !(config.scrapers.proxy?.flaresolverr?.enabled ?? false)}>
							{#snippet children()}
								<RefreshCw class={`h-4 w-4 mr-2 ${testingFlareSolverr ? 'animate-spin' : ''}`} />
								{testingFlareSolverr ? 'Testing FlareSolverr...' : 'Test FlareSolverr'}
							{/snippet}
						</Button>
					</div>
				</SettingsSubsection>

				<SettingsSubsection title="Download Proxy">
					<FormToggle
						label="Enable download proxy"
						description="Use a separate proxy for downloading covers, screenshots, and trailers"
						checked={config.output.download_proxy?.enabled ?? false}
						onchange={(val) => {
							if (!config.output.download_proxy) config.output.download_proxy = {};
							config.output.download_proxy.enabled = val;
						}}
					/>

					<FormTextInput
						label="Download proxy URL"
						description="Proxy server URL for downloads (leave empty to use no proxy for downloads)"
						value={config.output.download_proxy?.url ?? ""}
						placeholder="http://proxy.example.com:8080"
						disabled={!(config.output.download_proxy?.enabled ?? false)}
						onchange={(val) => {
							if (!config.output.download_proxy) config.output.download_proxy = {};
							config.output.download_proxy.url = val;
						}}
					/>

					<FormTextInput
						label="Download proxy username"
						description="Username for authenticated download proxy (optional)"
						value={config.output.download_proxy?.username ?? ""}
						placeholder=""
						disabled={!(config.output.download_proxy?.enabled ?? false)}
						onchange={(val) => {
							if (!config.output.download_proxy) config.output.download_proxy = {};
							config.output.download_proxy.username = val;
						}}
					/>

					<FormPasswordInput
						label="Download proxy password"
						description="Password for authenticated download proxy (optional)"
						value={config.output.download_proxy?.password ?? ""}
						disabled={!(config.output.download_proxy?.enabled ?? false)}
						onchange={(val) => {
							if (!config.output.download_proxy) config.output.download_proxy = {};
							config.output.download_proxy.password = val;
						}}
					/>
				</SettingsSubsection>
			</SettingsSection>

			<!-- Performance Settings -->
			<SettingsSection title="Performance Settings" description="Configure worker pool and performance tuning options" defaultExpanded={false}>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2" for="max-workers">
							Max Workers (concurrent tasks)
						</label>
						<input
							id="max-workers"
							type="number"
							bind:value={config.performance.max_workers}
							class={inputClass}
							min="1"
							max="20"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Higher values = faster but more resource intensive
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="worker-timeout">Worker Timeout (seconds)</label>
						<input
							id="worker-timeout"
							type="number"
							bind:value={config.performance.worker_timeout}
							class={inputClass}
							min="5"
							max="600"
						/>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="buffer-size">Buffer Size</label>
						<input
							id="buffer-size"
							type="number"
							bind:value={config.performance.buffer_size}
							class={inputClass}
							min="10"
							max="1000"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Channel buffer size for task communication
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="update-interval">UI Update Interval (ms)</label>
						<input
							id="update-interval"
							type="number"
							bind:value={config.performance.update_interval}
							class={inputClass}
							min="50"
							max="1000"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							How often to update the UI (lower = more responsive but more CPU)
						</p>
					</div>

				</div>
			</SettingsSection>

			<!-- File Matching Settings -->
			<SettingsSection title="File Matching Settings" description="Configure file scanning, extensions, and ID extraction patterns" defaultExpanded={false}>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2" for="file-extensions">File Extensions</label>
						<input
							id="file-extensions"
							type="text"
							value={config.file_matching.extensions.join(', ')}
							onchange={(e) => {
								config.file_matching.extensions = e.currentTarget.value
									.split(',')
									.map((s) => s.trim());
							}}
							class={inputClass}
							placeholder=".mp4, .mkv, .avi"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Comma-separated list of video file extensions to scan
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="min-size-mb">Minimum File Size (MB)</label>
						<input
							id="min-size-mb"
							type="number"
							bind:value={config.file_matching.min_size_mb}
							class={inputClass}
							min="0"
							max="10000"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Ignore files smaller than this (0 = no minimum)
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="exclude-patterns">Exclude Patterns</label>
						<input
							id="exclude-patterns"
							type="text"
							value={config.file_matching.exclude_patterns.join(', ')}
							onchange={(e) => {
								config.file_matching.exclude_patterns = e.currentTarget.value
									.split(',')
									.map((s) => s.trim())
									.filter((s) => s.length > 0);
							}}
							class={inputClass}
							placeholder="*-trailer*, *-sample*"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Glob patterns to exclude from scanning
						</p>
					</div>

					<div class="space-y-3">
						<label class="flex items-center gap-2">
							<input type="checkbox" bind:checked={config.file_matching.regex_enabled} class="rounded" />
							<span>Enable Custom Regex Pattern</span>
						</label>
					</div>

					{#if config.file_matching.regex_enabled}
						<div>
							<label class="block text-sm font-medium mb-2" for="regex-pattern">Regex Pattern</label>
							<input
								id="regex-pattern"
								type="text"
								bind:value={config.file_matching.regex_pattern}
								class="{inputClass} font-mono text-sm"
							/>
							<p class="text-xs text-muted-foreground mt-1">
								Custom regex pattern to extract movie IDs from filenames
							</p>
						</div>
					{/if}
				</div>
			</SettingsSection>

			<!-- Logging Settings -->
			<SettingsSection title="Logging Settings" description="Configure logging level, format, and output destination" defaultExpanded={false}>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2" for="log-level">Log Level</label>
						<select id="log-level" bind:value={config.logging.level} class={inputClass}>
							<option value="debug">Debug</option>
							<option value="info">Info</option>
							<option value="warn">Warning</option>
							<option value="error">Error</option>
						</select>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="log-format">Log Format</label>
						<select id="log-format" bind:value={config.logging.format} class={inputClass}>
							<option value="text">Text</option>
							<option value="json">JSON</option>
						</select>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2" for="log-output">Log Output</label>
						<input
							id="log-output"
							type="text"
							bind:value={config.logging.output}
							class={inputClass}
							placeholder="stdout or file path"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Use "stdout" for console or provide a file path
						</p>
					</div>
				</div>
			</SettingsSection>

			<!-- MediaInfo Settings -->
			<SettingsSection title="MediaInfo Settings" description="Configure MediaInfo CLI fallback for media file analysis" defaultExpanded={false}>
				<div class="space-y-4">
					<FormToggle
						label="Enable MediaInfo CLI"
						description="Enable MediaInfo CLI fallback when library-based parsing fails"
						checked={config.mediainfo?.cli_enabled ?? false}
						onchange={(val) => {
							if (!config.mediainfo) config.mediainfo = {};
							config.mediainfo.cli_enabled = val;
						}}
					/>

					<FormTextInput
						label="MediaInfo CLI path"
						description="Path to the mediainfo binary (default: 'mediainfo' from PATH)"
						value={config.mediainfo?.cli_path ?? "mediainfo"}
						placeholder="mediainfo"
						onchange={(val) => {
							if (!config.mediainfo) config.mediainfo = {};
							config.mediainfo.cli_path = val;
						}}
					/>

					<FormNumberInput
						label="CLI timeout"
						description="Maximum time to wait for MediaInfo CLI execution"
						value={config.mediainfo?.cli_timeout ?? 30}
						min={5}
						max={120}
						unit="seconds"
						onchange={(val) => {
							if (!config.mediainfo) config.mediainfo = {};
							config.mediainfo.cli_timeout = val;
						}}
					/>
				</div>
			</SettingsSection>
		{/if}
	</div>
</div>

<!-- Confirmation Modal -->
{#if showConfirmModal}
	<div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4 animate-fade-in">
		<Card class="w-full max-w-md animate-scale-in">
			<div class="p-6 space-y-4">
				<!-- Header -->
				<div class="flex items-start justify-between">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 bg-primary/10 rounded-full flex items-center justify-center">
							<AlertCircle class="h-5 w-5 text-primary" />
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
