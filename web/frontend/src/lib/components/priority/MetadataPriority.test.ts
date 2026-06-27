import { describe, it, expect } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import MetadataPriority from './MetadataPriority.svelte';
import type { SettingsConfig } from '$lib/api/types';

// jsdom lacks the Web Animations API, but Svelte's `transition:fade` (used by
// the popover) calls element.animate() during intro. Stub it for the test
// environment only so the click-open path runs under vitest. Scoped to this
// file (vitest isolates each test file in its own jsdom realm).
if (!Element.prototype.animate) {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	(Element.prototype as any).animate = function () {
		const anim = {
			onfinish: null as (() => void) | null,
			oncancel: null as (() => void) | null,
			effect: null as unknown,
			playState: 'finished' as const,
			currentTime: 0,
			cancel() {},
			finish() {
				anim.onfinish?.();
			},
			addEventListener() {},
			removeEventListener() {},
		};
		// Defer so Svelte can assign onfinish after animate() returns.
		queueMicrotask(() => anim.onfinish?.());
		return anim;
	};
}

// Minimal config — MetadataPriority only reads `scrapers.priority`, the
// per-scraper `enabled` flag, and `metadata.priority` during render. Cast to
// satisfy the fully-required SettingsConfig type, mirroring the factory in
// priority.test.ts.
function makeConfig(global: string[] = ['r18dev', 'dmm']): SettingsConfig {
	return {
		scrapers: {
			priority: global,
			r18dev: { enabled: true },
			dmm: { enabled: true },
		},
		metadata: {},
	} as unknown as SettingsConfig;
}

describe('MetadataPriority — header info icon', () => {
	it('renders a focusable help trigger wired to a tooltip', () => {
		const { container } = render(MetadataPriority, {
			props: { config: makeConfig(), onUpdate: () => {} },
		});

		const button = container.querySelector('button[aria-label="Priority mode help"]');
		expect(button).toBeTruthy();
		// aria-describedby points at the always-present tooltip node…
		expect(button?.getAttribute('aria-describedby')).toBe('priority-mode-help-tooltip');
		expect(button?.getAttribute('aria-expanded')).toBe('false');

		const tooltip = container.querySelector('[role="tooltip"]#priority-mode-help-tooltip');
		expect(tooltip).toBeTruthy();

		// …so its content is hidden until opened.
		expect(container.textContent).not.toContain('Metadata priority modes');
	});

	it('toggles the help popover open on click', async () => {
		const { container } = render(MetadataPriority, {
			props: { config: makeConfig(), onUpdate: () => {} },
		});

		const button = container.querySelector(
			'button[aria-label="Priority mode help"]',
		) as HTMLButtonElement;
		expect(button.getAttribute('aria-expanded')).toBe('false');

		await fireEvent.click(button);

		await waitFor(() => {
			expect(button.getAttribute('aria-expanded')).toBe('true');
			expect(container.textContent).toContain('Metadata priority modes');
			// Exclusive-semantics note is shown.
			expect(container.textContent).toContain('no fallback to the global list');
		});
	});
});
