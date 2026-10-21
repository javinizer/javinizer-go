import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import CollisionPanel from './CollisionPanel.svelte';
import { apiClient } from '$lib/api/client';

vi.mock('$lib/api/client', () => ({
	apiClient: { listCollisions: vi.fn(), resolveCollision: vi.fn(), searchActresses: vi.fn() },
}));
const clients: QueryClient[] = [];
beforeEach(() => vi.resetAllMocks());
afterEach(() => {
	cleanup();
	clients.splice(0).forEach((client) => client.clear());
});
function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
	clients.push(client);
	return render(
		CollisionPanel,
		{ movieContentId: 'movie-1', showEmpty: true },
		{
			wrapper: QueryClientWrapper,
			wrapperProps: { client },
		},
	);
}
it('shows no-open-conflicts guidance only after a successful current lookup', async () => {
	let resolve!: (value: { collisions: [] }) => void;
	vi.mocked(apiClient.listCollisions).mockReturnValue(
		new Promise((done) => {
			resolve = done;
		}),
	);
	const view = mount();
	await waitFor(() => expect(apiClient.listCollisions).toHaveBeenCalledWith('movie-1'));
	expect(view.queryByRole('status')).toBeNull();
	resolve({ collisions: [] });
	await waitFor(() => expect(view.getByRole('status').textContent).toMatch(/no open/i));
	expect(apiClient.resolveCollision).not.toHaveBeenCalled();
});
it('does not claim conflicts are cleared when the status lookup fails', async () => {
	vi.mocked(apiClient.listCollisions).mockRejectedValue(new Error('Collision status unavailable'));
	const view = mount();
	await view.findByText('Collision status unavailable');
	expect(view.queryByRole('status')).toBeNull();
	expect(view.getByRole('button', { name: 'Retry' })).toBeTruthy();
});
