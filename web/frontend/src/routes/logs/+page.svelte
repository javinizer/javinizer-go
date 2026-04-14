<script lang="ts">
	import { onMount } from 'svelte';
	import { fade, fly, slide } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import {
		AlertTriangle,
		RefreshCw,
		Filter,
		X,
		Search,
		Loader2,
		Calendar,
		Clock,
		CircleAlert,
		Info,
		Bug,
		ChevronDown,
		ChevronRight,
		Zap,
		Pause,
		Activity,
		FileText
	} from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import type { EventItem, EventStatsResponse, HealthResponse } from '$lib/api/types';

	let containerEl: HTMLElement;
	let events = $state<EventItem[]>([]);
	let stats = $state<EventStatsResponse | null>(null);
	let health = $state<HealthResponse | null>(null);
	let loading = $state(true);
	let loadingMore = $state(false);
	let hasLoadedOnce = $state(false);
	let isRefreshing = $state(false);
	let error = $state<string | null>(null);
	let listRenderVersion = $state(0);
	let activeTypeFilter = $state<string>('all');
	let activeSeverityFilter = $state<string>('all');
	let startDate = $state<string>('');
	let endDate = $state<string>('');
	let searchText = $state<string>('');
	let chipFilter = $state<{ field: string; value: string } | null>(null);
	let total = $state(0);
	let offset = $state(0);
	const limit = 50;
	let hasMore = $state(false);
	let expandedEvents = $state<Set<number>>(new Set());
	let isLiveMode = $state(false);
	let showFilters = $state(true);
	let searchFocused = $state(false);

	const releaseVersion = $derived(health?.version ?? 'unknown');

	const typeFilters = [
		{ key: 'all', label: 'All' },
		{ key: 'scraper', label: 'Scraper' },
		{ key: 'organize', label: 'Organize' },
		{ key: 'system', label: 'System' }
	];

	const severityConfig: Record<string, { label: string; icon: typeof AlertTriangle; dotClass: string; badgeClass: string }> = {
		error: { label: 'Error', icon: CircleAlert, dotClass: 'bg-red-500', badgeClass: 'bg-red-100 text-red-700 dark:bg-red-500/20 dark:text-red-300' },
		warn: { label: 'Warn', icon: AlertTriangle, dotClass: 'bg-amber-500', badgeClass: 'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-300' },
		info: { label: 'Info', icon: Info, dotClass: 'bg-blue-500', badgeClass: 'bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-300' },
		debug: { label: 'Debug', icon: Bug, dotClass: 'bg-violet-500', badgeClass: 'bg-violet-100 text-violet-700 dark:bg-violet-500/20 dark:text-violet-300' }
	};

	function hasActiveFilters(): boolean {
		return activeTypeFilter !== 'all' || activeSeverityFilter !== 'all' || startDate !== '' || endDate !== '' || searchText !== '' || chipFilter !== null;
	}

	function clearAllFilters() {
		activeTypeFilter = 'all';
		activeSeverityFilter = 'all';
		startDate = '';
		endDate = '';
		searchText = '';
		chipFilter = null;
		offset = 0;
		loadEvents();
	}

	async function loadHealth() {
		try { health = await apiClient.health(); } catch { /* optional */ }
	}

	async function loadEvents(append = false) {
		if (append && loadingMore) return;
		if (!hasLoadedOnce && !append) {
			loading = true;
		} else if (!append) {
			isRefreshing = true;
		} else {
			loadingMore = true;
		}
		error = null;
		try {
			const params: Record<string, string | number> = { limit, offset: append ? offset : 0 };
			if (activeTypeFilter !== 'all') params.type = activeTypeFilter;
			if (activeSeverityFilter !== 'all') params.severity = activeSeverityFilter;
			if (startDate) params.start = toRFC3339(startDate);
			if (endDate) params.end = toRFC3339(endDate);
			const response = await apiClient.listEvents(params);
			if (append) {
				events = [...events, ...response.events];
			} else {
				events = response.events;
				offset = 0;
			}
			total = response.total;
			offset += response.events.length;
			hasMore = offset < total;
			listRenderVersion += 1;
			hasLoadedOnce = true;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load events';
			if (!hasLoadedOnce) { events = []; }
		} finally {
			loading = false;
			isRefreshing = false;
			loadingMore = false;
		}
	}

	async function loadStats() {
		try { stats = await apiClient.getEventStats(); } catch { /* supplementary */ }
	}

	async function refreshAll() {
		await Promise.all([loadEvents(), loadStats(), loadHealth()]);
	}

	function setTypeFilter(key: string) { activeTypeFilter = key; offset = 0; loadEvents(); }
	function setSeverityFilter(key: string) { activeSeverityFilter = key; offset = 0; loadEvents(); }
	function clearDates() { startDate = ''; endDate = ''; offset = 0; loadEvents(); }

	$effect(() => {
		const _start = startDate;
		const _end = endDate;
		if (hasLoadedOnce) { offset = 0; loadEvents(); }
	});

	function setChipFilter(field: string, value: string) { chipFilter = { field, value }; }

	function toggleEventExpanded(id: number) {
		const newSet = new Set(expandedEvents);
		if (newSet.has(id)) {
			newSet.delete(id);
		} else {
			newSet.add(id);
		}
		expandedEvents = newSet;
	}

	function parseContext(context: string): Record<string, unknown> {
		if (!context) return {};
		try { return JSON.parse(context) as Record<string, unknown>; }
		catch { return {}; }
	}

	function getContextField(context: string, field: string): string | undefined {
		const parsed = parseContext(context);
		const value = parsed[field];
		if (value === undefined || value === null) return undefined;
		return String(value);
	}

	function formatTimestamp(dateStr: string): string {
		const date = new Date(dateStr);
		return new Intl.DateTimeFormat('en-US', {
			month: 'short', day: 'numeric',
			hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false
		}).format(date);
	}

	function formatRelativeTime(dateStr: string): string {
		const date = new Date(dateStr);
		const now = new Date();
		const diffMs = now.getTime() - date.getTime();
		const diffMins = Math.floor(diffMs / 60000);
		const diffHours = Math.floor(diffMs / 3600000);
		const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
		const eventDay = new Date(date.getFullYear(), date.getMonth(), date.getDate());
		const calendarDays = Math.round((today.getTime() - eventDay.getTime()) / 86400000);
		if (diffMins < 1) return 'just now';
		if (diffMins < 60) return `${diffMins}m ago`;
		if (diffHours < 24) return `${diffHours}h ago`;
		if (calendarDays === 1) return 'yesterday';
		if (calendarDays >= 2 && calendarDays < 7) return `${calendarDays}d ago`;
		return date.toLocaleDateString();
	}

	function toRFC3339(datetimeLocal: string): string {
		return new Date(datetimeLocal).toISOString();
	}

	function hasClientSideFilter(): boolean {
		return searchText !== '' || chipFilter !== null;
	}

	function getDisplayEvents(): EventItem[] {
		let result = events;
		if (searchText) {
			const q = searchText.toLowerCase();
			result = result.filter((e) => e.message.toLowerCase().includes(q));
		}
		if (chipFilter) {
			result = result.filter((e) => {
				const val = getContextField(e.context, chipFilter!.field);
				return val === chipFilter!.value;
			});
		}
		return result;
	}

	function handleScroll() {
		if (!containerEl) return;
		const { scrollTop, scrollHeight, clientHeight } = containerEl;
		if (scrollHeight - scrollTop - clientHeight < 300 && hasMore && !loadingMore && !loading) {
			loadEvents(true);
		}
	}

	onMount(() => { refreshAll(); });
