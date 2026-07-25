<script lang="ts">
	import type { OrganizePreviewResponse } from '$lib/api/types';
	import { pathSeparator } from '$lib/utils/path';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		preview: OrganizePreviewResponse | null;
		destinationPath: string;
		showAllScreenshots?: boolean;
	}

	let {
		preview,
		destinationPath,
		showAllScreenshots = $bindable(false)
	}: Props = $props();

	let sep = $derived(
		pathSeparator(destinationPath || preview?.full_path || preview?.source_path || preview?.subfolder_path)
	);

	function getOperationLabel(mode?: string): string {
		switch (mode) {
			case 'in-place': return m.browse_op_reorganize();
			case 'in-place-norenamefolder': return m.browse_op_rename_only();
			case 'metadata-artwork': return m.browse_op_metadata_artwork();
			case 'organize': return m.browse_op_organize();
			default: return m.browse_op_organize();
		}
	}

	function extractFileName(path: string): string {
		return path.split(/[\\/]/).pop() || path;
	}

	function extractParentDir(path: string): string {
		const isUnc = path.startsWith('\\\\');
		const isWindows = path.includes('\\');
		const isAbsPosix = !isWindows && path.startsWith('/');
		const sep = isWindows ? '\\' : '/';
		const parts = path.split(/[\\/]/).filter(Boolean);
		parts.pop();
		const result = parts.join(sep);
		if (!result) return '/';
		if (isUnc) return '\\\\' + result;
		if (isAbsPosix) return '/' + result;
		return result;
	}
</script>

