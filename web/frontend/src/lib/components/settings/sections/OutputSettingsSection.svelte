<script lang="ts">
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import FormTemplateInput from '$lib/components/settings/FormTemplateInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';

	interface Props {
		config: any;
		inputClass: string;
	}

	let { config, inputClass }: Props = $props();
</script>

<SettingsSection title="Output Settings" description="Configure output paths, templates, and download options" defaultExpanded={false}>
	<div class="space-y-4">
		<SettingsSubsection title="Template Options">
			<FormNumberInput
				label="Max title length"
				description="Maximum characters for movie titles in folder names. Longer titles will be intelligently truncated."
				value={config.output.max_title_length ?? 100}
				min={10}
				max={500}
				unit="characters"
				onchange={(val) => {
					config.output.max_title_length = val;
				}}
			/>

			<FormNumberInput
				label="Max path length"
				description="Maximum total path length to prevent Windows path errors (MAX_PATH = 260)"
				value={config.output.max_path_length ?? 240}
				min={100}
				max={250}
				unit="characters"
				onchange={(val) => {
					config.output.max_path_length = val;
				}}
			/>

			<FormToggle
				label="Group actress"
				description="Group actress names with @ prefix (e.g., '@GroupName')"
				checked={config.output.group_actress ?? false}
				onchange={(val) => {
					config.output.group_actress = val;
				}}
			/>

			<div class="py-4 border-b border-border">
				<label class="block text-sm font-medium mb-2" for="delimiter">Delimiter</label>
				<input
					id="delimiter"
					type="text"
					bind:value={config.output.delimiter}
					class={inputClass}
					placeholder=", "
				/>
				<p class="text-xs text-muted-foreground mt-1">
					Character(s) used to separate multiple values (e.g., actresses, genres)
				</p>
			</div>
		</SettingsSubsection>

		<div>
			<label class="block text-sm font-medium mb-2" for="subfolder-format">Subfolder Format</label>
			<input
				id="subfolder-format"
				type="text"
				value={config.output.subfolder_format.join(', ')}
				onchange={(e) => {
					config.output.subfolder_format = e.currentTarget.value
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
				<input type="checkbox" bind:checked={config.output.download_poster} class="rounded" />
				<span>Download Poster</span>
			</label>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_cover} class="rounded" />
				<span>Download Cover</span>
			</label>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_extrafanart} class="rounded" />
				<span>Download Extrafanart</span>
			</label>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_trailer} class="rounded" />
				<span>Download Trailer</span>
			</label>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_actress} class="rounded" />
				<span>Download Actress Images</span>
			</label>
		</div>

		<FormNumberInput
			label="Download timeout"
			description="Maximum time to wait for image/video downloads to complete"
			value={config.output.download_timeout ?? 60}
			min={5}
			max={600}
			unit="seconds"
			onchange={(val) => {
				config.output.download_timeout = val;
			}}
		/>

		<div>
			<label class="block text-sm font-medium mb-2" for="folder-format">Folder Naming Template</label>
			<input
				id="folder-format"
				type="text"
				bind:value={config.output.folder_format}
				class="{inputClass} font-mono text-sm"
				placeholder="<ID> - <TITLE>"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				Available tags: &lt;ID&gt;, &lt;TITLE&gt;, &lt;STUDIO&gt;, &lt;YEAR&gt;, &lt;ACTRESS&gt;, &lt;ACTRESSES&gt;, &lt;ACTORNAME&gt;
			</p>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="file-format">File Naming Template</label>
			<input
				id="file-format"
				type="text"
				bind:value={config.output.file_format}
				class="{inputClass} font-mono text-sm"
				placeholder="<ID><PARTSUFFIX>"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				Multi-part support: &lt;PART&gt; (part number), &lt;PARTSUFFIX&gt; (original suffix), &lt;IF:MULTIPART&gt;...&lt;/IF&gt;
			</p>
			<p class="text-xs text-muted-foreground">
				Examples: &lt;ID&gt;&lt;PARTSUFFIX&gt; or &lt;ID&gt;-CD&lt;PART:2&gt; or &lt;ID&gt;&lt;IF:MULTIPART&gt;-pt&lt;PART&gt;&lt;/IF&gt;
			</p>
		</div>

		<SettingsSubsection title="Media File Naming">
			<FormTemplateInput
				label="Poster format"
				description="Naming template for poster images"
				value={config.output.poster_format ?? '<ID>-poster.jpg'}
				placeholder="<ID>-poster.jpg"
				showTagList={true}
				onchange={(val) => {
					config.output.poster_format = val;
				}}
			/>

			<FormTemplateInput
				label="Fanart format"
				description="Naming template for fanart/cover images"
				value={config.output.fanart_format ?? '<ID>-fanart.jpg'}
				placeholder="<ID>-fanart.jpg"
				onchange={(val) => {
					config.output.fanart_format = val;
				}}
			/>

			<FormTemplateInput
				label="Trailer format"
				description="Naming template for trailer videos"
				value={config.output.trailer_format ?? '<ID>-trailer.mp4'}
				placeholder="<ID>-trailer.mp4"
				onchange={(val) => {
					config.output.trailer_format = val;
				}}
			/>

			<FormTemplateInput
				label="Screenshot format"
				description="Naming template for screenshot images"
				value={config.output.screenshot_format ?? 'fanart'}
				placeholder="fanart"
				onchange={(val) => {
					config.output.screenshot_format = val;
				}}
			/>

			<FormTextInput
				label="Screenshot folder"
				description="Folder name for storing screenshot images"
				value={config.output.screenshot_folder ?? 'extrafanart'}
				placeholder="extrafanart"
				onchange={(val) => {
					config.output.screenshot_folder = val;
				}}
			/>

			<FormNumberInput
				label="Screenshot padding"
				description="Zero-padding for screenshot numbers (e.g., 01, 02, 03)"
				value={config.output.screenshot_padding ?? 1}
				min={1}
				max={5}
				unit="digits"
				onchange={(val) => {
					config.output.screenshot_padding = val;
				}}
			/>

			<FormTextInput
				label="Actress folder"
				description="Folder name for storing actress images"
				value={config.output.actress_folder ?? '.actors'}
				placeholder=".actors"
				onchange={(val) => {
					config.output.actress_folder = val;
				}}
			/>

			<FormTemplateInput
				label="Actress format"
				description="Naming template for actress image files"
				value={config.output.actress_format ?? '<ACTORNAME>.jpg'}
				placeholder="<ACTORNAME>.jpg"
				onchange={(val) => {
					config.output.actress_format = val;
				}}
			/>
		</SettingsSubsection>
	</div>
</SettingsSection>
