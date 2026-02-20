<script lang="ts">
	import type { Movie, Genre } from '$lib/api/types';
	import { CircleAlert, X, Plus } from 'lucide-svelte';

	interface Props {
		movie: Movie;
		originalMovie: Movie;
		onUpdate: (movie: Movie) => void;
		fieldSources?: Record<string, string>;
		showFieldSources?: boolean;
	}

	let { movie, originalMovie, onUpdate, fieldSources, showFieldSources = false }: Props = $props();

	// Create a local editable copy - initialized by effect
	let editedMovie = $state<Movie>({} as Movie);

	// Genre input state
	let newGenreInput = $state('');

	// Sync editedMovie when movie prop changes (including initial mount)
	$effect(() => {
		editedMovie = { ...movie };
	});

	// Track which fields have been modified
	function isModified(field: keyof Movie): boolean {
		return editedMovie[field] !== originalMovie[field];
	}

	function sourceText(...fieldKeys: string[]): string | null {
		if (!showFieldSources || !fieldSources) return null;
		for (const fieldKey of fieldKeys) {
			const rawSource = fieldSources[fieldKey];
			if (!rawSource) continue;

			const source = rawSource.trim();
			if (!source) continue;

			const normalized = source.toLowerCase();
			if (normalized === 'nfo') return 'via NFO';
			if (normalized === 'merged') return 'via merged data';
			if (normalized === 'empty') return 'empty';
			return `via ${source}`;
		}
		return null;
	}

	function handleDateChange(e: Event) {
		const target = e.target as HTMLInputElement;
		if (target.value) {
			editedMovie.release_date = target.value;
			onUpdate(editedMovie);
		}
	}

	// Format date for input field
	const formattedDate = $derived(
		editedMovie.release_date
			? new Date(editedMovie.release_date).toISOString().split('T')[0]
			: ''
	);

	// Genre management functions
	function addGenre() {
		const trimmedInput = newGenreInput.trim();
		if (!trimmedInput) return;

		// Check if genre already exists
		const exists = editedMovie.genres?.some(g => g.name.toLowerCase() === trimmedInput.toLowerCase());
		if (exists) {
			newGenreInput = '';
			return;
		}

		// Add new genre
		if (!editedMovie.genres) {
			editedMovie.genres = [];
		}
		editedMovie.genres = [...editedMovie.genres, { name: trimmedInput }];
		newGenreInput = '';
		onUpdate(editedMovie);
	}

	function removeGenre(genreName: string) {
		if (!editedMovie.genres) return;
		editedMovie.genres = editedMovie.genres.filter(g => g.name !== genreName);
		onUpdate(editedMovie);
	}

	function handleGenreKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			addGenre();
		}
	}
</script>