</script>

<svelte:window onscroll={handleScroll} />

<div class="min-h-screen bg-background" bind:this={containerEl}>
	<div class="container mx-auto px-4 py-8 max-w-7xl">
		<div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-6">
			<div>
				<h1 class="text-2xl font-bold tracking-tight">Logs</h1>
				<p class="text-muted-foreground text-sm mt-1">Structured event stream for debugging</p>
			</div>
			<div class="flex items-center gap-2">
				{#if hasActiveFilters()}
					<Button variant="outline" size="sm" onclick={clearAllFilters}>
						<X class="h-4 w-4 mr-1.5" />
						Clear
					</Button>
				{/if}
				<Button
					variant={isLiveMode ? 'default' : 'outline'}
					size="sm"
					onclick={() => isLiveMode = !isLiveMode}
				>
					{#if isLiveMode}
						<Zap class="h-4 w-4 mr-1.5" />
					{:else}
						<Pause class="h-4 w-4 mr-1.5" />
					{/if}
					{isLiveMode ? 'Live' : 'Paused'}
				</Button>
				<Button variant="outline" size="sm" onclick={refreshAll} disabled={isRefreshing}>
					<RefreshCw class="h-4 w-4 mr-1.5 {isRefreshing ? 'animate-spin' : ''}" />
					Refresh
				</Button>
			</div>
		</div>

		{#if error}
			<div in:fade={{ duration: 150 }} class="mb-4">
				<Card class="p-3 bg-destructive/5 border-destructive/20">
					<div class="flex items-center gap-2 text-destructive text-sm">
						<AlertTriangle class="h-4 w-4" />
						<span>{error}</span>
					</div>
				</Card>
			</div>
		{/if}

		{#if !loading || hasLoadedOnce}
			<div in:fade={{ duration: 150 }}>
				<Card class="mb-6 {hasActiveFilters() ? 'border-primary/30 bg-primary/[0.02]' : ''}">
					<div class="p-4 space-y-3">
						<div class="flex items-center gap-3 flex-wrap">
							<div class="relative flex-1 min-w-[200px] max-w-[320px]">
								<Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
								<input
									type="text"
									placeholder="Search messages..."
									bind:value={searchText}
									class="h-9 w-full pl-9 pr-3 rounded-md border border-input bg-background text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:border-transparent"
								/>
							</div>

							<div class="flex items-center gap-1.5">
								{#each ['all', 'error', 'warn', 'info', 'debug'] as sev}
									{@const config = sev === 'all' ? null : severityConfig[sev]}
									{@const isActive = activeSeverityFilter === sev}
									<Button
										variant={isActive ? 'default' : 'ghost'}
										size="sm"
										onclick={() => setSeverityFilter(sev)}
									>
										{sev === 'all' ? 'All' : sev.charAt(0).toUpperCase() + sev.slice(1)}
									</Button>
								{/each}
							</div>
						</div>

					<div class="border-t border-border/50"></div>

					<div class="flex items-center gap-3 flex-wrap">
						<span class="text-xs font-semibold uppercase tracking-wider text-muted-foreground/70 w-16 shrink-0">Type</span>
						<div class="flex flex-wrap gap-1.5">
							{#each typeFilters as filter}
								{@const count = filter.key === 'all' ? (stats?.total ?? 0) : (stats?.by_type[filter.key] ?? 0)}
								{@const isActive = activeTypeFilter === filter.key}
								<Button
									variant={isActive ? 'default' : 'ghost'}
									size="sm"
									onclick={() => setTypeFilter(filter.key)}
								>
									{filter.label}
									<span class="ml-1.5 text-xs opacity-70">({count})</span>
								</Button>
							{/each}
						</div>
					</div>

					<div class="border-t border-border/50"></div>

					<div class="flex items-center gap-3 flex-wrap">
						<span class="text-xs font-semibold uppercase tracking-wider text-muted-foreground/70 w-16 shrink-0">
							<Calendar class="h-3.5 w-3.5 inline -mt-0.5" />
							Date
						</span>
						<div class="flex items-center gap-2">
							<input
								type="datetime-local"
								bind:value={startDate}
								class="h-8 rounded-md border border-input bg-background px-2.5 text-xs font-mono ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
							/>
							<span class="text-xs text-muted-foreground">→</span>
							<input
								type="datetime-local"
								bind:value={endDate}
								class="h-8 rounded-md border border-input bg-background px-2.5 text-xs font-mono ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
							/>
							{#if startDate || endDate}
								<Button variant="ghost" size="sm" class="h-8 px-2" onclick={clearDates}>
									<X class="h-3 w-3" />
								</Button>
							{/if}
						</div>
					</div>
				</div>

				{#if hasActiveFilters()}
					<div class="px-4 py-2 border-t border-primary/10 bg-primary/[0.03] flex items-center gap-2 text-xs text-muted-foreground">
						<Filter class="h-3 w-3" />
						<span>Showing</span>
						{#if activeTypeFilter !== 'all'}
							<span class="font-medium text-foreground">{activeTypeFilter}</span>
						{/if}
						{#if activeSeverityFilter !== 'all'}
							<span class="font-medium text-foreground">{activeSeverityFilter}</span>
						{/if}
						{#if startDate || endDate}
							<span class="font-medium text-foreground">
								{startDate ? new Date(startDate).toLocaleDateString() : '…'} → {endDate ? new Date(endDate).toLocaleDateString() : '…'}
							</span>
						{/if}
						{#if searchText}
							<span class="font-medium text-foreground">"{searchText}"</span>
						{/if}
						{#if chipFilter}
							<span class="font-medium text-foreground font-mono">{chipFilter.field}={chipFilter.value}</span>
						{/if}
						<span>— {hasClientSideFilter() ? `${getDisplayEvents().length} of ${total} loaded` : `${total} result${total !== 1 ? 's' : ''}`}</span>
						{#if hasClientSideFilter()}
							<span class="text-muted-foreground/60 italic">(local filter)</span>
						{/if}
					</div>
				{/if}
			</Card>
			</div>
		{/if}

		{#if loading && !hasLoadedOnce}
			<div class="flex items-center justify-center py-20">
				<div class="text-center">
					<Clock class="h-8 w-8 animate-spin mx-auto mb-3 text-muted-foreground" />
					<p class="text-muted-foreground text-sm">Loading events...</p>
				</div>
			</div>
		{:else if getDisplayEvents().length === 0 && !loading}
			<Card class="p-12 text-center">
				<Activity class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
				<p class="text-muted-foreground mb-4">
					{hasActiveFilters() ? 'No events match your filters' : 'No events recorded yet'}
				</p>
				<p class="text-muted-foreground text-sm">
					{hasActiveFilters() ? 'Try adjusting your filters' : 'Events will appear here as the application runs.'}
				</p>
			</Card>
		{:else}
			{@const displayEvents = getDisplayEvents()}
			{#key listRenderVersion}
				<div class="space-y-2" in:fade={{ duration: 150 }}>
					{#each displayEvents as event, index (`${event.id}-${listRenderVersion}`)}
						{@const ctx = parseContext(event.context)}
						{@const sevConfig = severityConfig[event.severity.toLowerCase()]}
						{@const isExpanded = expandedEvents.has(event.id)}
						{@const isError = event.severity.toLowerCase() === 'error'}
						{@const isOrganize = event.event_type.toLowerCase() === 'organize'}
						{@const isRevert = event.source === 'revert'}

						<div in:fly={{ y: 8, duration: 180, delay: Math.min(index * 15, 80), easing: cubicOut }}>
							<Card
								class="overflow-hidden transition-colors shadow-none hover:bg-muted/30 cursor-pointer {isError ? 'border-l-2 border-l-red-500 bg-red-500/[0.03] dark:bg-red-500/[0.06]' : ''}"
							>
								<div
									class="flex items-start gap-3 px-4 py-3"
									onclick={() => toggleEventExpanded(event.id)}
									onkeydown={(e) => e.key === 'Enter' && toggleEventExpanded(event.id)}
									role="button"
									tabindex="0"
								>
								<div class="flex-shrink-0 pt-0.5">
									<svelte:component this={sevConfig.icon} class="h-4 w-4 {isError ? 'text-red-500' : event.severity.toLowerCase() === 'warn' ? 'text-amber-500' : event.severity.toLowerCase() === 'info' ? 'text-blue-500' : 'text-violet-500'}" />
								</div>

								<div class="flex-1 min-w-0">
									<div class="flex items-center gap-2 mb-1.5 flex-wrap">
										<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold {sevConfig?.badgeClass}">
											{event.severity}
										</span>
										<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-muted text-muted-foreground">
											{event.event_type}
										</span>
										<span class="text-xs text-muted-foreground ml-auto tabular-nums" title={formatTimestamp(event.created_at)}>
											{formatRelativeTime(event.created_at)}
										</span>
									</div>

									<p class="text-sm leading-snug text-foreground/90 {isError ? 'font-medium' : ''}">
										{event.message}
									</p>

									{#if isError && ctx.error}
										<p class="text-sm text-red-600 dark:text-red-400 mt-1 font-medium">
											{String(ctx.error)}
										</p>
									{/if}

									{#if (ctx.job_id || ctx.movie_id || isOrganize || isRevert) && !isExpanded}
										<div class="flex items-center gap-1.5 mt-1.5 flex-wrap">
											{#if isOrganize && ctx.mode}
												<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300">
													{String(ctx.mode)}
												</span>
											{/if}
											{#if ctx.job_id}
												<button
													type="button"
													onclick={(e) => { e.stopPropagation(); setChipFilter('job_id', String(ctx.job_id)); }}
													class="inline-flex items-center px-2 py-0.5 rounded text-xs font-mono bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-300 hover:bg-blue-200 dark:hover:bg-blue-500/30 transition-colors cursor-pointer"
												>
													job: {String(ctx.job_id).slice(0, 8)}
												</button>
											{/if}
											{#if ctx.movie_id}
												<button
													type="button"
													onclick={(e) => { e.stopPropagation(); setChipFilter('movie_id', String(ctx.movie_id)); }}
													class="inline-flex items-center px-2 py-0.5 rounded text-xs font-mono bg-violet-100 text-violet-700 dark:bg-violet-500/20 dark:text-violet-300 hover:bg-violet-200 dark:hover:bg-violet-500/30 transition-colors cursor-pointer"
												>
													movie: {String(ctx.movie_id)}
												</button>
											{/if}
										</div>
									{/if}
								</div>

								<button
									type="button"
									class="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
									onclick={(e) => { e.stopPropagation(); toggleEventExpanded(event.id); }}
								>
									{#if isExpanded}
										<ChevronDown class="h-4 w-4" />
									{:else}
										<ChevronRight class="h-4 w-4" />
									{/if}
								</button>
							</div>

							{#if isExpanded}
								<div in:slide={{ duration: 150 }} class="px-4 pb-3 ml-7">
									<div class="rounded-md border overflow-hidden bg-muted/30">
										<div class="px-3 py-2 border-b bg-muted/50 flex items-center justify-between">
											<span class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Context</span>
											<span class="text-xs text-muted-foreground tabular-nums">{formatTimestamp(event.created_at)}</span>
										</div>
										<div class="p-3 text-sm">
											<div class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
												{#each Object.entries(ctx) as [key, value]}
													<span class="text-muted-foreground font-medium">{key}</span>
													{#if key === 'job_id' || key === 'movie_id'}
														<button
															type="button"
															onclick={(e) => { e.stopPropagation(); setChipFilter(key, String(value)); }}
															class="text-primary hover:underline text-left"
														>
															{String(value)}
														</button>
													{:else if key === 'error'}
														<span class="text-red-600 dark:text-red-400 break-all">{String(value)}</span>
													{:else}
														<span class="text-foreground break-all">{String(value)}</span>
													{/if}
												{/each}
												{#if Object.keys(ctx).length === 0}
													<span class="text-muted-foreground/60 col-span-2 italic text-xs">No context data</span>
												{/if}
											</div>
										</div>
									</div>
								</div>
							{/if}
							</Card>
						</div>
					{/each}
				</div>
			{/key}

			{#if hasMore}
				<div class="flex items-center justify-center py-8">
					{#if loadingMore}
						<div class="flex items-center gap-2 text-muted-foreground">
							<Loader2 class="h-4 w-4 animate-spin" />
							<span class="text-sm">Loading more events...</span>
						</div>
					{:else}
						<div class="h-12"></div>
					{/if}
				</div>
			{/if}

			{#if !hasMore && displayEvents.length > 0}
				<div class="py-6 text-center">
					<div class="inline-flex items-center gap-2 text-xs text-muted-foreground/50">
						<Clock class="h-3.5 w-3.5" />
						<span>Showing all {total} event{total !== 1 ? 's' : ''}</span>
					</div>
				</div>
			{/if}
		{/if}

		<div class="flex items-center justify-between text-xs text-muted-foreground px-1 mt-6" in:fade={{ duration: 180 }}>
			<div class="flex items-center gap-2">
				<span>{total} total</span>
				<span>·</span>
				<span class="text-red-500">{stats?.by_severity?.error ?? 0} errors</span>
				<span>·</span>
				<span class="text-amber-500">{stats?.by_severity?.warn ?? 0} warnings</span>
			</div>
			<div>
				Version: {releaseVersion}
			</div>
		</div>
	</div>
</div>
