<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fade, slide } from 'svelte/transition';
	import { ChevronDown, ChevronRight } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import type { SettingsConfig, ScraperOption, ScraperSettings } from '$lib/api/types';

	type ScraperProxyMode = 'direct' | 'inherit' | 'specific';

	interface ScraperItem {
		name: string;
		enabled: boolean;
		displayName: string;
		expanded: boolean;
		options: ScraperOption[];
	}

	interface Props {
		config: SettingsConfig;
		scrapers: ScraperItem[];
		inputClass: string;
		scraperHasOptions: (scraper: ScraperItem) => boolean;
		onScraperRowClick: (event: MouseEvent, index: number) => void;
		onScraperRowKeydown: (event: KeyboardEvent, index: number) => void;
		toggleScraper: (index: number) => void;
		toggleExpanded: (index: number) => void;
		selectAllScrapers: () => void;
		clearAllScrapers: () => void;
		getScraperUsage: (scraperName: string) => { count: number; fields: string[] };
		scraperSupportsProxyOptions: (scraper: ScraperItem) => boolean;
		getScraperProxyMode: (scraperName: string) => ScraperProxyMode;
		setScraperProxyMode: (scraperName: string, mode: ScraperProxyMode) => void;
		getProxyProfileNames: () => string[];
		setOptionValue: (scraperName: string, optionKey: string, value: string | number | boolean) => void;
		getRenderableScraperOptions: (scraper: ScraperItem) => ScraperOption[];
		isOptionDisabled: (scraperName: string, optionKey: string) => boolean;
		getOptionValue: (scraperName: string, optionKey: string) => string | number | boolean | undefined;
		parseOptionNumber: (value: string) => number | undefined;
	}

	let {
		config,
		scrapers,
		inputClass,
		scraperHasOptions,
		onScraperRowClick,
		onScraperRowKeydown,
		toggleScraper,
		toggleExpanded,
		selectAllScrapers,
		clearAllScrapers,
		getScraperUsage,
		scraperSupportsProxyOptions,
		getScraperProxyMode,
		setScraperProxyMode,
		getProxyProfileNames,
		setOptionValue,
		getRenderableScraperOptions,
		isOptionDisabled,
		getOptionValue,
		parseOptionNumber
	}: Props = $props();
</script>

