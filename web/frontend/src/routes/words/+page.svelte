<script lang="ts">
	import { cubicOut } from "svelte/easing";
	import { fade, fly } from "svelte/transition";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { apiClient } from "$lib/api/client";
	import type {
		WordReplacement,
		WordReplacementUpdateRequest,
	} from "$lib/api/types";
	import { toastStore } from "$lib/stores/toast";
	import {
		Trash2,
		Plus,
		Loader2,
		Search,
		X,
		Check,
		Pencil,
		ArrowDownUp,
		ChevronsDownUp,
		ArrowLeft,
		Type,
	} from "lucide-svelte";
	import Button from "$lib/components/ui/Button.svelte";
	import Card from "$lib/components/ui/Card.svelte";
	import { createWordReplacementsQuery } from "$lib/query/queries";

	const queryClient = useQueryClient();

	const replacementsQuery = createWordReplacementsQuery();
	let replacements = $derived<WordReplacement[]>(
		replacementsQuery.data?.replacements ?? [],
	);
	let loading = $derived(replacementsQuery.isPending);
	let error = $derived<string | null>(
		replacementsQuery.error?.message ?? null,
	);

	let newOriginal = $state("");
	let newReplacement = $state("");
	let searchQuery = $state("");
	let sortDirection = $state<"asc" | "desc">("asc");

	let filteredAndSorted = $derived.by(() => {
		let result = replacements;
		if (searchQuery.trim()) {
			const q = searchQuery.trim().toLowerCase();
			result = result.filter(
				(r) =>
					r.original.toLowerCase().includes(q) ||
					r.replacement.toLowerCase().includes(q),
			);
		}
		result = [...result].sort((a, b) => {
			return sortDirection === "asc"
				? a.original.localeCompare(b.original)
				: b.original.localeCompare(a.original);
		});
		return result;
	});

	let editingId = $state<number | null>(null);
	let editOriginal = $state("");
	let editReplacement = $state("");

	const addMutation = createMutation(() => ({
		mutationFn: ({
			original,
			replacement,
		}: {
			original: string;
			replacement: string;
		}) => apiClient.createWordReplacement({ original, replacement }),
		onSuccess: (_data, { original, replacement }) => {
			newOriginal = "";
			newReplacement = "";
			toastStore.success(
				`Word replacement "${original}" → "${replacement}" added`,
				3000,
			);
			void queryClient.invalidateQueries({
				queryKey: ["word-replacements"],
			});
		},
		onError: (err: Error) => {
			toastStore.error(
				err.message || "Failed to add word replacement",
				4000,
			);
		},
	}));

	const updateMutation = createMutation(() => ({
		mutationFn: (req: WordReplacementUpdateRequest) =>
			apiClient.updateWordReplacement(req),
		onSuccess: (_data, { original, replacement }) => {
			editingId = null;
			toastStore.success(
				`Word replacement updated: "${original}" → "${replacement}"`,
				3000,
			);
			void queryClient.invalidateQueries({
				queryKey: ["word-replacements"],
			});
		},
		onError: (err: Error) => {
			toastStore.error(
				err.message || "Failed to update word replacement",
				4000,
			);
		},
	}));

	const deleteMutation = createMutation(() => ({
		mutationFn: (original: string) =>
			apiClient.deleteWordReplacement(original),
		onSuccess: (_, original) => {
			toastStore.success(`Word replacement "${original}" removed`, 3000);
			void queryClient.invalidateQueries({
				queryKey: ["word-replacements"],
			});
		},
		onError: (err: Error) => {
			toastStore.error(
				err.message || "Failed to delete word replacement",
				4000,
			);
		},
	}));

	function handleAdd() {
		const original = newOriginal.trim();
		const replacement = newReplacement.trim();
		if (!original) {
			toastStore.error("Original field is required", 4000);
			return;
		}
		addMutation.mutate({ original, replacement });
	}

	function handleDelete(original: string) {
		deleteMutation.mutate(original);
	}

	function startEdit(rep: WordReplacement) {
		editingId = rep.id;
		editOriginal = rep.original;
		editReplacement = rep.replacement;
	}

	function cancelEdit() {
		editingId = null;
		editOriginal = "";
		editReplacement = "";
	}

	function saveEdit(rep: WordReplacement) {
		const o = editOriginal.trim();
		const r = editReplacement.trim();
		if (!o) {
			toastStore.error("Original field is required", 4000);
			return;
		}
		updateMutation.mutate({ original: rep.original, replacement: r });
	}

	function toggleSort() {
		sortDirection = sortDirection === "asc" ? "desc" : "asc";
	}

	function clearSearch() {
		searchQuery = "";
	}

	function handleAddKeydown(e: KeyboardEvent) {
		if (e.key === "Enter") {
			e.preventDefault();
			handleAdd();
		}
	}

	function handleEditKeydown(e: KeyboardEvent) {
		if (e.key === "Enter") {
			e.preventDefault();
			const rep = replacements.find((r) => r.id === editingId);
			if (rep) saveEdit(rep);
		} else if (e.key === "Escape") {
			cancelEdit();
		}
	}
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-5xl mx-auto space-y-6">
		<div
			class="flex flex-wrap items-center justify-between gap-3"
			in:fly|local={{ y: -10, duration: 240, easing: cubicOut }}
		>
			<div class="flex items-center gap-3">
				<a href="/settings">
					<Button variant="ghost" size="icon">
						{#snippet children()}
							<ArrowLeft class="h-5 w-5" />
						{/snippet}
					</Button>
				</a>
				<div>
					<div class="flex items-center gap-2">
						<Type class="h-6 w-6 text-primary" />
						<h1 class="text-3xl font-bold">Word Replacements</h1>
					</div>
					<p class="text-muted-foreground mt-1">
						Uncensor and normalize censored text in scraped metadata
						(e.g., asterisks → full words)
					</p>
				</div>
			</div>
		</div>

		{#if error}
			<div in:fly|local={{ y: 8, duration: 180 }}>
				<Card
					class="p-4 border-destructive bg-destructive/10 text-destructive"
				>
					Failed to load word replacements: {error}
				</Card>
			</div>
		{:else}
			<!-- Add new replacement form -->
			<div in:fly|local={{ y: 8, duration: 180, delay: 60 }}>
				<Card class="p-5">
					<p class="text-sm font-medium mb-3">
						Add a new word replacement rule
					</p>
					<div class="flex flex-col sm:flex-row items-start gap-3">
						<div class="flex-1 w-full sm:w-auto">
							<label
								for="word-original"
								class="block text-xs font-medium text-muted-foreground mb-1"
								>Original (Censored)</label
							>
							<input
								id="word-original"
								type="text"
								bind:value={newOriginal}
								placeholder="e.g., F***"
								onkeydown={handleAddKeydown}
								class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
							/>
						</div>
						<div class="flex-1 w-full sm:w-auto">
							<label
								for="word-replacement"
								class="block text-xs font-medium text-muted-foreground mb-1"
								>Replacement</label
							>
							<input
								id="word-replacement"
								type="text"
								bind:value={newReplacement}
								placeholder="e.g., Fuck (leave empty to remove)"
								onkeydown={handleAddKeydown}
								class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
							/>
						</div>
						<div class="flex items-end">
							<Button
								type="button"
								size="sm"
								onclick={handleAdd}
								disabled={addMutation.isPending ||
									!newOriginal.trim()}
							>
								{#if addMutation.isPending}
									<Loader2
										class="h-4 w-4 animate-spin mr-1"
									/>
								{:else}
									<Plus class="h-4 w-4 mr-1" />
								{/if}
								Add
							</Button>
						</div>
					</div>
				</Card>
			</div>

			<!-- Table -->
			<div in:fade|local={{ duration: 240, delay: 120 }}>
				{#if loading}
					<Card class="p-8 text-center text-muted-foreground">
						<Loader2 class="h-5 w-5 animate-spin mx-auto mb-2" />
						Loading word replacements...
					</Card>
				{:else if replacements.length === 0}
					<Card class="p-8 text-center">
						<p class="text-muted-foreground">
							No word replacements configured yet. Add one above.
						</p>
					</Card>
				{:else}
					<!-- Toolbar -->
					<div
						class="flex flex-col sm:flex-row items-start sm:items-center gap-3 mb-3"
					>
						<div class="relative flex-1">
							<Search
								class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground"
							/>
							<input
								type="text"
								bind:value={searchQuery}
								placeholder="Search by original or replacement..."
								class="w-full pl-9 pr-8 rounded-md border border-input bg-background py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
							/>
							{#if searchQuery}
								<button
									type="button"
									class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-0.5"
									onclick={clearSearch}
									title="Clear search"
								>
									<X class="h-3.5 w-3.5" />
								</button>
							{/if}
						</div>
						<button
							type="button"
							class="inline-flex items-center gap-1 px-2.5 py-2 text-sm border border-input rounded-md bg-background hover:bg-accent transition-colors text-muted-foreground hover:text-foreground"
							onclick={toggleSort}
							title="Toggle sort order"
						>
							{#if sortDirection === "asc"}
								<ArrowDownUp class="h-4 w-4" />
							{:else}
								<ChevronsDownUp class="h-4 w-4" />
							{/if}
							<span class="text-xs"
								>{sortDirection === "asc" ? "A-Z" : "Z-A"}</span
							>
						</button>
					</div>

					<!-- Scrollable table -->
					<div class="rounded-lg border bg-card overflow-hidden">
						<div
							class="relative"
							style="max-height: 560px; overflow-y: auto;"
						>
							<div class="sticky top-0 z-10">
								<div
									class="grid grid-cols-[1fr_1fr_auto] gap-0 text-sm py-3 px-4 font-medium text-muted-foreground border-b border-border bg-card/95 backdrop-blur"
								>
									<div>Original (Censored)</div>
									<div>Replacement</div>
									<div class="w-20 text-center">Actions</div>
								</div>
							</div>
							<div class="min-h-0">
								{#if filteredAndSorted.length === 0 && searchQuery.trim()}
									<div
										class="py-12 text-center text-muted-foreground text-sm"
									>
										No replacements match "{searchQuery}"
									</div>
								{:else}
									{#each filteredAndSorted as rep (rep.id)}
										<div
											class="grid grid-cols-[1fr_1fr_auto] gap-0 text-sm border-b border-border/50 last:border-b-0 hover:bg-accent/30 transition-colors"
										>
											{#if editingId === rep.id}
												<div class="py-2 px-4">
													<input
														type="text"
														value={rep.original}
														disabled
														class="w-full rounded border border-input bg-muted/50 px-2 py-1 text-sm font-mono text-muted-foreground cursor-not-allowed"
													/>
												</div>
												<div
													class="py-2 px-4 space-y-1"
												>
													<input
														type="text"
														bind:value={
															editReplacement
														}
														onkeydown={handleEditKeydown}
														class="w-full rounded border border-input bg-background px-2 py-1 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
													/>
													<div class="flex gap-1">
														<button
															type="button"
															class="inline-flex items-center gap-0.5 px-2.5 py-1 text-xs bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
															onclick={() =>
																saveEdit(rep)}
															disabled={updateMutation.isPending}
														>
															{#if updateMutation.isPending}
																<Loader2
																	class="h-3 w-3 animate-spin"
																/>
															{:else}
																<Check
																	class="h-3 w-3"
																/>
															{/if}
															Save
														</button>
														<button
															type="button"
															class="inline-flex items-center gap-0.5 px-2.5 py-1 text-xs border border-input rounded-md hover:bg-accent transition-colors"
															onclick={cancelEdit}
														>
															<X
																class="h-3 w-3"
															/>
															Cancel
														</button>
													</div>
												</div>
												<div class="py-2 px-4"></div>
											{:else}
												<div
													class="py-2.5 px-4 font-mono text-sm whitespace-nowrap overflow-hidden text-ellipsis max-w-[200px]"
													title={rep.original}
												>
													{rep.original}
												</div>
												<div
													class="py-2.5 px-4 font-mono text-sm whitespace-nowrap overflow-hidden text-ellipsis max-w-[200px]"
													title={rep.replacement ||
														"(removes text)"}
												>
													{#if rep.replacement}
														{rep.replacement}
													{:else}
														<span
															class="text-muted-foreground italic"
															>(removes text)</span
														>
													{/if}
												</div>
												<div
													class="py-2.5 px-4 flex items-center justify-center gap-0.5"
												>
													<button
														type="button"
														class="text-muted-foreground hover:text-foreground transition-colors p-1 rounded"
														title="Edit"
														onclick={() =>
															startEdit(rep)}
													>
														<Pencil
															class="h-4 w-4"
														/>
													</button>
													<button
														type="button"
														class="text-muted-foreground hover:text-destructive transition-colors p-1 rounded"
														title="Delete"
														onclick={() =>
															handleDelete(
																rep.original,
															)}
													>
														<Trash2
															class="h-4 w-4"
														/>
													</button>
												</div>
											{/if}
										</div>
									{/each}
								{/if}
							</div>
						</div>
					</div>

					<!-- Footer count -->
					{#if searchQuery.trim()}
						<p class="text-xs text-muted-foreground pt-1">
							Showing {filteredAndSorted.length} of {replacements.length}
							replacements
						</p>
					{:else}
						<p class="text-xs text-muted-foreground pt-1">
							{replacements.length} replacement{replacements.length !==
							1
								? "s"
								: ""} configured
						</p>
					{/if}
				{/if}
			</div>

			<!-- Note -->
			<div
				class="rounded-lg border border-border/60 bg-muted/20 px-4 py-3"
			>
				<p class="text-xs text-muted-foreground">
					Replacements are applied after scraping (post-translation).
					They perform literal string matching on all text metadata
					fields. Empty replacement removes the original text.
				</p>
			</div>
		{/if}
	</div>
</div>
