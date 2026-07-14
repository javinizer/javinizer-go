<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { untrack } from 'svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
	}

	let { config, inputClass, selectClass }: Props = $props();
	let logging = $derived(config.logging);

	function coerceToInt(value: string | number): number {
		if (typeof value === 'number') return value < 0 ? 0 : value;
		const num = parseInt(value, 10);
		if (isNaN(num) || num < 0) return 0;
		return num;
	}

	function ensureLoggingDefaults(cfg: SettingsConfig): void {
		if (!cfg.logging) cfg.logging = {};
		cfg.logging.level ??= 'info';
		cfg.logging.format ??= 'text';
		cfg.logging.output ??= 'stdout';
		cfg.logging.max_size_mb ??= 0;
		cfg.logging.max_backups ??= 0;
		cfg.logging.max_age_days ??= 0;
		cfg.logging.compress ??= false;
	}

	$effect(() => {
		if (config) {
			untrack(() => ensureLoggingDefaults(config));
		}
	});
</script>

<SettingsSection title={m.settings_logging_title()} description={m.settings_logging_desc()} defaultExpanded={false}>
	<div class="space-y-4">
		<div>
			<label class="block text-sm font-medium mb-2" for="log-level">{m.settings_logging_level_label()}</label>
			<select id="log-level" bind:value={logging.level} class={selectClass}>
				<option value="debug">{m.settings_logging_level_debug()}</option>
				<option value="info">{m.settings_logging_level_info()}</option>
				<option value="warn">{m.settings_logging_level_warn()}</option>
				<option value="error">{m.settings_logging_level_error()}</option>
			</select>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="log-format">{m.settings_logging_format_label()}</label>
			<select id="log-format" bind:value={logging.format} class={selectClass}>
				<option value="text">{m.settings_logging_format_text()}</option>
				<option value="json">{m.settings_logging_format_json()}</option>
			</select>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="log-output">{m.settings_logging_output_label()}</label>
			<input
				id="log-output"
				type="text"
				bind:value={logging.output}
				class={inputClass}
				placeholder={m.settings_logging_output_placeholder()}
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_logging_output_desc()}
			</p>
		</div>

		<SettingsSubsection title={m.settings_logging_rotation_subsection()} description={m.settings_logging_rotation_desc()}>
			<div class="space-y-4">
				<div>
					<label class="block text-sm font-medium mb-2" for="log-max-size">{m.settings_logging_max_size_label()}</label>
					<input
						id="log-max-size"
						type="number"
						value={logging.max_size_mb}
						oninput={(e) => { logging.max_size_mb = coerceToInt((e.target as HTMLInputElement).value); }}
						class={inputClass}
						min="0"
						placeholder="10"
					/>
					<p class="text-xs text-muted-foreground mt-1">
						{m.settings_logging_max_size_desc()}
					</p>
				</div>

				<div>
					<label class="block text-sm font-medium mb-2" for="log-max-backups">{m.settings_logging_max_backups_label()}</label>
					<input
						id="log-max-backups"
						type="number"
						value={logging.max_backups}
						oninput={(e) => { logging.max_backups = coerceToInt((e.target as HTMLInputElement).value); }}
						class={inputClass}
						min="0"
						placeholder="5"
					/>
					<p class="text-xs text-muted-foreground mt-1">
						{m.settings_logging_max_backups_desc()}
					</p>
				</div>

				<div>
					<label class="block text-sm font-medium mb-2" for="log-max-age">{m.settings_logging_max_age_label()}</label>
					<input
						id="log-max-age"
						type="number"
						value={logging.max_age_days}
						oninput={(e) => { logging.max_age_days = coerceToInt((e.target as HTMLInputElement).value); }}
						class={inputClass}
						min="0"
						placeholder="0"
					/>
					<p class="text-xs text-muted-foreground mt-1">
						{m.settings_logging_max_age_desc()}
					</p>
				</div>

				<div class="flex items-center gap-2">
					<input
						id="log-compress"
						type="checkbox"
						bind:checked={logging.compress}
						class="w-4 h-4"
					/>
					<label class="text-sm font-medium" for="log-compress">{m.settings_logging_compress_label()}</label>
				</div>
			</div>
		</SettingsSubsection>
	</div>
</SettingsSection>
