import { describe, expect, it, vi } from 'vitest';
import { focusCreditGuidance } from './credit-guidance';

describe('credit guidance focus', () => {
	it('waits for Detail before focusing and scrolling the accessible region', async () => {
		const region = document.createElement('section');
		region.tabIndex = -1;
		region.setAttribute('aria-label', 'Actress-credit conflicts');
		document.body.append(region);
		region.scrollIntoView = vi.fn();
		let mounted = false;
		await focusCreditGuidance(
			async () => {
				mounted = true;
			},
			() => true,
			() => (mounted ? region : undefined),
		);
		expect(document.activeElement).toBe(region);
		expect(region.scrollIntoView).toHaveBeenCalledWith({ block: 'center', behavior: 'smooth' });
		region.remove();
	});

	it('does not steal focus after another route, movie, tab or view is selected', async () => {
		const getRegion = vi.fn();
		await focusCreditGuidance(
			async () => {},
			() => false,
			getRegion,
		);
		expect(getRegion).not.toHaveBeenCalled();
	});

	it('tolerates a removed target', async () => {
		await expect(
			focusCreditGuidance(
				async () => {},
				() => true,
				() => undefined,
			),
		).resolves.toBeUndefined();
	});
});
