<script lang="ts">
	import { onMount } from 'svelte';
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fade, fly } from 'svelte/transition';
	import {
		Calendar,
		CircleCheckBig,
		CircleX,
		Clock,
		Trash2,
		ChevronLeft,
		ChevronRight,
		Filter,
		RefreshCw,
		AlertTriangle
	} from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import type { HistoryRecord, HistoryStats, HistoryListParams } from '$lib/api/types';

	// State
	let history = $state<HistoryRecord[]>([]);
	let stats = $state<HistoryStats | null>(null);
	let loading = $state(true);
	let hasLoadedOnce = $state(false);
	let isRefreshing = $state(false);
	let listRenderVersion = $state(0);
	let error = $state<string | null>(null);

	// Pagination
	let total = $state(0);
	let limit = $state(20);
	let offset = $state(0);

	// Filters
	let operationFilter = $state<string>('');
	let statusFilter = $state<string>('');
	let movieIdFilter = $state<string>('');

	// Delete confirmation
	let deleteConfirmId = $state<number | null>(null);
	let deleteLoading = $state(false);

	// Computed
	const currentPage = $derived(Math.floor(offset / limit) + 1);
	const totalPages = $derived(Math.ceil(total / limit));

	function itemDelay(index: number): number {
		return Math.min(index * 28, 220);
	}

	async function loadHistory() {
		if (!hasLoadedOnce) {
			loading = true;
		} else {
			isRefreshing = true;
		}
		error = null;

		try {
			const params: HistoryListParams = { limit, offset };
			if (operationFilter) params.operation = operationFilter;
			if (statusFilter) params.status = statusFilter;
			if (movieIdFilter) params.movie_id = movieIdFilter;

			const response = await apiClient.getHistory(params);
			history = response.records;
			total = response.total;
			listRenderVersion += 1;
			hasLoadedOnce = true;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load history';
			if (!hasLoadedOnce) {
				history = [];
			}
		} finally {
			loading = false;
			isRefreshing = false;
		}
	}

	async function loadStats() {
		try {
			stats = await apiClient.getHistoryStats();
		} catch (e) {
			console.error('Failed to load stats:', e);
		}
	}

	async function deleteRecord(id: number) {
		deleteLoading = true;
		try {
			await apiClient.deleteHistory(id);
			deleteConfirmId = null;
			// Reload data
			await Promise.all([loadHistory(), loadStats()]);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to delete record';
		} finally {
			deleteLoading = false;
		}
	}

	function goToPage(page: number) {
		offset = (page - 1) * limit;
		loadHistory();
	}

	function applyFilters() {
		offset = 0; // Reset to first page
		loadHistory();
	}

	function clearFilters() {
		operationFilter = '';
		statusFilter = '';
		movieIdFilter = '';
		offset = 0;
		loadHistory();
	}

	function formatDate(dateStr: string) {
		const date = new Date(dateStr);
		return new Intl.DateTimeFormat('en-US', {
			dateStyle: 'medium',
			timeStyle: 'short'
		}).format(date);
	}

	function getOperationLabel(operation: string): string {
		const labels: Record<string, string> = {
			scrape: 'Scrape',
			organize: 'Organize',
			download: 'Download',
			nfo: 'NFO Generation'
		};
		return labels[operation] || operation;
	}

	function getStatusColor(status: string): string {
		switch (status) {
			case 'success':
				return 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300';
			case 'failed':
				return 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300';
			case 'reverted':
				return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300';
			default:
				return 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300';
		}
	}

	function getFilename(path: string): string {
		if (!path) return '';
		const parts = path.split('/');
		return parts[parts.length - 1] || path;
	}

	function getParentDir(path: string): string {
		if (!path) return '';
		const lastSlash = path.lastIndexOf('/');
		if (lastSlash === -1) return '';
		return path.slice(0, lastSlash);
	}

	function truncateDir(dir: string, maxLen: number = 50): string {
		if (!dir || dir.length <= maxLen) return dir;
		// Show start...end
		const half = Math.floor((maxLen - 3) / 2);
		return dir.slice(0, half) + '...' + dir.slice(-half);
	}

	onMount(() => {
		Promise.all([loadHistory(), loadStats()]);
	});
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-6xl mx-auto space-y-6">
		<!-- Header -->
		<div class="flex items-center justify-between">
			<div>
				<h1 class="text-3xl font-bold">Operation History</h1>
				<p class="text-muted-foreground mt-1">View past scraping and organization operations</p>
			</div>
			<Button variant="outline" onclick={() => Promise.all([loadHistory(), loadStats()])}>
				<RefreshCw class="h-4 w-4 mr-2 {isRefreshing ? 'animate-spin' : ''}" />
				Refresh
			</Button>
		</div>

		<!-- Stats Cards -->
		{#if stats}
			<div class="grid grid-cols-2 md:grid-cols-4 gap-4">
				<div in:fly|local={{ y: 8, duration: 200, easing: quintOut }}>
					<Card class="p-4">
						<div class="text-2xl font-bold">{stats.total}</div>
						<div class="text-sm text-muted-foreground">Total Operations</div>
					</Card>
				</div>
				<div in:fly|local={{ y: 8, duration: 220, easing: quintOut }}>
					<Card class="p-4">
						<div class="text-2xl font-bold text-green-600">{stats.success}</div>
						<div class="text-sm text-muted-foreground">Successful</div>
					</Card>
				</div>
				<div in:fly|local={{ y: 8, duration: 240, easing: quintOut }}>
					<Card class="p-4">
						<div class="text-2xl font-bold text-red-600">{stats.failed}</div>
						<div class="text-sm text-muted-foreground">Failed</div>
					</Card>
				</div>
				<div in:fly|local={{ y: 8, duration: 260, easing: quintOut }}>
					<Card class="p-4">
						<div class="text-2xl font-bold text-yellow-600">{stats.reverted}</div>
						<div class="text-sm text-muted-foreground">Reverted</div>
					</Card>
				</div>
			</div>

			<!-- Operation breakdown -->
			<Card class="p-4">
				<h3 class="font-semibold mb-3">Operations by Type</h3>
				<div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
					<div class="flex justify-between">
						<span class="text-muted-foreground">Scrape</span>
						<span class="font-medium">{stats.by_operation.scrape || 0}</span>
					</div>
					<div class="flex justify-between">
						<span class="text-muted-foreground">Organize</span>
						<span class="font-medium">{stats.by_operation.organize || 0}</span>
					</div>
					<div class="flex justify-between">
						<span class="text-muted-foreground">Download</span>
						<span class="font-medium">{stats.by_operation.download || 0}</span>
					</div>
					<div class="flex justify-between">
						<span class="text-muted-foreground">NFO</span>
						<span class="font-medium">{stats.by_operation.nfo || 0}</span>
					</div>
				</div>
			</Card>
		{/if}

		<!-- Filters -->
		<Card class="p-4">
			<div class="flex items-center gap-2 mb-3">
				<Filter class="h-4 w-4" />
				<h3 class="font-semibold">Filters</h3>
			</div>
			<div class="grid grid-cols-1 md:grid-cols-4 gap-4">
				<div>
					<label class="block text-sm font-medium mb-1" for="operation-filter">Operation</label>
					<select
						id="operation-filter"
						bind:value={operationFilter}
						class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
					>
						<option value="">All Operations</option>
						<option value="scrape">Scrape</option>
						<option value="organize">Organize</option>
						<option value="download">Download</option>
						<option value="nfo">NFO Generation</option>
					</select>
				</div>
				<div>
					<label class="block text-sm font-medium mb-1" for="status-filter">Status</label>
					<select
						id="status-filter"
						bind:value={statusFilter}
						class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
					>
						<option value="">All Statuses</option>
						<option value="success">Success</option>
						<option value="failed">Failed</option>
						<option value="reverted">Reverted</option>
					</select>
				</div>
				<div>
					<label class="block text-sm font-medium mb-1" for="movie-filter">Movie ID</label>
					<input
						id="movie-filter"
						type="text"
						bind:value={movieIdFilter}
						placeholder="e.g., IPX-123"
						class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
					/>
				</div>
				<div class="flex items-end gap-2">
					<Button onclick={applyFilters} class="flex-1">Apply</Button>
					<Button variant="outline" onclick={clearFilters}>Clear</Button>
				</div>
			</div>
		</Card>

		<!-- Error display -->
		{#if error}
			<Card class="p-4 bg-destructive/10 border-destructive">
				<div class="flex items-center gap-2 text-destructive">
					<AlertTriangle class="h-5 w-5" />
					<span>{error}</span>
				</div>
			</Card>
		{/if}

		<!-- History list -->
		{#if loading && !hasLoadedOnce}
			<Card class="p-8 text-center">
				<Clock class="h-8 w-8 animate-spin mx-auto mb-2" />
				<p class="text-muted-foreground">Loading history...</p>
			</Card>
		{:else if history.length === 0}
			<Card class="p-8 text-center">
				<p class="text-muted-foreground">No operations in history</p>
				{#if operationFilter || statusFilter || movieIdFilter}
					<Button variant="link" onclick={clearFilters} class="mt-2">Clear filters</Button>
				{/if}
			</Card>
		{:else}
			{#key listRenderVersion}
				<div class="space-y-3" in:fade|local={{ duration: 170 }}>
					{#each history as item, index (`${item.id}-${listRenderVersion}`)}
						<div
							animate:flip={{ duration: 240, easing: quintOut }}
							in:fly|local={{ y: 8, duration: 210, delay: itemDelay(index), easing: quintOut }}
							out:fade|local={{ duration: 120 }}
						>
							<Card class="p-4 hover:shadow-md transition-shadow">
						<div class="flex items-start justify-between">
							<div class="flex-1">
								<div class="flex items-center gap-3 mb-2">
									{#if item.status === 'success'}
										<CircleCheckBig class="h-5 w-5 text-green-500" />
									{:else if item.status === 'failed'}
										<CircleX class="h-5 w-5 text-red-500" />
									{:else}
										<Clock class="h-5 w-5 text-yellow-500" />
									{/if}
									<h3 class="font-semibold">{getOperationLabel(item.operation)}</h3>
									<span class="px-2 py-0.5 text-xs rounded {getStatusColor(item.status)}">
										{item.status}
									</span>
									{#if item.dry_run}
										<span
											class="px-2 py-0.5 text-xs rounded bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300"
										>
											Dry Run
										</span>
									{/if}
								</div>

								<div class="grid grid-cols-1 md:grid-cols-2 gap-2 text-sm text-muted-foreground">
									<div class="flex items-center gap-1">
										<Calendar class="h-4 w-4" />
										{formatDate(item.created_at)}
									</div>
									{#if item.movie_id}
										<div>
											<span class="font-medium">Movie:</span>
											{item.movie_id}
										</div>
									{/if}
								</div>

								{#if item.original_path || item.new_path}
									<div class="mt-2 text-sm space-y-1">
										{#if item.original_path}
											<div class="flex items-baseline gap-1" title={item.original_path}>
												<span class="text-muted-foreground shrink-0">From:</span>
												<span class="font-medium text-foreground">{getFilename(item.original_path)}</span>
												{#if getParentDir(item.original_path)}
													<span class="text-muted-foreground text-xs truncate max-w-xs">
														in {truncateDir(getParentDir(item.original_path))}
													</span>
												{/if}
											</div>
										{/if}
										{#if item.new_path}
											<div class="flex items-baseline gap-1" title={item.new_path}>
												<span class="text-muted-foreground shrink-0">To:</span>
												<span class="font-medium text-foreground">{getFilename(item.new_path)}</span>
												{#if getParentDir(item.new_path)}
													<span class="text-muted-foreground text-xs truncate max-w-xs">
														in {truncateDir(getParentDir(item.new_path))}
													</span>
												{/if}
											</div>
										{/if}
									</div>
								{/if}

								{#if item.error_message}
									<div class="mt-2 text-sm text-red-600 dark:text-red-400">
										<span class="font-medium">Error:</span>
										{item.error_message}
									</div>
								{/if}
							</div>

							<!-- Delete button -->
							<div class="ml-4">
								{#if deleteConfirmId === item.id}
									<div class="flex items-center gap-2">
										<Button
											variant="destructive"
											size="sm"
											onclick={() => deleteRecord(item.id)}
											disabled={deleteLoading}
										>
											{#if deleteLoading}
												<Clock class="h-4 w-4 animate-spin" />
											{:else}
												Confirm
											{/if}
										</Button>
										<Button variant="outline" size="sm" onclick={() => (deleteConfirmId = null)}>
											Cancel
										</Button>
									</div>
								{:else}
									<Button
										variant="ghost"
										size="icon"
										onclick={() => (deleteConfirmId = item.id)}
										class="text-muted-foreground hover:text-destructive"
									>
										<Trash2 class="h-4 w-4" />
									</Button>
								{/if}
							</div>
						</div>
							</Card>
						</div>
					{/each}
				</div>
			{/key}

			<!-- Pagination -->
			{#if totalPages > 1}
				<div class="flex items-center justify-between">
					<div class="text-sm text-muted-foreground">
						Showing {offset + 1} - {Math.min(offset + limit, total)} of {total} records
					</div>
					<div class="flex items-center gap-2">
						<Button
							variant="outline"
							size="sm"
							onclick={() => goToPage(currentPage - 1)}
							disabled={currentPage <= 1}
						>
							<ChevronLeft class="h-4 w-4" />
							Previous
						</Button>
						<span class="text-sm">Page {currentPage} of {totalPages}</span>
						<Button
							variant="outline"
							size="sm"
							onclick={() => goToPage(currentPage + 1)}
							disabled={currentPage >= totalPages}
						>
							Next
							<ChevronRight class="h-4 w-4" />
						</Button>
					</div>
				</div>
			{/if}
		{/if}
	</div>
</div>
