<script lang="ts">
	import { fly, scale } from 'svelte/transition';
	import { cubicOut, quintOut } from 'svelte/easing';
	import { ShieldCheck, Fingerprint } from 'lucide-svelte';

	interface Props {
		username: string;
		sessionActive: boolean;
		registeredAt: Date;
	}

	let { username, sessionActive, registeredAt }: Props = $props();

	let time = $derived(
		registeredAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
	);
	let date = $derived(
		registeredAt
			.toLocaleDateString([], { year: 'numeric', month: 'short', day: '2-digit' })
			.toUpperCase(),
	);
</script>



<div class="success">
	<!-- Emblem -->
	<div class="emblem" in:scale={{ start: 0.6, duration: 520, easing: quintOut }}>
		<div class="emblem-burst"></div>
		<svg class="emblem-ring" viewBox="0 0 120 120" aria-hidden="true">
			<circle
				class="ring-track"
				cx="60"
				cy="60"
				r="52"
				fill="none"
				stroke-width="3"
			/>
			<circle
				class="ring-fill"
				cx="60"
				cy="60"
				r="52"
				fill="none"
				stroke-width="3"
				stroke-linecap="round"
				pathLength="100"
			/>
			<path
				class="ring-check"
				d="M42 61 L54 73 L80 47"
				fill="none"
				stroke-width="4.5"
				stroke-linecap="round"
				stroke-linejoin="round"
				pathLength="100"
			/>
		</svg>
		<div class="emblem-core">
			<ShieldCheck class="emblem-icon" />
		</div>
	</div>

	<!-- Heading -->
	<div class="head" in:fly={{ y: 14, duration: 460, delay: 280, easing: cubicOut }}>
		<span class="kicker">Account Registered</span>
		<h1 class="title">Your admin account is secured</h1>
		<p class="sub">
			Credentials have been registered and your session is now active. Keep these
			details safe — you'll use them to sign in to Javinizer.
		</p>
	</div>

	<!-- Credential receipt -->
	<dl class="receipt" in:fly={{ y: 18, duration: 480, delay: 420, easing: cubicOut }}>
		<div class="receipt-top">
			<span class="receipt-stamp">
				<Fingerprint class="h-3.5 w-3.5" />
				Javinizer · Credential Receipt
			</span>
			<span class="receipt-no">№ {username.slice(0, 4).toUpperCase().padEnd(4, '0')}-{registeredAt.getTime().toString().slice(-4)}</span>
		</div>

		<div class="receipt-rows">
			<div class="receipt-row">
				<dt>Username</dt>
				<dd class="mono">{username}</dd>
			</div>
			<div class="receipt-row">
				<dt>Password</dt>
				<dd class="mono masked" aria-label="password hidden">
					<span class="masked-bullets">
						{#each [...Array(8).keys()] as i (i)}<span class="dot" style="animation-delay: {600 + i * 60}ms"></span>{/each}
					</span>
				</dd>
			</div>
			<div class="receipt-row">
				<dt>Registered</dt>
				<dd class="mono">{date} · {time}</dd>
			</div>
			<div class="receipt-row">
				<dt>Session</dt>
				<dd class="session">
					<span class="session-dot" class:active={sessionActive}></span>
					{sessionActive ? 'Active' : 'Pending'}
				</dd>
			</div>
		</div>

		<div class="receipt-perf"></div>
		<div class="receipt-foot">
			<span>Receipt issued locally · no data leaves this server</span>
		</div>
	</dl>
</div>

<style>
	.success {
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
		gap: 1.35rem;
		padding: 0.5rem 0 0.25rem;
	}

	/* ---- Emblem ---- */
	.emblem {
		position: relative;
		width: 104px;
		height: 104px;
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.emblem-burst {
		position: absolute;
		inset: -22px;
		border-radius: 9999px;
		background:
			radial-gradient(circle at 50% 50%, hsl(152 76% 42% / 0.32) 0%, transparent 62%),
			conic-gradient(from 0deg, hsl(152 76% 45% / 0.18), transparent 25%, hsl(217 91% 60% / 0.16), transparent 60%, hsl(152 76% 45% / 0.18));
		filter: blur(6px);
		animation: burst 900ms cubic-bezier(0.33, 1, 0.68, 1) both;
	}

	@keyframes burst {
		from { opacity: 0; transform: scale(0.5); }
		to { opacity: 1; transform: scale(1); }
	}

	.emblem-ring {
		position: absolute;
		width: 104px;
		height: 104px;
		overflow: visible;
	}

	.ring-track {
		stroke: hsl(var(--border));
	}

	.ring-fill {
		stroke: hsl(152 71% 45%);
		stroke-dasharray: 100;
		stroke-dashoffset: 100;
		transform: rotate(-90deg);
		transform-origin: 60px 60px;
		animation: draw-ring 760ms cubic-bezier(0.65, 0, 0.35, 1) 120ms forwards;
		filter: drop-shadow(0 0 6px hsl(152 71% 50% / 0.45));
	}

	@keyframes draw-ring {
		to { stroke-dashoffset: 0; }
	}

	.ring-check {
		stroke: hsl(152 71% 42%);
		stroke-dasharray: 100;
		stroke-dashoffset: 100;
		animation: draw-check 360ms cubic-bezier(0.33, 1, 0.68, 1) 720ms forwards;
		filter: drop-shadow(0 1px 2px hsl(152 71% 45% / 0.4));
	}

	@keyframes draw-check {
		to { stroke-dashoffset: 0; }
	}

	.emblem-core {
		position: relative;
		width: 56px;
		height: 56px;
		border-radius: 9999px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: hsl(152 71% 96%);
		border: 1px solid hsl(152 71% 80%);
		box-shadow: inset 0 0 0 3px hsl(0 0% 100% / 0.6);
		animation: core-in 420ms cubic-bezier(0.33, 1, 0.68, 1) 560ms both;
	}

	@keyframes core-in {
		from { opacity: 0; transform: scale(0.4); }
		to { opacity: 1; transform: scale(1); }
	}

	:global(.emblem-icon) {
		width: 26px;
		height: 26px;
		color: hsl(152 71% 38%);
	}

	/* ---- Heading ---- */
	.head {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.35rem;
	}

	.kicker {
		font-family: ui-monospace, 'SF Mono', 'Cascadia Code', 'Menlo', monospace;
		font-size: 0.68rem;
		font-weight: 700;
		letter-spacing: 0.22em;
		text-transform: uppercase;
		color: hsl(152 71% 40%);
	}

	.title {
		font-family: ui-sans-serif, system-ui, -apple-system, 'Segoe UI', sans-serif;
		font-size: 1.7rem;
		font-weight: 700;
		letter-spacing: -0.025em;
		line-height: 1.12;
	}

	.sub {
		max-width: 30rem;
		font-size: 0.9rem;
		line-height: 1.55;
		color: hsl(var(--muted-foreground));
	}

	/* ---- Receipt ---- */
	.receipt {
		width: 100%;
		max-width: 26rem;
		margin-top: 0.4rem;
		background: hsl(var(--card));
		border: 1.5px dashed hsl(var(--border));
		border-radius: 14px;
		padding: 1.1rem 1.25rem 0;
		position: relative;
		box-shadow: 0 18px 40px -24px hsl(222 84% 4% / 0.3);
	}

	.receipt-top {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		padding-bottom: 0.85rem;
		border-bottom: 1px dashed hsl(var(--border));
	}

	.receipt-stamp {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		font-family: ui-monospace, 'SF Mono', 'Cascadia Code', 'Menlo', monospace;
		font-size: 0.66rem;
		font-weight: 700;
		letter-spacing: 0.1em;
		text-transform: uppercase;
		color: hsl(var(--muted-foreground));
	}

	.receipt-no {
		font-family: ui-monospace, 'SF Mono', 'Cascadia Code', 'Menlo', monospace;
		font-size: 0.62rem;
		color: hsl(var(--muted-foreground));
		opacity: 0.8;
	}

	.receipt-rows {
		display: flex;
		flex-direction: column;
		gap: 0.65rem;
		padding: 0.95rem 0 1.1rem;
	}

	.receipt-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
	}

	.receipt-row dt {
		font-size: 0.72rem;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: hsl(var(--muted-foreground));
	}

	.receipt-row dd {
		font-size: 0.88rem;
		font-weight: 600;
		color: hsl(var(--foreground));
	}

	.mono {
		font-family: ui-monospace, 'SF Mono', 'Cascadia Code', 'Menlo', monospace;
		font-weight: 700;
		letter-spacing: -0.01em;
	}

	.masked-bullets {
		display: inline-flex;
		gap: 0.4rem;
		align-items: center;
	}

	.dot {
		width: 7px;
		height: 7px;
		border-radius: 9999px;
		background: hsl(var(--foreground));
		opacity: 1;
		animation: dot-in 220ms cubic-bezier(0.33, 1, 0.68, 1) both;
	}

	@keyframes dot-in {
		from { opacity: 0; transform: translateY(3px); }
		to { opacity: 1; transform: translateY(0); }
	}

	.session {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		color: hsl(var(--muted-foreground));
		font-family: ui-monospace, 'SF Mono', 'Cascadia Code', 'Menlo', monospace;
		font-size: 0.78rem;
		font-weight: 700;
	}

	.session-dot {
		width: 8px;
		height: 8px;
		border-radius: 9999px;
		background: hsl(var(--muted-foreground));
		opacity: 0.5;
	}

	.session-dot.active {
		background: hsl(152 71% 45%);
		opacity: 1;
		box-shadow: 0 0 0 3px hsl(152 71% 45% / 0.18);
		animation: pulse 1.8s ease-in-out infinite;
	}

	@keyframes pulse {
		0%, 100% { box-shadow: 0 0 0 3px hsl(152 71% 45% / 0.18); }
		50% { box-shadow: 0 0 0 5px hsl(152 71% 45% / 0.08); }
	}

	.receipt-perf {
		height: 10px;
		margin: 0 -1.25rem;
		background:
			radial-gradient(circle at 5px -2px, transparent 5px, hsl(var(--card)) 5.5px) repeat-x,
			hsl(var(--card));
		background-size: 10px 10px;
		mask-image: linear-gradient(to bottom, black 60%, transparent 100%);
	}

	.receipt-foot {
		padding: 0.6rem 0 0.9rem;
		font-family: ui-monospace, 'SF Mono', 'Cascadia Code', 'Menlo', monospace;
		font-size: 0.6rem;
		letter-spacing: 0.05em;
		color: hsl(var(--muted-foreground));
		opacity: 0.75;
		text-align: center;
	}

</style>
