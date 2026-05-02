<script lang="ts">
	import type { CompletenessTier } from '$lib/utils/completeness';

	interface Props {
		score: number;
		tier: CompletenessTier;
		size?: number;
		'aria-describedby'?: string;
	}

	let { score, tier, size = 34, 'aria-describedby': ariaDescribedby }: Props = $props();

	const circumference = 2 * Math.PI * 14;

	const tierColor = $derived(
		tier === 'incomplete'
			? 'rgb(239 68 68)'
			: tier === 'partial'
				? 'rgb(234 179 8)'
				: 'rgb(34 197 94)'
	);

	const dashOffset = $derived(circumference * (1 - score / 100));
</script>

<div class="flex-shrink-0" style="width: {size}px; height: {size}px;" role="img" aria-label={`${score}% complete`} aria-describedby={ariaDescribedby}>
	<svg viewBox="0 0 36 36" width={size} height={size}>
		<circle
			cx="18"
			cy="18"
			r="14"
			fill="none"
			stroke="rgb(107 114 128)"
			stroke-width="3"
		/>
		<circle
			cx="18"
			cy="18"
			r="14"
			fill="none"
			stroke={tierColor}
			stroke-width="3"
			stroke-dasharray={circumference}
			stroke-dashoffset={dashOffset}
			stroke-linecap="round"
			transform="rotate(-90 18 18)"
		/>
		<text
			x="18"
			y="18"
			text-anchor="middle"
			dominant-baseline="central"
			fill="white"
			font-size="8"
			font-weight="600"
		>
			{score}%
		</text>
	</svg>
</div>
