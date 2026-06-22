import { describe, it, expect } from 'vitest';
import { computeCropPreview } from './review-utils';
import type { PosterCropBox } from './review-utils';

function box(width: number, height: number): PosterCropBox {
	return { x: 0, y: 0, width, height };
}

describe('computeCropPreview', () => {
	it('returns empty output when crop box is null', () => {
		expect(computeCropPreview(null, 0)).toEqual({
			outputWidth: 0,
			outputHeight: 0,
			ratioLabel: '',
			willResize: false,
		});
	});

	it('preserves source resolution when max height is 0 (no cap)', () => {
		// Issue #33 regression: high-res crop must not be downscaled to 500
		const result = computeCropPreview(box(1032, 1468), 0);
		expect(result.outputWidth).toBe(1032);
		expect(result.outputHeight).toBe(1468);
		expect(result.willResize).toBe(false);
		// gcd(1032,1468)=4 → 258:367 exceeds 20, so decimal form is used.
		expect(result.ratioLabel).toBe('0.703:1');
	});

	it('simplifies 2:3 aspect ratio label', () => {
		// 800x1200 → gcd=400, 2:3
		const result = computeCropPreview(box(800, 1200), 0);
		expect(result.outputWidth).toBe(800);
		expect(result.outputHeight).toBe(1200);
		expect(result.willResize).toBe(false);
		expect(result.ratioLabel).toBe('2:3');
	});

	it('downscales preserving aspect ratio when source exceeds cap', () => {
		// 1032x1468 cap at 500 → 351x500
		const result = computeCropPreview(box(1032, 1468), 500);
		expect(result.outputWidth).toBe(351);
		expect(result.outputHeight).toBe(500);
		expect(result.willResize).toBe(true);
	});

	it('does not downscale when source equals the cap', () => {
		const result = computeCropPreview(box(800, 500), 500);
		expect(result.outputWidth).toBe(800);
		expect(result.outputHeight).toBe(500);
		expect(result.willResize).toBe(false);
	});

	it('does not downscale when source is below the cap', () => {
		const result = computeCropPreview(box(400, 600), 1000);
		expect(result.outputWidth).toBe(400);
		expect(result.outputHeight).toBe(600);
		expect(result.willResize).toBe(false);
	});

	it('uses decimal ratio when integers do not simplify small', () => {
		// 472x600 → gcd=8, 59:75 → both > 20? 59 > 20 → decimals
		const result = computeCropPreview(box(472, 600), 0);
		expect(result.outputWidth).toBe(472);
		expect(result.outputHeight).toBe(600);
		expect(result.willResize).toBe(false);
		// 472/600 = 0.787 → '0.787:1'
		expect(result.ratioLabel).toBe('0.787:1');
	});

	it('caps at 1 produce minimal output', () => {
		// 200x300 cap 1 → 1x1 (rounded)
		const result = computeCropPreview(box(200, 300), 1);
		expect(result.outputHeight).toBe(1);
		expect(result.outputWidth).toBe(1); // Math.round(200/300) = 1
		expect(result.willResize).toBe(true);
	});
});
