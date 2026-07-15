<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
	}

	let { config, inputClass, selectClass }: Props = $props();
</script>

<SettingsSection title={m.settings_file_matching_title()} description={m.settings_file_matching_desc()} defaultExpanded={false}>
	<div class="space-y-4">
		<div>
			<label class="block text-sm font-medium mb-2" for="file-extensions">{m.settings_file_matching_extensions_label()}</label>
			<input
				id="file-extensions"
				type="text"
				value={config.file_matching.extensions?.join(', ') ?? ''}
				onchange={(e) => {
					config.file_matching.extensions = e.currentTarget.value
						.split(',')
						.map((s) => s.trim());
				}}
				class={inputClass}
				placeholder=".mp4, .mkv, .avi"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_file_matching_extensions_desc()}
			</p>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="min-size-mb">{m.settings_file_matching_min_size_label()}</label>
			<input
				id="min-size-mb"
				type="number"
				bind:value={config.file_matching.min_size_mb}
				class={inputClass}
				min="0"
				max="10000"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_file_matching_min_size_desc()}
			</p>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="exclude-patterns">{m.settings_file_matching_exclude_label()}</label>
			<input
				id="exclude-patterns"
				type="text"
				value={config.file_matching.exclude_patterns?.join(', ') ?? ''}
				onchange={(e) => {
					config.file_matching.exclude_patterns = e.currentTarget.value
						.split(',')
						.map((s) => s.trim())
						.filter((s) => s.length > 0);
				}}
				class={inputClass}
				placeholder="*-trailer*, *-sample*"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_file_matching_exclude_desc()}
			</p>
		</div>

			<div class="space-y-3">
				<label class="flex items-center gap-2">
					<input type="checkbox" bind:checked={config.file_matching.regex_enabled} class="rounded" />
					<span>{m.settings_file_matching_regex_enable()}</span>
				</label>
			</div>

			<fieldset disabled={!config.file_matching.regex_enabled} class={`${!config.file_matching.regex_enabled ? 'opacity-60' : ''}`}>
				<div>
					<label class="block text-sm font-medium mb-2" for="regex-pattern">{m.settings_file_matching_regex_label()}</label>
					<input
						id="regex-pattern"
						type="text"
						bind:value={config.file_matching.regex_pattern}
						class="{inputClass} font-mono text-sm"
					/>
					<p class="text-xs text-muted-foreground mt-1">
						{m.settings_file_matching_regex_desc()}
					</p>
				</div>
			</fieldset>
		</div>
	</SettingsSection>
