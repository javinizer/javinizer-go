import { cleanup, fireEvent, render } from '@testing-library/svelte';
import { afterEach, expect, it } from 'vitest';
import Option from './OfflineEvidenceOption.svelte';
afterEach(cleanup);
it('defaults off and permits only explicit selection', async () => {
	const view = render(Option);
	const input = view.getByRole('checkbox', { name: /Show offline R18/ }) as HTMLInputElement;
	expect(input.checked).toBe(false);
	await fireEvent.click(input);
	expect(input.checked).toBe(true);
});
it('a fresh mount never restores a prior choice', async () => {
	const first = render(Option);
	await fireEvent.click(first.getByRole('checkbox'));
	first.unmount();
	expect((render(Option).getByRole('checkbox') as HTMLInputElement).checked).toBe(false);
});
it('can be disabled while submitting', () => {
	expect(
		(render(Option, { disabled: true }).getByRole('checkbox') as HTMLInputElement).disabled,
	).toBe(true);
});
