import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
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
function mount(cachedCollisions?: unknown[]) {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
	clients.push(client);
	if (cachedCollisions)
		client.setQueryData(['collisions', 'movie-1'], { collisions: cachedCollisions });
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

it('hides cached conflict actions after a forced fresh lookup fails and restores only fresh results', async () => {
	const cached = {
		id: 1,
		credit_id: 1,
		movie_content_id: 'movie-1',
		field: 'credited_name',
		reported_value: 'Cached reported name',
		canonical_value: 'Cached catalog name',
		status: 'open',
		occurrences: 1,
		allowed_resolutions: ['keep_identity', 'reassign'],
	};
	let reject!: (error: Error) => void;
	vi.mocked(apiClient.listCollisions).mockReturnValue(
		new Promise((_resolve, fail) => {
			reject = fail;
		}),
	);
	const view = mount([cached]);
	await waitFor(() => expect(apiClient.listCollisions).toHaveBeenCalledWith('movie-1'));
	expect(view.queryByText('Cached reported name')).toBeNull();
	expect(view.queryByRole('button', { name: /Keep catalog|Relink/ })).toBeNull();
	expect(view.queryByRole('status')).toBeNull();
	reject(new Error('Latest status check failed'));
	await view.findByText('Latest status check failed');
	// TanStack retains the data on refetch failure; the UI must not offer it as current.
	expect(clients[0].getQueryData(['collisions', 'movie-1'])).toEqual({ collisions: [cached] });
	expect(view.queryByText('Cached reported name')).toBeNull();
	expect(view.queryByRole('button', { name: /Keep catalog|Relink/ })).toBeNull();
	expect(view.queryByRole('status')).toBeNull();
	expect(apiClient.resolveCollision).not.toHaveBeenCalled();
	vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [] });
	await fireEvent.click(view.getByRole('button', { name: 'Retry' }));
	await waitFor(() => expect(view.getByRole('status').textContent).toMatch(/no open/i));
	expect(view.queryByText('Latest status check failed')).toBeNull();
});
it('only newly fetched rows become actionable after cached-row failure and retry', async () => {
	const stale = {
		id: 1,
		credit_id: 1,
		movie_content_id: 'movie-1',
		field: 'credited_name',
		reported_value: 'Stale reported',
		canonical_value: 'Stale catalog',
		status: 'open',
		occurrences: 1,
		allowed_resolutions: ['keep_identity'],
	};
	let reject!: (error: Error) => void;
	vi.mocked(apiClient.listCollisions).mockReturnValueOnce(
		new Promise((_, no) => {
			reject = no;
		}),
	);
	const view = mount([stale]);
	await waitFor(() => expect(apiClient.listCollisions).toHaveBeenCalledOnce());
	expect(view.queryByText('Stale reported')).toBeNull();
	expect(view.queryByRole('button', { name: 'Keep catalog' })).toBeNull();
	reject(new Error('Independent lookup error'));
	await view.findByText('Independent lookup error');
	expect(view.queryByRole('status')).toBeNull();
	const fresh = { ...stale, id: 2, credit_id: 2, reported_value: 'Fresh reported' };
	let resolve!: (value: any) => void;
	vi.mocked(apiClient.listCollisions).mockReturnValueOnce(
		new Promise((done) => {
			resolve = done;
		}),
	);
	await fireEvent.click(view.getByRole('button', { name: 'Retry' }));
	expect(view.queryByRole('button', { name: 'Keep catalog' })).toBeNull();
	resolve({ collisions: [fresh] });
	await view.findByText('Fresh reported');
	expect(view.queryByText('Stale reported')).toBeNull();
	vi.mocked(apiClient.resolveCollision).mockReturnValue(new Promise(() => {}));
	await fireEvent.click(view.getByRole('button', { name: 'Keep catalog' }));
	await waitFor(() =>
		expect(apiClient.resolveCollision).toHaveBeenCalledWith(2, {
			resolution: 'keep_identity',
			target_actress_id: undefined,
		}),
	);
});

it('stale retained resolution control cannot submit after lookup invalidation', async () => {
	const row = {
		id: 1,
		credit_id: 1,
		movie_content_id: 'movie-1',
		field: 'credited_name',
		reported_value: 'Was current',
		canonical_value: 'Catalog',
		status: 'open',
		occurrences: 1,
		allowed_resolutions: ['keep_identity'],
	};
	vi.mocked(apiClient.listCollisions).mockResolvedValueOnce({ collisions: [row] } as never);
	const view = mount();
	const staleButton = await view.findByRole('button', { name: 'Keep catalog' });
	vi.mocked(apiClient.listCollisions).mockRejectedValue(new Error('Now unavailable'));
	await clients[0].refetchQueries({ queryKey: ['collisions', 'movie-1'] });
	await view.findByText('Now unavailable');
	vi.mocked(apiClient.resolveCollision).mockReturnValue(new Promise(() => {}));
	await fireEvent.click(staleButton);
	expect(apiClient.resolveCollision).not.toHaveBeenCalled();
});

it('background failed refresh discards relink draft before retry restores same collision', async () => {
	const row = {
		id: 1,
		credit_id: 1,
		movie_content_id: 'movie-1',
		field: 'identity_link',
		reported_value: 'Reported',
		canonical_value: 'Catalog',
		current_actress_id: 7,
		status: 'open',
		occurrences: 1,
		allowed_resolutions: ['reassign'],
	};
	vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [row] } as never);
	vi.mocked(apiClient.searchActresses).mockResolvedValue([
		{ id: 8, first_name: 'Verified', last_name: 'Target', verified: true },
	] as never);
	const view = mount();
	await fireEvent.click(await view.findByRole('button', { name: 'Relink' }));
	await fireEvent.click(await view.findByRole('button', { name: /Target.*#8/ }));
	expect((view.getByRole('button', { name: 'Confirm relink' }) as HTMLButtonElement).disabled).toBe(
		false,
	);
	vi.mocked(apiClient.listCollisions).mockRejectedValue(new Error('Background error'));
	await clients[0].refetchQueries({ queryKey: ['collisions', 'movie-1'] });
	await view.findByText('Background error');
	expect(view.queryByRole('dialog')).toBeNull();
	vi.mocked(apiClient.listCollisions).mockResolvedValue({ collisions: [row] } as never);
	await fireEvent.click(view.getByRole('button', { name: 'Retry' }));
	await view.findByRole('button', { name: 'Relink' });
	expect(view.queryByRole('dialog')).toBeNull();
	await fireEvent.click(view.getByRole('button', { name: 'Relink' }));
	expect((view.getByRole('button', { name: 'Confirm relink' }) as HTMLButtonElement).disabled).toBe(
		true,
	);
	expect(apiClient.resolveCollision).not.toHaveBeenCalled();
});
