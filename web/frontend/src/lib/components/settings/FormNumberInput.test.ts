import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/svelte';
import FormNumberInput from './FormNumberInput.svelte';

afterEach(cleanup);

function mount(value = 60, bounds: { min?: number; max?: number; step?: number } = {}) {
	const onchange = vi.fn();
	const view = render(FormNumberInput, { props: { label: 'Timeout', value, onchange, ...bounds } });
	return { onchange, input: view.getByLabelText('Timeout') as HTMLInputElement };
}

describe('FormNumberInput integer callback parsing', () => {
	it.each([
		['1e2', 100],
		['2E+2', 200],
		['1.25e2', 125],
		['1.29e1', 12],
		['-1.29e1', -12],
		['1e-2', 0],
		['-1e-2', -0],
		['.5e2', 50],
		['1e21', 1e21],
	])('expands %s before integer truncation', async (text, expected) => {
		const { input, onchange } = mount();
		await fireEvent.input(input, { target: { value: text } });
		expect(onchange).toHaveBeenCalledExactlyOnceWith(expected);
	});

	it.each([
		['42', 42],
		['0', 0],
		['-42', -42],
		['12.9', 12],
		['-12.9', -12],
		['0.5', 0],
		['-0.5', -0],
	])('preserves ordinary parsing for %s', async (text, expected) => {
		const { input, onchange } = mount();
		await fireEvent.input(input, { target: { value: text } });
		expect(onchange).toHaveBeenCalledExactlyOnceWith(expected);
	});

	it.each(['.5', '-.5'])('preserves non-emitting leading-dot decimal %s', async (text) => {
		const { input, onchange } = mount();
		await fireEvent.input(input, { target: { value: text } });
		expect(input.value).toBe(text);
		expect(onchange).not.toHaveBeenCalled();
	});

	it('clears without emitting zero or NaN and accepts the next value', async () => {
		const { input, onchange } = mount();
		await fireEvent.input(input, { target: { value: '' } });
		expect(input.value).toBe('');
		expect(onchange).not.toHaveBeenCalled();
		await fireEvent.input(input, { target: { value: '23' } });
		expect(onchange).toHaveBeenCalledExactlyOnceWith(23);
	});

	it.each(['abc', 'NaN', 'Infinity', '1e', '--1', '1e309'])(
		'does not emit invalid input %s',
		async (text) => {
			const { input, onchange } = mount();
			await fireEvent.input(input, { target: { value: text } });
			// jsdom sanitizes invalid number-input strings; this does not simulate real typing.
			expect(input.value).toBe('');
			expect(onchange).not.toHaveBeenCalled();
		},
	);

	it.each([
		['5', 5, false, false],
		['300', 300, false, false],
		['4', 4, true, false],
		['301', 301, false, true],
		['1e0', 1, true, false],
		['4e2', 400, false, true],
	])(
		'keeps bounds as validity hints without clamping %s',
		async (text, expected, underflow, overflow) => {
			const { input, onchange } = mount(60, { min: 5, max: 300 });
			await fireEvent.input(input, { target: { value: text } });
			expect(input.min).toBe('5');
			expect(input.max).toBe('300');
			expect(input.validity.rangeUnderflow).toBe(underflow);
			expect(input.validity.rangeOverflow).toBe(overflow);
			expect(onchange).toHaveBeenCalledExactlyOnceWith(expected);
		},
	);

	it('retains default step and caller-provided value without emitting on mount', () => {
		const { input, onchange } = mount(0);
		expect(input.value).toBe('0');
		expect(input.step).toBe('1');
		expect(input.min).toBe('');
		expect(input.max).toBe('');
		expect(onchange).not.toHaveBeenCalled();
	});

	it('preserves a custom step without changing integer callbacks', async () => {
		const { input, onchange } = mount(60, { step: 0.5 });
		expect(input.step).toBe('0.5');
		await fireEvent.input(input, { target: { value: '12.5' } });
		expect(onchange).toHaveBeenCalledExactlyOnceWith(12);
	});
});
