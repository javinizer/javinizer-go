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
			{ key: 'ID', label: 'Movie ID' },
			{ key: 'Title', label: 'Title' },
			{ key: 'OriginalTitle', label: 'Original Title' },
			{ key: 'Description', label: 'Description' },
			{ key: 'ReleaseDate', label: 'Release Date' },
			{ key: 'Runtime', label: 'Runtime' },
			{ key: 'ContentID', label: 'Content ID' },
			{ key: 'Actress', label: 'Actresses' },
			{ key: 'Genre', label: 'Genres' },
			{ key: 'Director', label: 'Director' },
			{ key: 'Maker', label: 'Studio/Maker' },
			{ key: 'Label', label: 'Label' },
			{ key: 'Series', label: 'Series' },
			{ key: 'Rating', label: 'Rating' },
			{ key: 'CoverURL', label: 'Cover Image' },
			{ key: 'PosterURL', label: 'Poster Image' },
			{ key: 'ScreenshotURL', label: 'Screenshots' },
			{ key: 'TrailerURL', label: 'Trailer' }
		];

		const globalPriority = config?.Scrapers?.Priority || [];
		const fieldsUsing: string[] = [];

		metadataFields.forEach((field) => {
			// Check if field has custom priority
			const fieldPriority = config?.Metadata?.Priority?.[field.key];
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
		if (config.Scrapers?.Priority) {
			config.Scrapers.Priority = config.Scrapers.Priority.filter((s: string) => s !== scraperName);
		}

		// Remove from all field-specific priorities
		if (config.Metadata?.Priority) {
			Object.keys(config.Metadata.Priority).forEach((fieldKey) => {
				config.Metadata.Priority[fieldKey] = config.Metadata.Priority[fieldKey].filter(
					(s: string) => s !== scraperName
				);

				// Clean up empty arrays
				if (config.Metadata.Priority[fieldKey].length === 0) {
					delete config.Metadata.Priority[fieldKey];
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
						<label class="block text-sm font-medium mb-2">Host</label>
						<input type="text" bind:value={config.Server.Host} class={inputClass} placeholder="localhost" />
					</div>
					<div>
						<label class="block text-sm font-medium mb-2">Port</label>
						<input type="number" bind:value={config.Server.Port} class={inputClass} placeholder="8080" />
					</div>
				</div>
			</SettingsSection>

			<!-- Scraper Settings -->
			<SettingsSection title="Scraper Settings" description="Enable/disable scrapers and configure user agent. Scraper priority is managed in Metadata Priority section." defaultExpanded={false}>
				<div class="space-y-4">
					<div>
						<label class="block text-sm font-medium mb-2">Available Scrapers</label>
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
																	onchange={(e) => setOptionValue(scraper.name, option.key, e.currentTarget.checked)}
																	class="rounded"
																/>
																<span class="text-sm">{option.label}</span>
															</label>
															<p class="text-xs text-muted-foreground ml-6">
																{option.description}
															</p>
														{:else if option.type === 'number'}
															<div>
																<label class="block text-sm font-medium mb-1">{option.label}</label>
																<div class="flex items-center gap-2">
																	<input
																		type="number"
																		value={getOptionValue(scraper.name, option.key)}
																		oninput={(e) => setOptionValue(scraper.name, option.key, parseInt(e.currentTarget.value))}
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
						<label class="block text-sm font-medium mb-2">User Agent</label>
						<input type="text" bind:value={config.Scrapers.UserAgent} class={inputClass} />
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
					checked={config.Output.MoveToFolder ?? true}
					onchange={(val) => { config.Output.MoveToFolder = val; }}
				/>

				<FormToggle
					label="Rename file"
					description="Rename video files according to the file naming template"
					checked={config.Output.RenameFile ?? true}
					onchange={(val) => { config.Output.RenameFile = val; }}
				/>

				<FormToggle
					label="Rename folder in place"
					description="Rename the parent folder without moving files to a new location"
					checked={config.Output.RenameFolderInPlace ?? false}
					onchange={(val) => { config.Output.RenameFolderInPlace = val; }}
				/>

				<SettingsSubsection title="Subtitle Handling">
					<FormToggle
						label="Move subtitles"
						description="Automatically move subtitle files (.srt, .ass, etc.) with video files"
						checked={config.Output.MoveSubtitles ?? false}
						onchange={(val) => { config.Output.MoveSubtitles = val; }}
					/>

					<FormTextInput
						label="Subtitle extensions"
						description="Comma-separated list of subtitle file extensions to move with videos"
						value={config.Output.SubtitleExtensions?.join(', ') ?? ".srt, .ass, .ssa, .sub, .vtt"}
						placeholder=".srt, .ass, .ssa, .sub, .vtt"
						onchange={(val) => {
							config.Output.SubtitleExtensions = val.split(',').map(s => s.trim()).filter(s => s.length > 0);
						}}
					/>
				</SettingsSubsection>
			</SettingsSection>

			<!-- Output Settings -->
			<SettingsSection title="Output Settings" description="Configure output paths, templates, and download options" defaultExpanded={false}>
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

					<SettingsSubsection title="Template Options">
						<FormNumberInput
							label="Max title length"
							description="Maximum characters for movie titles in folder names. Longer titles will be intelligently truncated."
							value={config.Output.MaxTitleLength ?? 100}
							min={10}
							max={500}
							unit="characters"
							onchange={(val) => { config.Output.MaxTitleLength = val; }}
						/>

						<FormNumberInput
							label="Max path length"
							description="Maximum total path length to prevent Windows path errors (MAX_PATH = 260)"
							value={config.Output.MaxPathLength ?? 240}
							min={100}
							max={250}
							unit="characters"
							onchange={(val) => { config.Output.MaxPathLength = val; }}
						/>

						<FormToggle
							label="Group actress"
							description="Group actress names with @ prefix (e.g., '@GroupName')"
							checked={config.Output.GroupActress ?? false}
							onchange={(val) => { config.Output.GroupActress = val; }}
						/>

						<div class="py-4 border-b border-border">
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
					</SettingsSubsection>

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

					<FormNumberInput
						label="Download timeout"
						description="Maximum time to wait for image/video downloads to complete"
						value={config.Output.DownloadTimeout ?? 60}
						min={5}
						max={600}
						unit="seconds"
						onchange={(val) => { config.Output.DownloadTimeout = val; }}
					/>

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

					<SettingsSubsection title="Media File Naming">
						<FormTemplateInput
							label="Poster format"
							description="Naming template for poster images"
							value={config.Output.PosterFormat ?? "<ID>-poster.jpg"}
							placeholder="<ID>-poster.jpg"
							showTagList={true}
							onchange={(val) => { config.Output.PosterFormat = val; }}
						/>

						<FormTemplateInput
							label="Fanart format"
							description="Naming template for fanart/cover images"
							value={config.Output.FanartFormat ?? "<ID>-fanart.jpg"}
							placeholder="<ID>-fanart.jpg"
							onchange={(val) => { config.Output.FanartFormat = val; }}
						/>

						<FormTemplateInput
							label="Trailer format"
							description="Naming template for trailer videos"
							value={config.Output.TrailerFormat ?? "<ID>-trailer.mp4"}
							placeholder="<ID>-trailer.mp4"
							onchange={(val) => { config.Output.TrailerFormat = val; }}
						/>

						<FormTemplateInput
							label="Screenshot format"
							description="Naming template for screenshot images"
							value={config.Output.ScreenshotFormat ?? "fanart"}
							placeholder="fanart"
							onchange={(val) => { config.Output.ScreenshotFormat = val; }}
						/>

						<FormTextInput
							label="Screenshot folder"
							description="Folder name for storing screenshot images"
							value={config.Output.ScreenshotFolder ?? "extrafanart"}
							placeholder="extrafanart"
							onchange={(val) => { config.Output.ScreenshotFolder = val; }}
						/>

						<FormNumberInput
							label="Screenshot padding"
							description="Zero-padding for screenshot numbers (e.g., 01, 02, 03)"
							value={config.Output.ScreenshotPadding ?? 1}
							min={1}
							max={5}
							unit="digits"
							onchange={(val) => { config.Output.ScreenshotPadding = val; }}
						/>

						<FormTextInput
							label="Actress folder"
							description="Folder name for storing actress images"
							value={config.Output.ActressFolder ?? ".actors"}
							placeholder=".actors"
							onchange={(val) => { config.Output.ActressFolder = val; }}
						/>

					<FormTemplateInput
						label="Actress format"
						description="Naming template for actress image files"
						value={config.Output.ActressFormat ?? "<ACTORNAME>.jpg"}
						placeholder="<ACTORNAME>.jpg"
						onchange={(val) => { config.Output.ActressFormat = val; }}
					/>
					</SettingsSubsection>
				</div>
			</SettingsSection>

			<!-- Database Settings -->
			<SettingsSection title="Database Settings" description="Configure database options and behavior" defaultExpanded={false}>
				<div class="mb-4">
					<label class="block text-sm font-medium mb-2">Database Type</label>
					<select bind:value={config.Database.Type} class={inputClass}>
						<option value="sqlite">SQLite</option>
						<option value="postgres">PostgreSQL</option>
						<option value="mysql">MySQL</option>
					</select>
					<p class="text-xs text-muted-foreground mt-1">
						Database engine to use (SQLite recommended for most users)
					</p>
				</div>

				<div class="mb-4">
					<label class="block text-sm font-medium mb-2">Database Path (DSN)</label>
					<input
						type="text"
						bind:value={config.Database.DSN}
						class={inputClass}
						placeholder="data/javinizer.db"
					/>
				</div>

				<SettingsSubsection title="Actress Database">
					<FormToggle
						label="Auto-add actresses"
						description="Automatically add new actresses to the database when encountered"
						checked={config.Metadata.ActressDatabase?.AutoAdd ?? false}
						onchange={(val) => {
							if (!config.Metadata.ActressDatabase) config.Metadata.ActressDatabase = {};
							config.Metadata.ActressDatabase.AutoAdd = val;
						}}
					/>

					<FormToggle
						label="Convert aliases"
						description="Use actress aliases from the database when generating metadata"
						checked={config.Metadata.ActressDatabase?.ConvertAlias ?? false}
						onchange={(val) => {
							if (!config.Metadata.ActressDatabase) config.Metadata.ActressDatabase = {};
							config.Metadata.ActressDatabase.ConvertAlias = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Genre Replacement">
					<FormToggle
						label="Auto-add genres"
						description="Automatically add new genre replacements to the database"
						checked={config.Metadata.GenreReplacement?.AutoAdd ?? false}
						onchange={(val) => {
							if (!config.Metadata.GenreReplacement) config.Metadata.GenreReplacement = {};
							config.Metadata.GenreReplacement.AutoAdd = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Tag Database">
					<FormToggle
						label="Enable tag database"
						description="Enable per-movie tag lookup from database"
						checked={config.Metadata.TagDatabase?.Enabled ?? false}
						onchange={(val) => {
							if (!config.Metadata.TagDatabase) config.Metadata.TagDatabase = {};
							config.Metadata.TagDatabase.Enabled = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Advanced Metadata Options">
					<FormTextInput
						label="Ignore genres"
						description="Comma-separated list of genres to exclude from metadata"
						value={config.Metadata.IgnoreGenres?.join(', ') ?? ""}
						placeholder="e.g., Sample, Trailer"
						onchange={(val) => {
							config.Metadata.IgnoreGenres = val.split(',').map(s => s.trim()).filter(s => s.length > 0);
						}}
					/>

					<FormTextInput
						label="Required fields"
						description="Comma-separated list of required metadata fields (scraping fails if missing)"
						value={config.Metadata.RequiredFields?.join(', ') ?? ""}
						placeholder="e.g., title, actress, studio"
						onchange={(val) => {
							config.Metadata.RequiredFields = val.split(',').map(s => s.trim()).filter(s => s.length > 0);
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
						checked={config.Metadata.NFO?.Enabled ?? true}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.Enabled = val;
						}}
					/>

					<FormToggle
						label="NFO per file"
						description="Create separate NFO files for each video file (useful for multi-part movies)"
						checked={config.Metadata.NFO?.PerFile ?? false}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.PerFile = val;
						}}
					/>

					<FormTemplateInput
						label="Display name template"
						description="Template for the <title> field in NFO files"
						value={config.Metadata.NFO?.DisplayName ?? "[<ID>] <TITLE>"}
						placeholder="[<ID>] <TITLE>"
						showTagList={true}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.DisplayName = val;
						}}
					/>

					<FormTemplateInput
						label="Filename template"
						description="Template for NFO filenames"
						value={config.Metadata.NFO?.FilenameTemplate ?? "<ID>"}
						placeholder="<ID>"
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.FilenameTemplate = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Actress Settings">
					<FormToggle
						label="First name order"
						description="Use first-name-first order for actress names (Western style)"
						checked={config.Metadata.NFO?.FirstNameOrder ?? false}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.FirstNameOrder = val;
						}}
					/>

					<FormToggle
						label="Japanese actress names"
						description="Use Japanese names for actresses in NFO files"
						checked={config.Metadata.NFO?.ActressLanguageJA ?? false}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.ActressLanguageJA = val;
						}}
					/>

					<FormTextInput
						label="Unknown actress text"
						description="Text to display when actress information is unavailable"
						value={config.Metadata.NFO?.UnknownActressText ?? "Unknown"}
						placeholder="Unknown"
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.UnknownActressText = val;
						}}
					/>

					<FormToggle
						label="Actress as tag"
						description="Include actress names in the <tag> field"
						checked={config.Metadata.NFO?.ActressAsTag ?? false}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.ActressAsTag = val;
						}}
					/>

					<FormToggle
						label="Add generic role"
						description="Add 'Actress' as a generic role for all performers"
						checked={config.Metadata.NFO?.AddGenericRole ?? false}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.AddGenericRole = val;
						}}
					/>

					<FormToggle
						label="Use alternate name for role"
						description="Use actress alternate names in <role> field"
						checked={config.Metadata.NFO?.AltNameRole ?? false}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.AltNameRole = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Media Information">
					<FormToggle
						label="Include stream details"
						description="Include video/audio codec information from MediaInfo analysis"
						checked={config.Metadata.NFO?.IncludeStreamDetails ?? false}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.IncludeStreamDetails = val;
						}}
					/>

					<FormToggle
						label="Include fanart"
						description="Include fanart/cover image reference in NFO"
						checked={config.Metadata.NFO?.IncludeFanart ?? true}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.IncludeFanart = val;
						}}
					/>

					<FormToggle
						label="Include trailer"
						description="Include trailer video reference in NFO"
						checked={config.Metadata.NFO?.IncludeTrailer ?? true}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.IncludeTrailer = val;
						}}
					/>

					<FormTextInput
						label="Rating source"
						description="Source name for movie ratings (e.g., 'r18dev', 'dmm')"
						value={config.Metadata.NFO?.RatingSource ?? "r18dev"}
						placeholder="r18dev"
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.RatingSource = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Advanced NFO Options">
					<FormToggle
						label="Include original path"
						description="Include the original file path in NFO metadata"
						checked={config.Metadata.NFO?.IncludeOriginalPath ?? false}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.IncludeOriginalPath = val;
						}}
					/>

					<FormTemplateInput
						label="Tag template"
						description="Template for custom tags in NFO files"
						value={config.Metadata.NFO?.Tag ?? "<SET>"}
						placeholder="<SET>"
						showTagList={true}
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.Tag = val;
						}}
					/>

					<FormTemplateInput
						label="Tagline template"
						description="Template for the tagline field in NFO files"
						value={config.Metadata.NFO?.Tagline ?? ""}
						placeholder=""
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.Tagline = val;
						}}
					/>

					<FormTextInput
						label="Credits"
						description="Credits to include in NFO (comma-separated)"
						value={config.Metadata.NFO?.Credits?.join(', ') ?? ""}
						placeholder="Director Name, Studio Name"
						onchange={(val) => {
							if (!config.Metadata.NFO) config.Metadata.NFO = {};
							config.Metadata.NFO.Credits = val.split(',').map(s => s.trim()).filter(s => s.length > 0);
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
						checked={config.Scrapers.Proxy?.Enabled ?? false}
						onchange={(val) => {
							if (!config.Scrapers.Proxy) config.Scrapers.Proxy = {};
							config.Scrapers.Proxy.Enabled = val;
						}}
					/>

					<FormTextInput
						label="Proxy URL"
						description="Proxy server URL (e.g., http://proxy.example.com:8080 or socks5://localhost:1080)"
						value={config.Scrapers.Proxy?.URL ?? ""}
						placeholder="http://proxy.example.com:8080"
						onchange={(val) => {
							if (!config.Scrapers.Proxy) config.Scrapers.Proxy = {};
							config.Scrapers.Proxy.URL = val;
						}}
					/>

					<FormTextInput
						label="Proxy username"
						description="Username for authenticated proxy (optional)"
						value={config.Scrapers.Proxy?.Username ?? ""}
						placeholder=""
						onchange={(val) => {
							if (!config.Scrapers.Proxy) config.Scrapers.Proxy = {};
							config.Scrapers.Proxy.Username = val;
						}}
					/>

					<FormPasswordInput
						label="Proxy password"
						description="Password for authenticated proxy (optional)"
						value={config.Scrapers.Proxy?.Password ?? ""}
						onchange={(val) => {
							if (!config.Scrapers.Proxy) config.Scrapers.Proxy = {};
							config.Scrapers.Proxy.Password = val;
						}}
					/>
				</SettingsSubsection>

				<SettingsSubsection title="Download Proxy">
					<FormToggle
						label="Enable download proxy"
						description="Use a separate proxy for downloading covers, screenshots, and trailers"
						checked={config.Output.DownloadProxy?.Enabled ?? false}
						onchange={(val) => {
							if (!config.Output.DownloadProxy) config.Output.DownloadProxy = {};
							config.Output.DownloadProxy.Enabled = val;
						}}
					/>

					<FormTextInput
						label="Download proxy URL"
						description="Proxy server URL for downloads (leave empty to use no proxy for downloads)"
						value={config.Output.DownloadProxy?.URL ?? ""}
						placeholder="http://proxy.example.com:8080"
						onchange={(val) => {
							if (!config.Output.DownloadProxy) config.Output.DownloadProxy = {};
							config.Output.DownloadProxy.URL = val;
						}}
					/>

					<FormTextInput
						label="Download proxy username"
						description="Username for authenticated download proxy (optional)"
						value={config.Output.DownloadProxy?.Username ?? ""}
						placeholder=""
						onchange={(val) => {
							if (!config.Output.DownloadProxy) config.Output.DownloadProxy = {};
							config.Output.DownloadProxy.Username = val;
						}}
					/>

					<FormPasswordInput
						label="Download proxy password"
						description="Password for authenticated download proxy (optional)"
						value={config.Output.DownloadProxy?.Password ?? ""}
						onchange={(val) => {
							if (!config.Output.DownloadProxy) config.Output.DownloadProxy = {};
							config.Output.DownloadProxy.Password = val;
						}}
					/>
				</SettingsSubsection>
			</SettingsSection>

			<!-- Performance Settings -->
			<SettingsSection title="Performance Settings" description="Configure worker pool and performance tuning options" defaultExpanded={false}>
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
			</SettingsSection>

			<!-- File Matching Settings -->
			<SettingsSection title="File Matching Settings" description="Configure file scanning, extensions, and ID extraction patterns" defaultExpanded={false}>
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
			</SettingsSection>

			<!-- Logging Settings -->
			<SettingsSection title="Logging Settings" description="Configure logging level, format, and output destination" defaultExpanded={false}>
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
			</SettingsSection>

			<!-- MediaInfo Settings -->
			<SettingsSection title="MediaInfo Settings" description="Configure MediaInfo CLI fallback for media file analysis" defaultExpanded={false}>
				<div class="space-y-4">
					<FormToggle
						label="Enable MediaInfo CLI"
						description="Enable MediaInfo CLI fallback when library-based parsing fails"
						checked={config.MediaInfo?.CLIEnabled ?? false}
						onchange={(val) => {
							if (!config.MediaInfo) config.MediaInfo = {};
							config.MediaInfo.CLIEnabled = val;
						}}
					/>

					<FormTextInput
						label="MediaInfo CLI path"
						description="Path to the mediainfo binary (default: 'mediainfo' from PATH)"
						value={config.MediaInfo?.CLIPath ?? "mediainfo"}
						placeholder="mediainfo"
						onchange={(val) => {
							if (!config.MediaInfo) config.MediaInfo = {};
							config.MediaInfo.CLIPath = val;
						}}
					/>

					<FormNumberInput
						label="CLI timeout"
						description="Maximum time to wait for MediaInfo CLI execution"
						value={config.MediaInfo?.CLITimeout ?? 30}
						min={5}
						max={120}
						unit="seconds"
						onchange={(val) => {
							if (!config.MediaInfo) config.MediaInfo = {};
							config.MediaInfo.CLITimeout = val;
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