<SettingsSection title="Scraper Settings" description="Enable/disable scrapers and configure user agent. Scraper priority is managed in Metadata Priority section." defaultExpanded={false}>
	<div class="space-y-4">
		<div>
			<div class="flex items-center justify-between mb-2">
				<span class="text-sm font-medium">Available Scrapers</span>
				<div class="flex gap-2">
					<Button variant="outline" size="sm" onclick={selectAllScrapers}>
						{#snippet children()}Select All{/snippet}
					</Button>
					<Button variant="outline" size="sm" onclick={clearAllScrapers}>
						{#snippet children()}Clear All{/snippet}
					</Button>
				</div>
			</div>
			<p class="text-xs text-muted-foreground mb-3">
				Global proxy must be enabled for scraper proxy settings to take effect.
				Each scraper can be set to Direct (bypass), Inherit (global default), or Specific (custom profile).
			</p>
			<div class="space-y-2">
				{#each scrapers as scraper, index (scraper.name)}
					<div class="rounded-lg border {scraper.enabled ? 'bg-background' : 'bg-muted/30'}" animate:flip={{ duration: 250, easing: quintOut }}>
						<div
							class="flex items-center gap-3 p-3 {scraper.enabled && scraperHasOptions(scraper) ? 'cursor-pointer hover:bg-muted/30' : ''}"
							role="button"
							tabindex="0"
							onclick={(event) => onScraperRowClick(event, index)}
							onkeydown={(e) => onScraperRowKeydown(e, index)}
						>
							<input
								type="checkbox"
								checked={scraper.enabled}
								onclick={(e) => e.stopPropagation()}
								onchange={() => toggleScraper(index)}
								class="rounded"
							/>

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

							{#if scraper.enabled && scraperHasOptions(scraper)}
								<Button variant="ghost" size="icon" onclick={() => toggleExpanded(index)} class="h-8 w-8">
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

						{#if scraper.enabled && scraper.expanded && scraperHasOptions(scraper)}
							<div class="px-3 pb-3 pt-0 border-t bg-muted/20" transition:slide|local={{ duration: 220, easing: quintOut }}>
								<div class="pl-8 py-3 space-y-3" in:fade|local={{ duration: 170 }}>
									<h4 class="text-sm font-medium">{scraper.displayName} Options</h4>
									{#if scraperSupportsProxyOptions(scraper)}
										<div class="rounded-md border border-border/80 bg-background/70 p-3 space-y-3">
											<div>
												<p class="text-sm font-medium">Proxy Routing</p>
												<p class="text-xs text-muted-foreground mt-1">
													Global proxy must be enabled for these settings to apply.
												</p>
											</div>

											<div class="grid gap-3 md:grid-cols-2">
												<div>
													<label class="block text-sm font-medium mb-1" for="proxy-mode-{scraper.name}">Proxy mode</label>
													<select
														id="proxy-mode-{scraper.name}"
														value={getScraperProxyMode(scraper.name)}
														onchange={(e) => setScraperProxyMode(scraper.name, e.currentTarget.value as ScraperProxyMode)}
														class="w-full px-3 py-2 border rounded-md transition-all text-sm bg-background focus:ring-2 focus:ring-primary focus:border-primary"
													>
														<option value="direct">Direct (No proxy)</option>
														<option value="inherit">Inherit Global Proxy</option>
														<option value="specific">Use Scraper Profile</option>
													</select>
													<p class="text-xs text-muted-foreground mt-1">
														{#if !(config.scrapers?.proxy?.enabled ?? false)}
															<span class="text-amber-600">⚠ Global proxy is disabled. This scraper will use Direct mode regardless of setting above.</span>
														{:else if getScraperProxyMode(scraper.name) === 'direct'}
															This scraper will bypass proxy and connect directly.
														{:else if getScraperProxyMode(scraper.name) === 'inherit'}
															Uses global proxy default profile.
														{:else}
															Uses the scraper-specific profile selected below.
														{/if}
													</p>
												</div>

												<div class={getScraperProxyMode(scraper.name) === 'specific' ? '' : 'opacity-60'}>
													<label class="block text-sm font-medium mb-1" for="proxy-profile-{scraper.name}">Scraper profile</label>
													<select
														id="proxy-profile-{scraper.name}"
														value={(config.scrapers?.[scraper.name] as ScraperSettings)?.proxy?.profile ?? ''}
														disabled={getScraperProxyMode(scraper.name) !== 'specific'}
														onchange={(e) => setOptionValue(scraper.name, 'proxy.profile', e.currentTarget.value)}
														class="w-full px-3 py-2 border rounded-md transition-all text-sm {getScraperProxyMode(scraper.name) === 'specific' ? 'bg-background focus:ring-2 focus:ring-primary focus:border-primary' : 'bg-muted/70 text-muted-foreground border-border/60 cursor-not-allowed'}"
													>
														<option value="">Select profile</option>
														{#each getProxyProfileNames() as profileName}
															<option value={profileName}>{profileName}</option>
														{/each}
													</select>
													<p class="text-xs text-muted-foreground mt-1">
														Only used when Proxy mode is "Use Scraper Profile".
													</p>
												</div>
											</div>
										</div>
									{/if}

									{#each getRenderableScraperOptions(scraper).filter(opt => opt.key !== 'use_browser' || scraper.name === 'dmm') as option}
										{@const optionDisabled = isOptionDisabled(scraper.name, option.key)}
										<div class="space-y-1">
											{#if option.type === 'boolean'}
												<label class="flex items-center gap-2">
													<input
														type="checkbox"
														checked={!!getOptionValue(scraper.name, option.key)}
														disabled={optionDisabled}
														onchange={(e) => setOptionValue(scraper.name, option.key, e.currentTarget.checked)}
														class="rounded"
													/>
													<span class="text-sm {optionDisabled ? 'text-muted-foreground' : ''}">{option.label}</span>
												</label>
												<p class="text-xs text-muted-foreground ml-6">
													{option.description}
													{#if optionDisabled && option.key === 'use_browser'}
														<span class="text-amber-600 block mt-1">⚠ Enable 'Use Browser' in global settings to use this option.</span>
													{:else if optionDisabled && option.key === 'scrape_actress'}
														<span class="text-amber-600 block mt-1">⚠ Enable 'Scrape Actress Information' in global settings to use this option.</span>
													{/if}
												</p>
											{:else if option.type === 'select'}
												<div class={optionDisabled ? 'opacity-60' : ''}>
													<label class="block text-sm font-medium mb-1 {optionDisabled ? 'text-muted-foreground' : ''}" for="option-{scraper.name}-{option.key}">{option.label}</label>
													<select
														id="option-{scraper.name}-{option.key}"
														value={getOptionValue(scraper.name, option.key) ?? ''}
														disabled={optionDisabled}
														onchange={(e) => setOptionValue(scraper.name, option.key, e.currentTarget.value)}
														class="w-48 px-3 py-2 border rounded-md transition-all text-sm {optionDisabled ? 'bg-muted/70 text-muted-foreground border-border/60 cursor-not-allowed' : 'focus:ring-2 focus:ring-primary focus:border-primary bg-background'}"
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
														disabled={optionDisabled}
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
															disabled={optionDisabled}
															oninput={(e) => setOptionValue(scraper.name, option.key, parseOptionNumber(e.currentTarget.value) ?? 0)}
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

	</div>
</SettingsSection>
