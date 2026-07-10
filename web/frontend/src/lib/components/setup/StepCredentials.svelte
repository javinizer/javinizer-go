<script lang="ts">
	import { ShieldCheck, KeyRound, User, Eye, EyeOff } from 'lucide-svelte';

	interface Credentials {
		username: string;
		password: string;
		confirm: string;
	}

	let {
		credentials = $bindable(),
		error = null as string | null,
		submitting = false,
		onSubmit,
	}: {
		credentials: Credentials;
		error?: string | null;
		submitting?: boolean;
		onSubmit?: () => void;
	} = $props();

	let showPassword = $state(false);
	let showConfirm = $state(false);

	let passwordStrength = $derived.by(() => {
		const p = credentials.password;
		if (p.length === 0) return { score: 0, label: '', pct: 0 };
		let score = 0;
		if (p.length >= 8) score++;
		if (p.length >= 12) score++;
		if (/[A-Z]/.test(p) && /[a-z]/.test(p)) score++;
		if (/\d/.test(p) && /[^A-Za-z0-9]/.test(p)) score++;
		const labels = ['', 'Weak', 'Fair', 'Good', 'Strong'];
		return { score, label: labels[score], pct: (score / 4) * 100 };
	});

	let match = $derived(
		credentials.confirm.length === 0 || credentials.password === credentials.confirm,
	);
</script>

<div class="step-head">
	<div class="step-badge"><ShieldCheck class="h-5 w-5" /></div>
	<h1 class="step-title">Create your admin account</h1>
	<p class="step-sub">
		This becomes the master login for this Javinizer server. You can add more users later.
	</p>
</div>

<form id="credentials-form" class="step-body" onsubmit={(e) => { e.preventDefault(); onSubmit?.(); }}>
	{#if error}
		<div class="alert" role="alert">{error}</div>
	{/if}

	<label class="field">
		<span class="field-label">Username</span>
		<div class="field-control">
			<User class="field-icon" />
			<input
				class="field-input pl-9"
				type="text"
				required
				autocomplete="username"
				placeholder="admin"
				bind:value={credentials.username}
				disabled={submitting}
			/>
		</div>
	</label>

	<label class="field">
		<span class="field-label">Password</span>
		<div class="field-control">
			<KeyRound class="field-icon" />
			<input
				class="field-input pl-9 pr-9"
				type={showPassword ? 'text' : 'password'}
				required
				minlength="8"
				autocomplete="new-password"
				placeholder="At least 8 characters"
				bind:value={credentials.password}
				disabled={submitting}
			/>
			<button
				type="button"
				class="field-eye"
				onclick={() => (showPassword = !showPassword)}
				tabindex="0"
				disabled={submitting}
				aria-label={showPassword ? 'Hide password' : 'Show password'}
			>
				{#if showPassword}<EyeOff class="h-4 w-4" />{:else}<Eye class="h-4 w-4" />{/if}
			</button>
		</div>
		{#if credentials.password.length > 0}
			<div class="strength">
				<div class="strength-bar"><span class="strength-fill" data-score={passwordStrength.score} style="width: {passwordStrength.pct}%"></span></div>
				<span class="strength-label" data-score={passwordStrength.score}>{passwordStrength.label}</span>
			</div>
		{/if}
	</label>

	<label class="field">
		<span class="field-label">Confirm password</span>
		<div class="field-control">
			<KeyRound class="field-icon" />
			<input
				class="field-input pl-9 pr-9"
				type={showConfirm ? 'text' : 'password'}
				required
				minlength="8"
				autocomplete="new-password"
				placeholder="Re-enter password"
				bind:value={credentials.confirm}
				disabled={submitting}
			/>
			<button
				type="button"
				class="field-eye"
				onclick={() => (showConfirm = !showConfirm)}
				tabindex="0"
				disabled={submitting}
				aria-label={showConfirm ? 'Hide password' : 'Show password'}
			>
				{#if showConfirm}<EyeOff class="h-4 w-4" />{:else}<Eye class="h-4 w-4" />{/if}
			</button>
		</div>
		{#if credentials.confirm.length > 0 && !match}
			<span class="field-hint field-hint-error">Passwords do not match</span>
		{/if}
	</label>
</form>

<button type="submit" form="credentials-form" class="sr-only" aria-hidden="true" tabindex="-1"></button>

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

	.step-body {
		display: flex;
		flex-direction: column;
		gap: 1.1rem;
	}

	.alert {
		border: 1px solid hsl(var(--destructive) / 0.4);
		background: hsl(var(--destructive) / 0.1);
		color: hsl(var(--destructive));
		padding: 0.55rem 0.75rem;
		border-radius: 0.5rem;
		font-size: 0.85rem;
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
	}

	.field-label {
		font-size: 0.82rem;
		font-weight: 600;
	}

	.field-control {
		position: relative;
		display: flex;
		align-items: center;
	}

	:global(.field-icon) {
		position: absolute;
		left: 0.7rem;
		width: 16px;
		height: 16px;
		color: hsl(var(--muted-foreground));
		pointer-events: none;
	}

	.field-input {
		width: 100%;
		border: 1px solid hsl(var(--border));
		background: hsl(var(--background));
		border-radius: 0.5rem;
		padding: 0.6rem 0.75rem;
		font-size: 0.92rem;
		outline: none;
		transition: border-color 160ms, box-shadow 160ms;
	}

	.field-input:focus {
		border-color: hsl(var(--primary));
		box-shadow: 0 0 0 3px hsl(var(--primary) / 0.18);
	}

	.field-input:disabled {
		opacity: 0.6;
	}

	.field-input.pl-9 {
		padding-left: 2.5rem;
	}

	.field-input.pr-9 {
		padding-right: 2.5rem;
	}

	.field-eye {
		position: absolute;
		right: 0.5rem;
		display: inline-flex;
		padding: 0.3rem;
		border-radius: 0.375rem;
		color: hsl(var(--muted-foreground));
		background: transparent;
		border: none;
		cursor: pointer;
	}

	.field-eye:hover {
		color: hsl(var(--foreground));
	}

	.field-hint {
		font-size: 0.78rem;
	}

	.field-hint-error {
		color: hsl(var(--destructive));
	}

	.strength {
		display: flex;
		align-items: center;
		gap: 0.6rem;
	}

	.strength-bar {
		flex: 1;
		height: 5px;
		border-radius: 9999px;
		background: hsl(var(--muted));
		overflow: hidden;
	}

	.strength-fill {
		display: block;
		height: 100%;
		border-radius: 9999px;
		transition: width 240ms cubic-bezier(0.33, 1, 0.68, 1), background 240ms;
		background: hsl(var(--destructive));
	}

	.strength-fill[data-score='2'] {
		background: hsl(38 92% 50%);
	}

	.strength-fill[data-score='3'] {
		background: hsl(142 71% 45%);
	}

	.strength-fill[data-score='4'] {
		background: hsl(152 76% 40%);
	}

	.strength-label {
		font-size: 0.72rem;
		font-weight: 600;
		min-width: 3rem;
		text-align: right;
		color: hsl(var(--muted-foreground));
	}

	.strength-label[data-score='1'] {
		color: hsl(var(--destructive));
	}

	.strength-label[data-score='2'] {
		color: hsl(38 92% 45%);
	}

	.strength-label[data-score='3'],
	.strength-label[data-score='4'] {
		color: hsl(142 71% 40%);
	}
</style>
