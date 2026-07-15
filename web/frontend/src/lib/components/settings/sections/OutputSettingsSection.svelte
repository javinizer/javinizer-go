<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import FormTemplateInput from '$lib/components/settings/FormTemplateInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
	}

	let { config, inputClass, selectClass }: Props = $props();
</script>

<SettingsSection title={m.settings_output_title()} description={m.settings_output_desc()} defaultExpanded={false}>
	<div class="space-y-4">
		<SettingsSubsection title={m.settings_output_template_subsection()}>
			<FormNumberInput
				label={m.settings_output_max_title_label()}
				description={m.settings_output_max_title_desc()}
				value={config.output.max_title_length ?? 100}
				min={10}
				max={500}
				unit={m.common_unit_characters()}
				onchange={(val) => {
					config.output.max_title_length = val;
				}}
			/>

			<FormNumberInput
				label={m.settings_output_max_path_label()}
				description={m.settings_output_max_path_desc()}
				value={config.output.max_path_length ?? 240}
				min={100}
				max={250}
				unit={m.common_unit_characters()}
				onchange={(val) => {
					config.output.max_path_length = val;
				}}
			/>

			<FormToggle
				label={m.settings_output_group_actress_label()}
				description={m.settings_output_group_actress_desc()}
				checked={config.output.group_actress ?? false}
				onchange={(val) => {
					config.output.group_actress = val;
				}}
			/>

			{#if config.output.group_actress}
				<div class="py-4 border-b border-border">
					<label class="block text-sm font-medium mb-2" for="group-actress-name">{m.settings_output_group_actress_name_label()}</label>
					<input
						id="group-actress-name"
						type="text"
						bind:value={config.output.group_actress_name}
						class={inputClass}
						placeholder="@Group"
					/>
					<p class="text-xs text-muted-foreground mt-1">
						{m.settings_output_group_actress_name_desc()}
					</p>
				</div>
			{/if}

			<div class="py-4 border-b border-border">
				<label class="block text-sm font-medium mb-2" for="delimiter">{m.settings_output_delimiter_label()}</label>
				<input
					id="delimiter"
					type="text"
					bind:value={config.output.actress_delimiter}
					class={inputClass}
					placeholder=", "
				/>
				<p class="text-xs text-muted-foreground mt-1">
					{m.settings_output_delimiter_desc()}
				</p>
			</div>
		</SettingsSubsection>

		<div>
			<label class="block text-sm font-medium mb-2" for="subfolder-format">{m.settings_output_subfolder_label()}</label>
			<input
				id="subfolder-format"
				type="text"
				value={config.output.subfolder_format?.join(', ') ?? ''}
				onchange={(e) => {
					config.output.subfolder_format = e.currentTarget.value
						.split(',')
						.map((s) => s.trim())
						.filter((s) => s.length > 0);
				}}
				class={inputClass}
				placeholder={m.settings_output_subfolder_placeholder()}
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_output_subfolder_desc()}
			</p>
		</div>

		<div class="space-y-3">
			<h3 class="font-medium">{m.settings_output_download_heading()}</h3>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_poster} class="rounded" />
				<span>{m.settings_output_download_poster()}</span>
			</label>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_cover} class="rounded" />
				<span>{m.settings_output_download_cover()}</span>
			</label>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_extrafanart} class="rounded" />
				<span>{m.settings_output_download_extrafanart()}</span>
			</label>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_trailer} class="rounded" />
				<span>{m.settings_output_download_trailer()}</span>
			</label>
			<label class="flex items-center gap-2">
				<input type="checkbox" bind:checked={config.output.download_actress} class="rounded" />
				<span>{m.settings_output_download_actress()}</span>
			</label>
		</div>

		<FormNumberInput
			label={m.settings_output_download_timeout_label()}
			description={m.settings_output_download_timeout_desc()}
			value={config.output.download_timeout ?? 60}
			min={5}
			max={600}
			unit="seconds"
			onchange={(val) => {
				config.output.download_timeout = val;
			}}
		/>

		<div>
			<label class="block text-sm font-medium mb-2" for="folder-format">{m.settings_output_folder_template_label()}</label>
			<input
				id="folder-format"
				type="text"
				bind:value={config.output.folder_format}
				class="{inputClass} font-mono text-sm"
				placeholder="<ID> - <TITLE>"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_output_folder_template_desc()}
			</p>
			{#if !config.output.folder_format}
				<p class="text-xs text-primary mt-1">
					{m.settings_output_folder_template_none()}
				</p>
			{/if}
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="file-format">{m.settings_output_file_template_label()}</label>
			<input
				id="file-format"
				type="text"
				bind:value={config.output.file_format}
				class="{inputClass} font-mono text-sm"
				placeholder="<ID><PARTSUFFIX>"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_output_file_template_desc()}
			</p>
			<p class="text-xs text-muted-foreground">
				{m.settings_output_file_template_examples()}
			</p>
		</div>

		<SettingsSubsection title={m.settings_output_media_subsection()}>
			<FormTemplateInput
				label={m.settings_output_poster_format_label()}
				description={m.settings_output_poster_format_desc()}
				value={config.output.poster_format ?? '<ID>-poster.jpg'}
				placeholder="<ID>-poster.jpg"
				showTagList={true}
				onchange={(val) => {
					config.output.poster_format = val;
				}}
			/>

			<FormTemplateInput
				label={m.settings_output_fanart_format_label()}
				description={m.settings_output_fanart_format_desc()}
				value={config.output.fanart_format ?? '<ID>-fanart.jpg'}
				placeholder="<ID>-fanart.jpg"
				onchange={(val) => {
					config.output.fanart_format = val;
				}}
			/>

			<FormTemplateInput
				label={m.settings_output_trailer_format_label()}
				description={m.settings_output_trailer_format_desc()}
				value={config.output.trailer_format ?? '<ID>-trailer.mp4'}
				placeholder="<ID>-trailer.mp4"
				onchange={(val) => {
					config.output.trailer_format = val;
				}}
			/>

			<FormTemplateInput
				label={m.settings_output_screenshot_format_label()}
				description={m.settings_output_screenshot_format_desc()}
				value={config.output.screenshot_format ?? 'fanart'}
				placeholder="fanart"
				onchange={(val) => {
					config.output.screenshot_format = val;
				}}
			/>

			<FormTextInput
				label={m.settings_output_screenshot_folder_label()}
				description={m.settings_output_screenshot_folder_desc()}
				value={config.output.screenshot_folder ?? 'extrafanart'}
				placeholder="extrafanart"
				onchange={(val) => {
					config.output.screenshot_folder = val;
				}}
			/>

			<FormNumberInput
				label={m.settings_output_screenshot_padding_label()}
				description={m.settings_output_screenshot_padding_desc()}
				value={config.output.screenshot_padding ?? 1}
				min={1}
				max={5}
				unit={m.common_unit_digits()}
				onchange={(val) => {
					config.output.screenshot_padding = val;
				}}
			/>

			<FormTextInput
				label={m.settings_output_actress_folder_label()}
				description={m.settings_output_actress_folder_desc()}
				value={config.output.actress_folder ?? '.actors'}
				placeholder=".actors"
				onchange={(val) => {
					config.output.actress_folder = val;
				}}
			/>

			<FormTemplateInput
				label={m.settings_output_actress_format_label()}
				description={m.settings_output_actress_format_desc()}
				value={config.output.actress_format ?? '<ACTORNAME>.jpg'}
				placeholder="<ACTORNAME>.jpg"
				onchange={(val) => {
					config.output.actress_format = val;
				}}
			/>
		</SettingsSubsection>
	</div>
</SettingsSection>
