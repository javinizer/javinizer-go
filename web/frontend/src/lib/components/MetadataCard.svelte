<script lang="ts">
	import type { Movie } from '$lib/api/types';
	import { formatDuration } from '$lib/utils';
	import { Calendar, Clock, Star, Film } from 'lucide-svelte';
	import Card from './ui/Card.svelte';
	import Button from './ui/Button.svelte';

	interface Props {
		movie: Movie;
		onEdit?: () => void;
		selected?: boolean;
	}

	let { movie, onEdit, selected = false }: Props = $props();
</script>

<Card class="overflow-hidden {selected ? 'ring-2 ring-primary' : ''}">
	<div class="flex gap-4 p-4">
		<!-- Cover Image -->
		<div class="shrink-0">
			{#if movie.cover_url}
				<img
					src={movie.cover_url}
					alt={movie.title}
					class="w-32 h-48 object-cover rounded-md"
					loading="lazy"
				/>
			{:else}
				<div class="w-32 h-48 bg-muted rounded-md flex items-center justify-center">
					<Film class="h-12 w-12 text-muted-foreground" />
				</div>
			{/if}
		</div>

		<!-- Metadata -->
		<div class="flex-1 min-w-0 space-y-3">
			<!-- Title & ID -->
			<div>
				<div class="text-xs text-muted-foreground mb-1">{movie.id}</div>
				<h3 class="font-semibold text-lg truncate">{movie.display_name || movie.title}</h3>
				{#if movie.original_title && movie.original_title !== movie.title}
					<p class="text-sm text-muted-foreground truncate">{movie.original_title}</p>
				{/if}
			</div>

			<!-- Info Row -->
			<div class="flex flex-wrap gap-4 text-sm text-muted-foreground">
				{#if movie.release_date}
					<div class="flex items-center gap-1">
						<Calendar class="h-4 w-4" />
						{new Date(movie.release_date).toLocaleDateString()}
					</div>
				{/if}
				{#if movie.runtime}
					<div class="flex items-center gap-1">
						<Clock class="h-4 w-4" />
						{formatDuration(movie.runtime)}
					</div>
				{/if}
				{#if movie.rating_score}
					<div class="flex items-center gap-1">
						<Star class="h-4 w-4 fill-yellow-400 text-yellow-400" />
						{movie.rating_score.toFixed(1)}
						{#if movie.rating_votes}
							<span class="text-xs">({movie.rating_votes})</span>
						{/if}
					</div>
				{/if}
			</div>

			<!-- Maker & Label -->
			{#if movie.maker || movie.label}
				<div class="text-sm">
					{#if movie.maker}
						<span class="font-medium">{movie.maker}</span>
					{/if}
					{#if movie.label}
						<span class="text-muted-foreground"> • {movie.label}</span>
					{/if}
				</div>
			{/if}

			<!-- Actresses -->
			{#if movie.actresses && movie.actresses.length > 0}
				<div class="flex flex-wrap gap-1">
					{#each movie.actresses.slice(0, 3) as actress}
						<span class="px-2 py-1 bg-primary/10 text-primary rounded-md text-xs">
							{actress.japanese_name || `${actress.first_name || ''} ${actress.last_name || ''}`.trim() || 'Unknown'}
						</span>
					{/each}
					{#if movie.actresses.length > 3}
						<span class="px-2 py-1 bg-muted text-muted-foreground rounded-md text-xs">
							+{movie.actresses.length - 3} more
						</span>
					{/if}
				</div>
			{/if}

			<!-- Genres -->
			{#if movie.genres && movie.genres.length > 0}
				<div class="flex flex-wrap gap-1">
					{#each movie.genres.slice(0, 5) as genre}
						<span class="px-2 py-0.5 bg-secondary text-secondary-foreground rounded text-xs">
							{genre}
						</span>
					{/each}
					{#if movie.genres.length > 5}
						<span class="px-2 py-0.5 bg-muted text-muted-foreground rounded text-xs">
							+{movie.genres.length - 5}
						</span>
					{/if}
				</div>
			{/if}

			<!-- Description Preview -->
			{#if movie.description}
				<p class="text-sm text-muted-foreground line-clamp-2">
					{movie.description}
				</p>
			{/if}

			<!-- Actions -->
			{#if onEdit}
				<div class="pt-2">
					<Button size="sm" onclick={onEdit}>Edit Metadata</Button>
				</div>
			{/if}
		</div>
	</div>
</Card>