<div class="space-y-4">
	<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
		<!-- ID -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Movie ID
				{#if sourceText('id')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('id')}</span>
				{/if}
				{#if isModified('id')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="text"
				bind:value={editedMovie.id}
				onchange={() => onUpdate(editedMovie)}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Content ID -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Content ID
				{#if sourceText('content_id')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('content_id')}</span>
				{/if}
				{#if isModified('content_id')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="text"
				bind:value={editedMovie.content_id}
				onchange={() => onUpdate(editedMovie)}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Title -->
		<div class="md:col-span-2">
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Title
				{#if sourceText('title', 'display_name')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('title', 'display_name')}</span>
				{/if}
				{#if isModified('title')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="text"
				value={editedMovie.display_name || editedMovie.title}
				onchange={(e) => {
					editedMovie.title = e.currentTarget.value;
					onUpdate(editedMovie);
				}}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Original Title -->
		<div class="md:col-span-2">
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Original Title (Japanese)
				{#if sourceText('original_title')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('original_title')}</span>
				{/if}
				{#if isModified('original_title')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="text"
				bind:value={editedMovie.original_title}
				onchange={() => onUpdate(editedMovie)}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Description -->
		<div class="md:col-span-2">
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Description
				{#if sourceText('description')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('description')}</span>
				{/if}
				{#if isModified('description')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<textarea
				bind:value={editedMovie.description}
				onchange={() => onUpdate(editedMovie)}
				rows="4"
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			></textarea>
		</div>

		<!-- Release Date -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Release Date
				{#if sourceText('release_date')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('release_date')}</span>
				{/if}
				{#if isModified('release_date')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="date"
				value={formattedDate}
				onchange={handleDateChange}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Runtime -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Runtime (minutes)
				{#if sourceText('runtime')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('runtime')}</span>
				{/if}
				{#if isModified('runtime')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="number"
				bind:value={editedMovie.runtime}
				onchange={() => onUpdate(editedMovie)}
				min="0"
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Director -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Director
				{#if sourceText('director')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('director')}</span>
				{/if}
				{#if isModified('director')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="text"
				bind:value={editedMovie.director}
				onchange={() => onUpdate(editedMovie)}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Maker -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Studio / Maker
				{#if sourceText('maker')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('maker')}</span>
				{/if}
				{#if isModified('maker')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="text"
				bind:value={editedMovie.maker}
				onchange={() => onUpdate(editedMovie)}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Label -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Label
				{#if sourceText('label')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('label')}</span>
				{/if}
				{#if isModified('label')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="text"
				bind:value={editedMovie.label}
				onchange={() => onUpdate(editedMovie)}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Series -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Series
				{#if sourceText('series')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('series')}</span>
				{/if}
				{#if isModified('series')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="text"
				bind:value={editedMovie.series}
				onchange={() => onUpdate(editedMovie)}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Rating Score -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Rating Score (0-10)
				{#if sourceText('rating_score')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('rating_score')}</span>
				{/if}
				{#if isModified('rating_score')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="number"
				bind:value={editedMovie.rating_score}
				onchange={() => onUpdate(editedMovie)}
				min="0"
				max="10"
				step="0.1"
				placeholder="0.0"
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
			{#if editedMovie.rating_score !== undefined && editedMovie.rating_score !== null}
				<p class="text-xs text-muted-foreground mt-1">
					{editedMovie.rating_score.toFixed(1)}/10
					{#if editedMovie.rating_votes}
						({editedMovie.rating_votes} vote{editedMovie.rating_votes !== 1 ? 's' : ''})
					{/if}
				</p>
			{/if}
		</div>

		<!-- Rating Votes -->
		<div>
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Rating Votes
				{#if sourceText('rating_votes')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('rating_votes')}</span>
				{/if}
				{#if isModified('rating_votes')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>
			<input
				type="number"
				bind:value={editedMovie.rating_votes}
				onchange={() => onUpdate(editedMovie)}
				min="0"
				step="1"
				placeholder="0"
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all"
			/>
		</div>

		<!-- Genres -->
		<div class="md:col-span-2">
			<label class="flex items-center gap-2 text-sm font-medium mb-1">
				Genres
				{#if sourceText('genres')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('genres')}</span>
				{/if}
				{#if isModified('genres')}
					<CircleAlert class="h-3 w-3 text-orange-600" />
				{/if}
			</label>

			<!-- Cloud tags display -->
			<div class="w-full px-3 py-2.5 border rounded-lg min-h-11.5 flex flex-wrap gap-2 items-center bg-white focus-within:ring-2 focus-within:ring-primary/20 focus-within:border-primary transition-all">
				{#if editedMovie.genres && editedMovie.genres.length > 0}
					{#each editedMovie.genres as genre}
						<span class="inline-flex items-center gap-1.5 px-3 py-1.5 bg-linear-to-br from-primary/10 to-primary/5 text-primary rounded-full text-sm font-medium hover:from-primary/15 hover:to-primary/10 transition-all shadow-sm border border-primary/10">
							<span class="leading-none">{genre.name}</span>
							<button
								type="button"
								onclick={() => removeGenre(genre.name)}
								class="ml-0.5 -mr-1 p-0.5 rounded-full hover:bg-primary/20 transition-all opacity-70 hover:opacity-100"
								title="Remove {genre.name}"
							>
								<X class="h-3.5 w-3.5" />
							</button>
						</span>
					{/each}
				{/if}

				<!-- Input for adding new genre -->
				<div class="flex-1 min-w-35 inline-flex items-center gap-2">
					<input
						type="text"
						bind:value={newGenreInput}
						onkeydown={handleGenreKeydown}
						placeholder={editedMovie.genres && editedMovie.genres.length > 0 ? "Add another..." : "Type a genre and press Enter"}
						class="flex-1 outline-none bg-transparent text-sm min-w-0 placeholder:text-muted-foreground/60"
					/>
					{#if newGenreInput.trim()}
						<button
							type="button"
							onclick={addGenre}
							class="p-1.5 rounded-full bg-primary/10 hover:bg-primary/20 text-primary transition-all hover:scale-110"
							title="Add genre"
						>
							<Plus class="h-3.5 w-3.5" />
						</button>
					{/if}
				</div>
			</div>

			<p class="text-xs text-muted-foreground mt-1.5 flex items-center gap-1">
				<kbd class="px-1.5 py-0.5 bg-accent rounded text-xs font-mono border">Enter</kbd>
				to add
				{#if editedMovie.genres && editedMovie.genres.length > 0}
					• Click <X class="h-3 w-3 inline" /> to remove
				{/if}
			</p>
		</div>
	</div>
</div>
