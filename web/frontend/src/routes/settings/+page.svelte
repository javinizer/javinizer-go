<script lang="ts">
	import { onMount } from 'svelte';
	import { apiClient } from '$lib/api/client';
	import type { ScraperOption } from '$lib/api/types';
	import { Save, RefreshCw, AlertCircle, ArrowLeft, CheckCircle2, X, GripVertical, ChevronUp, ChevronDown, ChevronRight } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { toastStore } from '$lib/stores/toast';

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
	let error = $state<string | null>(null);
	let showConfirmModal = $state(false);
	let scrapers = $state<ScraperItem[]>([]);

	const inputClass =
		'w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background';

	// Build scraper list from config and API
	async function buildScraperList() {
		if (!config) return;

		try {
			// Fetch available scrapers from backend
			const response = await apiClient.getAvailableScrapers();
			console.log('API Response:', response);

			// Create maps from API data
			const scraperDisplayNames: Record<string, string> = {};
			const scraperOptionsMap: Record<string, ScraperOption[]> = {};

			response.scrapers.forEach(scraper => {
				scraperDisplayNames[scraper.name] = scraper.display_name;
				scraperOptionsMap[scraper.name] = scraper.options || [];
				console.log(`Scraper ${scraper.name} has ${scraper.options?.length || 0} options:`, scraper.options);
			});

			// Build scraper list based on priority order in config
			scrapers = config.Scrapers.Priority.map((name: string) => ({
				name,
				enabled: config.Scrapers[getConfigKey(name)]?.Enabled ?? false,
				displayName: scraperDisplayNames[name] || name,
				expanded: false,
				options: scraperOptionsMap[name] || []
			}));

			console.log('Built scrapers array:', scrapers);
		} catch (e) {
			console.error('Failed to fetch scrapers from API:', e);
			// Fallback to config-based list without display names or options
			scrapers = config.Scrapers.Priority.map((name: string) => ({
				name,
				enabled: config.Scrapers[getConfigKey(name)]?.Enabled ?? false,
				displayName: name,
				expanded: false,
				options: []
			}));
		}
	}

	// Check if scraper has options to show
	function scraperHasOptions(scraper: ScraperItem): boolean {
		const hasOptions = scraper.options && scraper.options.length > 0;
		console.log(`scraperHasOptions(${scraper.name}):`, hasOptions, 'options:', scraper.options);
		return hasOptions;
	}

	function toggleExpanded(index: number) {
		scrapers[index].expanded = !scrapers[index].expanded;
	}

	// Helper to convert scraper name to config key
	function getConfigKey(scraperName: string): string {
		// Special case for DMM - needs to be all uppercase
		if (scraperName === 'dmm') {
			return 'DMM';
		}
		// r18dev -> R18Dev
		return scraperName.charAt(0).toUpperCase() + scraperName.slice(1).replace('dev', 'Dev');
	}

	// Helper to get option value from config
	function getOptionValue(scraperName: string, optionKey: string): any {
		const configKey = getConfigKey(scraperName);
		// Convert snake_case to PascalCase (e.g., scrape_actress -> ScrapeActress)
		const pascalKey = optionKey.split('_').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join('');
		return config?.Scrapers?.[configKey]?.[pascalKey];
	}

	// Helper to set option value in config
	function setOptionValue(scraperName: string, optionKey: string, value: any) {
		const configKey = getConfigKey(scraperName);
		// Convert snake_case to PascalCase (e.g., scrape_actress -> ScrapeActress)
		const pascalKey = optionKey.split('_').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join('');
		if (config?.Scrapers?.[configKey]) {
			config.Scrapers[configKey][pascalKey] = value;
			// Trigger reactivity by reassigning the config object with a deep clone
			config = JSON.parse(JSON.stringify(config));
			console.log(`Set ${scraperName}.${optionKey} (${pascalKey}) to ${value} in config.Scrapers.${configKey}`);
			console.log('Updated config:', config.Scrapers[configKey]);
		}
	}

	// Update config from scraper list
	function updateConfigFromScrapers() {
		if (!config) return;

		// Update priority order
		config.Scrapers.Priority = scrapers.map(s => s.name);

		// Update enabled status
		scrapers.forEach(scraper => {
			const configKey = getConfigKey(scraper.name);
			if (config.Scrapers[configKey]) {
				config.Scrapers[configKey].Enabled = scraper.enabled;
			}
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
		scrapers[index].enabled = !scrapers[index].enabled;
		updateConfigFromScrapers();
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
			<Card class="p-6">
				<h2 class="text-xl font-semibold mb-4">Server Settings</h2>
				<div class="space-y-4">
					<div class="grid grid-cols-2 gap-4">
						<div>
							<label class="block text-sm font-medium mb-2">Host</label>
							<input type="text" bind:value={config.Server.Host} class={inputClass} placeholder="localhost" />
						</div>
						<div>
							<label class="block text-sm font-medium mb-2">Port</label>
							<input type="number" bind:value={config.Server.Port} class={inputClass} placeholder="8080" />
						</div>
					</div>
				</div>
			</Card>

			<!-- Scraper Settings -->
			<Card class="p-6">
				<h2 class="text-xl font-semibold mb-4">Scraper Settings</h2>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2">Scraper Priority & Status</label>
						<p class="text-sm text-muted-foreground mb-3">
							Scrapers are tried in order from top to bottom. Use arrows to reorder.
						</p>
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

										<!-- Move Buttons -->
										<div class="flex gap-1">
											<Button
												variant="ghost"
												size="icon"
												onclick={() => moveScraperUp(index)}
												disabled={index === 0}
											>
												{#snippet children()}
													<ChevronUp class="h-4 w-4" />
												{/snippet}
											</Button>
											<Button
												variant="ghost"
												size="icon"
												onclick={() => moveScraperDown(index)}
												disabled={index === scrapers.length - 1}
											>
												{#snippet children()}
													<ChevronDown class="h-4 w-4" />
												{/snippet}
											</Button>
										</div>
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
																	onchange={(e) => setOptionValue(scraper.name, option.key, e.currentTarget.checked)}
																	class="rounded"
																/>
																<span class="text-sm">{option.label}</span>
															</label>
															<p class="text-xs text-muted-foreground ml-6">
																{option.description}
															</p>
														{/if}
														<!-- Add more option types here as needed (string, number, etc.) -->
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
						<label class="block text-sm font-medium mb-2">User Agent</label>
						<input type="text" bind:value={config.Scrapers.UserAgent} class={inputClass} />
					</div>
				</div>
			</Card>

			<!-- Output Settings -->
			<Card class="p-6">
				<h2 class="text-xl font-semibold mb-4">Output Settings</h2>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2">Destination Path</label>
						<p class="text-sm text-muted-foreground mb-2">
							Note: Destination path is currently not configurable via API
						</p>
						<input
							type="text"
							value="Configured in config.yaml"
							class={inputClass}
							disabled
							placeholder="/path/to/output"
						/>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">Delimiter</label>
						<input
							type="text"
							bind:value={config.Output.Delimiter}
							class={inputClass}
							placeholder=", "
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Character(s) used to separate multiple values (e.g., actresses, genres)
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">Subfolder Format</label>
						<input
							type="text"
							value={config.Output.SubfolderFormat.join(', ')}
							onchange={(e) => {
								config.Output.SubfolderFormat = e.currentTarget.value
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
							<input type="checkbox" bind:checked={config.Output.DownloadPoster} class="rounded" />
							<span>Download Poster</span>
						</label>
						<label class="flex items-center gap-2">
							<input type="checkbox" bind:checked={config.Output.DownloadCover} class="rounded" />
							<span>Download Cover</span>
						</label>
						<label class="flex items-center gap-2">
							<input
								type="checkbox"
								bind:checked={config.Output.DownloadExtrafanart}
								class="rounded"
							/>
							<span>Download Extrafanart</span>
						</label>
						<label class="flex items-center gap-2">
							<input type="checkbox" bind:checked={config.Output.DownloadTrailer} class="rounded" />
							<span>Download Trailer</span>
						</label>
						<label class="flex items-center gap-2">
							<input type="checkbox" bind:checked={config.Output.DownloadActress} class="rounded" />
							<span>Download Actress Images</span>
						</label>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">Folder Naming Template</label>
						<input
							type="text"
							bind:value={config.Output.FolderFormat}
							class="{inputClass} font-mono text-sm"
							placeholder="<ID> - <TITLE>"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Available tags: &lt;ID&gt;, &lt;TITLE&gt;, &lt;STUDIO&gt;, &lt;YEAR&gt;, &lt;ACTRESS&gt;
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">File Naming Template</label>
						<input
							type="text"
							bind:value={config.Output.FileFormat}
							class="{inputClass} font-mono text-sm"
							placeholder="<ID>"
						/>
					</div>
				</div>
			</Card>

			<!-- Database Settings -->
			<Card class="p-6">
				<h2 class="text-xl font-semibold mb-4">Database Settings</h2>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2">Database Path (DSN)</label>
						<input
							type="text"
							bind:value={config.Database.DSN}
							class={inputClass}
							placeholder="data/javinizer.db"
						/>
					</div>
				</div>
			</Card>

			<!-- Performance Settings -->
			<Card class="p-6">
				<h2 class="text-xl font-semibold mb-4">Performance Settings</h2>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2">
							Max Workers (concurrent tasks)
						</label>
						<input
							type="number"
							bind:value={config.Performance.MaxWorkers}
							class={inputClass}
							min="1"
							max="20"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Higher values = faster but more resource intensive
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">Worker Timeout (seconds)</label>
						<input
							type="number"
							bind:value={config.Performance.WorkerTimeout}
							class={inputClass}
							min="5"
							max="600"
						/>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">Buffer Size</label>
						<input
							type="number"
							bind:value={config.Performance.BufferSize}
							class={inputClass}
							min="10"
							max="1000"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Channel buffer size for task communication
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">UI Update Interval (ms)</label>
						<input
							type="number"
							bind:value={config.Performance.UpdateInterval}
							class={inputClass}
							min="50"
							max="1000"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							How often to update the UI (lower = more responsive but more CPU)
						</p>
					</div>

				</div>
			</Card>

			<!-- File Matching Settings -->
			<Card class="p-6">
				<h2 class="text-xl font-semibold mb-4">File Matching Settings</h2>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2">File Extensions</label>
						<input
							type="text"
							value={config.Matching.Extensions.join(', ')}
							onchange={(e) => {
								config.Matching.Extensions = e.currentTarget.value
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
						<label class="block text-sm font-medium mb-2">Minimum File Size (MB)</label>
						<input
							type="number"
							bind:value={config.Matching.MinSizeMB}
							class={inputClass}
							min="0"
							max="10000"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Ignore files smaller than this (0 = no minimum)
						</p>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">Exclude Patterns</label>
						<input
							type="text"
							value={config.Matching.ExcludePatterns.join(', ')}
							onchange={(e) => {
								config.Matching.ExcludePatterns = e.currentTarget.value
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
							<input type="checkbox" bind:checked={config.Matching.RegexEnabled} class="rounded" />
							<span>Enable Custom Regex Pattern</span>
						</label>
					</div>

					{#if config.Matching.RegexEnabled}
						<div>
							<label class="block text-sm font-medium mb-2">Regex Pattern</label>
							<input
								type="text"
								bind:value={config.Matching.RegexPattern}
								class="{inputClass} font-mono text-sm"
							/>
							<p class="text-xs text-muted-foreground mt-1">
								Custom regex pattern to extract movie IDs from filenames
							</p>
						</div>
					{/if}
				</div>
			</Card>

			<!-- Logging Settings -->
			<Card class="p-6">
				<h2 class="text-xl font-semibold mb-4">Logging Settings</h2>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2">Log Level</label>
						<select bind:value={config.Logging.Level} class={inputClass}>
							<option value="debug">Debug</option>
							<option value="info">Info</option>
							<option value="warn">Warning</option>
							<option value="error">Error</option>
						</select>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">Log Format</label>
						<select bind:value={config.Logging.Format} class={inputClass}>
							<option value="text">Text</option>
							<option value="json">JSON</option>
						</select>
					</div>

					<div>
						<label class="block text-sm font-medium mb-2">Log Output</label>
						<input
							type="text"
							bind:value={config.Logging.Output}
							class={inputClass}
							placeholder="stdout or file path"
						/>
						<p class="text-xs text-muted-foreground mt-1">
							Use "stdout" for console or provide a file path
						</p>
					</div>
				</div>
			</Card>
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
