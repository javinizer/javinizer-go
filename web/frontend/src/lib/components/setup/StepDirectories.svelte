<script lang="ts">
	import { FolderCheck } from 'lucide-svelte';
	import AllowedDirectoriesEditor from '$lib/components/AllowedDirectoriesEditor.svelte';

	let {
		dirs = $bindable(),
		error = null as string | null,
		submitting = false,
	}: {
		dirs: string[];
		error?: string | null;
		submitting?: boolean;
	} = $props();
</script>

<div class="step-head">
	<div class="step-badge"><FolderCheck class="h-5 w-5" /></div>
	<h1 class="step-title">Point Javinizer at your library</h1>
	<p class="step-sub">
		Add the folders that hold your video files. Javinizer can only read and write inside these
		locations — add as many as you need.
	</p>
</div>

{#if error}
	<div class="alert" role="alert">{error}</div>
{/if}

<div class="editor-wrap" class:disabled={submitting}>
	<AllowedDirectoriesEditor
		bind:directories={dirs}
		whitelistPaths={dirs}
		emptyHint="No directories added yet. Add one below to enable scanning, or skip and add later."
	/>
</div>

<style>
	.step-head {
		margin-bottom: 1.5rem;
	}

	.step-badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 44px;
		height: 44px;
		border-radius: 12px;
		background: hsl(var(--primary) / 0.12);
		color: hsl(var(--primary));
		margin-bottom: 0.85rem;
	}

	.step-title {
		font-size: 1.6rem;
		font-weight: 700;
		letter-spacing: -0.02em;
		line-height: 1.15;
	}

	.step-sub {
		margin-top: 0.4rem;
		color: hsl(var(--muted-foreground));
		font-size: 0.92rem;
		line-height: 1.5;
	}

	.alert {
		border: 1px solid hsl(var(--destructive) / 0.4);
		background: hsl(var(--destructive) / 0.1);
		color: hsl(var(--destructive));
		padding: 0.55rem 0.75rem;
		border-radius: 0.5rem;
		font-size: 0.85rem;
		margin-bottom: 1rem;
	}

	.editor-wrap.disabled {
		opacity: 0.6;
		pointer-events: none;
	}
</style>