{#snippet screenshotList(indentPx: number)}
	{#if preview!.screenshots && preview!.screenshots!.length > 0}
		<div class="text-muted-foreground break-all" style="margin-left: {indentPx}px">📁 extrafanart{sep}</div>
		{#each (showAllScreenshots ? preview!.screenshots! : preview!.screenshots!.slice(0, 3)) as screenshot}
			<div class="break-all" style="margin-left: {indentPx + 4}px">🖼️ {screenshot}</div>
		{/each}
		{#if preview!.screenshots!.length > 3 && !showAllScreenshots}
			<button
				onclick={() => (showAllScreenshots = true)}
				class="text-muted-foreground hover:text-primary transition-colors cursor-pointer text-left"
				style="margin-left: {indentPx + 4}px"
			>
				{m.review_screenshots_more({ count: preview!.screenshots!.length - 3 })}
			</button>
		{/if}
		{#if showAllScreenshots && preview!.screenshots!.length > 3}
			<button
				onclick={() => (showAllScreenshots = false)}
				class="text-muted-foreground hover:text-primary transition-colors cursor-pointer text-left"
				style="margin-left: {indentPx + 4}px"
			>
				{m.review_show_less()}
			</button>
		{/if}
	{/if}
{/snippet}

{#if preview}
	{@const opMode = preview.operation_mode || 'organize'}
	{@const isInPlaceMode = opMode === 'in-place-norenamefolder'}
	{@const isMetadataArtwork = opMode === 'metadata-artwork'}
	{@const isInPlace = opMode === 'in-place'}

	{#if isInPlaceMode}
		<div class="p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
			<p class="text-xs font-medium mb-1 text-muted-foreground">{getOperationLabel(opMode)}:</p>
			<div class="font-mono text-xs space-y-1 overflow-x-auto">
				{#if preview.source_path}
					<div class="text-muted-foreground break-all">📄 {preview.source_path}</div>
					<div class="text-muted-foreground">→</div>
				{/if}
				{#if preview.video_files && preview.video_files.length > 0}
					{#each preview.video_files as videoFile}
						<div class="break-all">🎬 {extractFileName(videoFile)}</div>
					{/each}
				{:else}
					<div class="break-all">🎬 {preview.file_name}.mp4</div>
				{/if}
				{#if preview.nfo_path || (preview.nfo_paths && preview.nfo_paths.length > 0)}
					{#if preview.nfo_paths && preview.nfo_paths.length > 0}
						{#each preview.nfo_paths as nfoFile}
							<div class="break-all">📄 {extractFileName(nfoFile)}</div>
						{/each}
					{:else if preview.nfo_path}
						<div class="break-all">📄 {extractFileName(preview.nfo_path)}</div>
					{/if}
				{/if}
				{#if preview.poster_path}
					<div class="break-all">🖼️ {extractFileName(preview.poster_path)}</div>
				{/if}
				{#if preview.fanart_path}
					<div class="break-all">🖼️ {extractFileName(preview.fanart_path)}</div>
				{/if}
				{#if preview.trailer_path}
					<div class="break-all">🎞️ {extractFileName(preview.trailer_path)}</div>
				{/if}
				{@render screenshotList(4)}
			</div>
		</div>
	{:else if isInPlace}
		<div class="p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
			<p class="text-xs font-medium mb-1 text-muted-foreground">{getOperationLabel(opMode)}:</p>
			<div class="font-mono text-xs space-y-1 overflow-x-auto">
				{#if preview.source_path}
					<div class="text-muted-foreground break-all">📄 {preview.source_path}</div>
					<div class="text-muted-foreground">→</div>
				{/if}
				{#if preview.full_path}
					{@const targetDir = extractParentDir(preview.full_path)}
					<div class="text-muted-foreground break-all">📁 {targetDir}{sep}</div>
				{:else if preview.folder_name}
					<div class="text-muted-foreground break-all">📁 {preview.folder_name}{sep}</div>
				{/if}
				{#if preview.video_files && preview.video_files.length > 0}
					{#each preview.video_files as videoFile}
						<div class="break-all" style="margin-left: 4px">🎬 {extractFileName(videoFile)}</div>
					{/each}
				{:else}
					<div class="break-all" style="margin-left: 4px">🎬 {preview.file_name}.mp4</div>
				{/if}
				{#if preview.nfo_path || (preview.nfo_paths && preview.nfo_paths.length > 0)}
					{#if preview.nfo_paths && preview.nfo_paths.length > 0}
						{#each preview.nfo_paths as nfoFile}
							<div class="break-all" style="margin-left: 4px">📄 {extractFileName(nfoFile)}</div>
						{/each}
					{:else if preview.nfo_path}
						<div class="break-all" style="margin-left: 4px">📄 {extractFileName(preview.nfo_path)}</div>
					{/if}
				{/if}
				{#if preview.poster_path}
					<div class="break-all" style="margin-left: 4px">🖼️ {extractFileName(preview.poster_path)}</div>
				{/if}
				{#if preview.fanart_path}
					<div class="break-all" style="margin-left: 4px">🖼️ {extractFileName(preview.fanart_path)}</div>
				{/if}
				{#if preview.trailer_path}
					<div class="break-all" style="margin-left: 4px">🎞️ {extractFileName(preview.trailer_path)}</div>
				{/if}
				{@render screenshotList(8)}
			</div>
		</div>
	{:else if isMetadataArtwork}
		<div class="p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
			<p class="text-xs font-medium mb-1 text-muted-foreground">{getOperationLabel(opMode)} (no file changes):</p>
			<div class="font-mono text-xs space-y-1 overflow-x-auto">
				{#if preview.source_path}
					<div class="text-muted-foreground break-all">📄 {preview.source_path}</div>
				{/if}
				{#if preview.nfo_path || (preview.nfo_paths && preview.nfo_paths.length > 0)}
					{#if preview.nfo_paths && preview.nfo_paths.length > 0}
						{#each preview.nfo_paths as nfoFile}
							<div class="break-all">📄 {extractFileName(nfoFile)}</div>
						{/each}
					{:else if preview.nfo_path}
						<div class="break-all">📄 {extractFileName(preview.nfo_path)}</div>
					{/if}
				{/if}
				{#if preview.poster_path}
					<div class="break-all">🖼️ {extractFileName(preview.poster_path)}</div>
				{/if}
				{#if preview.fanart_path}
					<div class="break-all">🖼️ {extractFileName(preview.fanart_path)}</div>
				{/if}
				{#if preview.trailer_path}
					<div class="break-all">🎞️ {extractFileName(preview.trailer_path)}</div>
				{/if}
				{@render screenshotList(4)}
			</div>
		</div>
	{:else}
		{@const subfolderParts = preview.subfolder_path ? preview.subfolder_path.split(/[\\/]/).filter(Boolean) : []}
		{@const allPathParts = [...subfolderParts, preview.folder_name].filter(Boolean)}
		{@const fileIndent = allPathParts.length * 4}
		<div class="p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
			<p class="text-xs font-medium mb-2 text-muted-foreground">{m.review_preview_label()}</p>
			<div class="font-mono text-xs space-y-1 overflow-x-auto">
				<div class="text-muted-foreground break-all">📁 {destinationPath}{sep}</div>
				{#each allPathParts as part, index}
					<div class="text-muted-foreground break-all" style="margin-left: {(index + 1) * 4}px">📁 {part}{sep}</div>
				{/each}
				{#if preview.video_files && preview.video_files.length > 0}
					{#each preview.video_files as videoFile}
						<div class="break-all" style="margin-left: {fileIndent + 4}px">🎬 {extractFileName(videoFile)}</div>
					{/each}
				{:else}
					<div class="break-all" style="margin-left: {fileIndent + 4}px">🎬 {preview.file_name}.mp4</div>
				{/if}
				{#if preview.nfo_path || (preview.nfo_paths && preview.nfo_paths.length > 0)}
					{#if preview.nfo_paths && preview.nfo_paths.length > 0}
						{#each preview.nfo_paths as nfoFile}
							<div class="break-all" style="margin-left: {fileIndent + 4}px">📄 {extractFileName(nfoFile)}</div>
						{/each}
					{:else if preview.nfo_path}
						<div class="break-all" style="margin-left: {fileIndent + 4}px">📄 {extractFileName(preview.nfo_path)}</div>
					{/if}
				{/if}
				{#if preview.poster_path}
					<div class="break-all" style="margin-left: {fileIndent + 4}px">🖼️ {extractFileName(preview.poster_path)}</div>
				{/if}
				{#if preview.fanart_path}
					<div class="break-all" style="margin-left: {fileIndent + 4}px">🖼️ {extractFileName(preview.fanart_path)}</div>
				{/if}
				{#if preview.trailer_path}
					<div class="break-all" style="margin-left: {fileIndent + 4}px">🎞️ {extractFileName(preview.trailer_path)}</div>
				{/if}
				{@render screenshotList(fileIndent + 4)}
			</div>
		</div>
	{/if}
{:else}
	<p class="text-xs text-muted-foreground">{m.review_files_organized_in_dir()}</p>
{/if}