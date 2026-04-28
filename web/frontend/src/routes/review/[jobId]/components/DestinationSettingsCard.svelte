<script lang="ts">
	import type { OrganizePreviewResponse } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { FolderOpen } from 'lucide-svelte';

	type OrganizeOperation = 'move' | 'copy' | 'hardlink' | 'softlink';

	interface Props {
		destinationPath: string;
		organizeOperation: OrganizeOperation;
		preview: OrganizePreviewResponse | null;
		previewNeedsDestination: boolean;
		effectiveOperationMode?: string;
		showAllPreviewScreenshots: boolean;
		skipNfo?: boolean;
		skipDownload?: boolean;
		onOpenDestinationBrowser: () => void;
	}

	let {
		destinationPath = $bindable(''),
		organizeOperation = $bindable<OrganizeOperation>('move'),
		preview,
		previewNeedsDestination = false,
		effectiveOperationMode,
		showAllPreviewScreenshots = $bindable(false),
		skipNfo = $bindable(false),
		skipDownload = $bindable(false),
		onOpenDestinationBrowser
	}: Props = $props();

	let opMode = $derived(preview?.operation_mode || effectiveOperationMode || 'organize');
	let needsDestination = $derived(opMode === 'organize');
	let isInPlaceImplied = $derived(
		effectiveOperationMode === 'organize' && opMode === 'in-place-norenamefolder'
	);

	function getOperationLabel(mode?: string): string {
		switch (mode) {
			case 'in-place': return 'Reorganize in place';
			case 'in-place-norenamefolder': return 'Rename file only';
			case 'metadata-only': return 'Metadata only';
			case 'organize': return 'Organize';
			default: return 'Organize';
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

<Card class="p-4">
	<div class="space-y-3 min-w-0">
		<div class="flex items-center gap-2">
			<FolderOpen class="h-5 w-5 text-primary" />
			<h3 class="font-semibold">
				{needsDestination ? 'Output Destination' : 'File Operations'}
			</h3>
		</div>

		{#if needsDestination}
			<div class="flex gap-2 min-w-0">
				<input
					type="text"
					bind:value={destinationPath}
					placeholder="Enter destination path (e.g., /path/to/output)"
					class="flex-1 min-w-0 px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
					title={destinationPath}
				/>
				<Button onclick={onOpenDestinationBrowser} variant="outline">
					{#snippet children()}
						<FolderOpen class="h-4 w-4 mr-2" />
						Browse
					{/snippet}
				</Button>
			</div>

			{#if previewNeedsDestination && !destinationPath.trim()}
				<p class="text-xs text-muted-foreground">
					Set a destination path to see the organization preview.
				</p>
			{/if}

			<div class="space-y-2">
				<label for="organizeOperation" class="text-sm font-medium">File operation</label>
				<select
					id="organizeOperation"
					bind:value={organizeOperation}
					class="w-full px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all text-sm"
				>
					<option value="move">Move files</option>
					<option value="copy">Copy files</option>
					<option value="hardlink">Hard link files</option>
					<option value="softlink">Soft link files</option>
				</select>
				<p class="text-xs text-muted-foreground">
					{#if organizeOperation === 'hardlink'}
						Hard links require source and destination on the same filesystem.
					{:else if organizeOperation === 'softlink'}
						Soft links point to the original file path. Windows may require Developer Mode or elevated privileges.
					{:else if organizeOperation === 'copy'}
						Copy creates independent destination files and keeps originals unchanged.
					{:else}
						Move relocates source files into the organized destination.
					{/if}
				</p>
			</div>
		{:else}
			<p class="text-xs text-muted-foreground">
				{getOperationLabel(opMode)} — files stay in their current location.
			</p>
			{#if isInPlaceImplied}
				<p class="text-xs text-primary mt-1">
					Auto-switched from Organize: destination matches source path with no folder/subfolder format.
				</p>
			{/if}
		{/if}

		<div class="grid gap-3 md:grid-cols-2">
			<label
				class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
			>
				<input
					type="checkbox"
					bind:checked={skipNfo}
					class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
				/>
				<div class="flex-1">
					<span class="text-sm font-medium">Skip NFO Generation</span>
					<p class="text-xs text-muted-foreground">Don't create NFO metadata files</p>
				</div>
			</label>

			<label
				class="flex items-center gap-3 p-3 rounded-lg border border-border bg-background hover:bg-accent/50 cursor-pointer transition-colors"
			>
				<input
					type="checkbox"
					bind:checked={skipDownload}
					class="h-4 w-4 rounded border-input text-primary focus:ring-2 focus:ring-primary"
				/>
				<div class="flex-1">
					<span class="text-sm font-medium">Skip Media Download</span>
					<p class="text-xs text-muted-foreground">Don't download cover, poster, and screenshots</p>
					</div>
			</label>
		</div>

		{#if preview}
			{@const opMode = preview.operation_mode || 'organize'}
			{@const isInPlaceMode = opMode === 'in-place-norenamefolder'}
			{@const isMetadataOnly = opMode === 'metadata-only'}
			{@const isInPlace = opMode === 'in-place'}

			{#snippet screenshotList(indentPx: number)}
				{#if preview!.screenshots && preview!.screenshots!.length > 0}
					<div class="text-muted-foreground break-all" style="margin-left: {indentPx}px">📁 extrafanart/</div>
					{#each (showAllPreviewScreenshots ? preview!.screenshots! : preview!.screenshots!.slice(0, 3)) as screenshot}
						<div class="break-all" style="margin-left: {indentPx + 4}px">🖼️ {screenshot}</div>
					{/each}
					{#if preview!.screenshots!.length > 3 && !showAllPreviewScreenshots}
						<button
							onclick={() => (showAllPreviewScreenshots = true)}
							class="text-muted-foreground hover:text-primary transition-colors cursor-pointer text-left"
							style="margin-left: {indentPx + 4}px"
						>
							... and {preview!.screenshots!.length - 3} more
						</button>
					{/if}
					{#if showAllPreviewScreenshots && preview!.screenshots!.length > 3}
						<button
							onclick={() => (showAllPreviewScreenshots = false)}
							class="text-muted-foreground hover:text-primary transition-colors cursor-pointer text-left"
							style="margin-left: {indentPx + 4}px"
						>
							Show less
						</button>
					{/if}
				{/if}
			{/snippet}

			{#if isInPlaceMode}
				<div class="mt-3 p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
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
						{@render screenshotList(4)}
					</div>
				</div>
		{:else if isInPlace}
			<div class="mt-3 p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
				<p class="text-xs font-medium mb-1 text-muted-foreground">{getOperationLabel(opMode)}:</p>
				<div class="font-mono text-xs space-y-1 overflow-x-auto">
					{#if preview.source_path}
						<div class="text-muted-foreground break-all">📄 {preview.source_path}</div>
						<div class="text-muted-foreground">→</div>
					{/if}
					{#if preview.full_path}
						{@const targetDir = extractParentDir(preview.full_path)}
						<div class="text-muted-foreground break-all">📁 {targetDir}/</div>
					{:else if preview.folder_name}
						<div class="text-muted-foreground break-all">📁 {preview.folder_name}/</div>
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
					{@render screenshotList(8)}
				</div>
			</div>
			{:else if isMetadataOnly}
				<div class="mt-3 p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
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
						{@render screenshotList(4)}
					</div>
				</div>
			{:else}
				<!-- Organize mode: full destination path with folder hierarchy -->
				{@const subfolderParts = preview.subfolder_path ? preview.subfolder_path.split(/[\\/]/).filter(Boolean) : []}
				{@const allPathParts = [...subfolderParts, preview.folder_name].filter(Boolean)}
				{@const fileIndent = allPathParts.length * 4}
				<div class="mt-3 p-3 bg-accent/50 rounded border border-dashed overflow-hidden">
					<p class="text-xs font-medium mb-2 text-muted-foreground">Preview:</p>
					<div class="font-mono text-xs space-y-1 overflow-x-auto">
						<div class="text-muted-foreground break-all">📁 {destinationPath}/</div>
						{#each allPathParts as part, index}
							<div class="text-muted-foreground break-all" style="margin-left: {(index + 1) * 4}px">📁 {part}/</div>
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
						{@render screenshotList(fileIndent + 4)}
					</div>
				</div>
			{/if}
		{:else}
			<p class="text-xs text-muted-foreground">Files will be organized with metadata, artwork, and NFO files in this directory</p>
		{/if}
	</div>
</Card>