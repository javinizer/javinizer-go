import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { tick } from 'svelte';
import Panel from './ActressNameSyncPanel.svelte';
import Evidence from './ActressNameSyncEvidence.svelte';
import { createActressNameSyncController } from '../stores/actress-name-sync.svelte';
import { ActressNameSyncClient } from '$lib/api/clients/actress-name-sync';
import type { ActressNameSyncJob, ActressNameSyncSource } from '$lib/api/types/actress-name-sync';
const source: ActressNameSyncSource = {
	source_key: 'r18:1',
	source_id: '1',
	japanese_name: '新名',
	aliases: ['Alias'],
	source_url: 'https://example.test/person',
	observed_at: '2026-10-25T00:00:00Z',
};
const actor = { id: 1, first_name: 'Actor', japanese_name: '旧名' };
const job: ActressNameSyncJob = {
	id: 'job-a',
	snapshot_digest: 'digest',
	created_at: '2026-10-25',
	status: 'completed',
	cancelled: false,
	counts: { applied: 1 },
};
const controllers: ReturnType<typeof createActressNameSyncController>[] = [];
let closeSpy: ReturnType<typeof vi.fn>;
beforeEach(() => {
	sessionStorage.clear();
	Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
		configurable: true,
		value: function (this: HTMLDialogElement) {
			this.open = true;
		},
	});
	closeSpy = vi.fn(function (this: HTMLDialogElement) {
		this.open = false;
	});
	Object.defineProperty(HTMLDialogElement.prototype, 'close', {
		configurable: true,
		value: closeSpy,
	});
});
afterEach(() => {
	cleanup();
	controllers.splice(0).forEach((c) => c.close());
	vi.restoreAllMocks();
	sessionStorage.clear();
	document.body.innerHTML = '';
});
function setup(info = { enabled: true, available: true }) {
	const client = new ActressNameSyncClient('http://unused.test');
	const api = {
		status: vi.spyOn(client, 'status').mockResolvedValue({
			...info,
			record_count: 1,
			recovered: false,
			max_age_seconds: 3600,
			restart_required_for_config_changes: true,
		}),
		sources: vi
			.spyOn(client, 'sources')
			.mockResolvedValue({ items: [source], next_offset: null, artifact_digest: 'digest' }),
		review: vi.spyOn(client, 'review').mockResolvedValue({
			review_id: 'a'.repeat(64),
			expires_at: '2030-01-01T00:00:00Z',
			artifact_digest: 'digest',
			max_age_seconds: 3600,
			proposals: [{ actress_id: 1, source, before_name: '旧名', after_name: '新名' }],
		}),
		confirm: vi.spyOn(client, 'confirm').mockResolvedValue(job),
		jobs: vi.spyOn(client, 'jobs').mockResolvedValue({ items: [job] }),
		job: vi.spyOn(client, 'job').mockResolvedValue(job),
		tasks: vi.spyOn(client, 'tasks').mockResolvedValue({ items: [], next_offset: null }),
		cancel: vi
			.spyOn(client, 'cancel')
			.mockResolvedValue({ ...job, status: 'cancelled', cancelled: true }),
	};
	const c = createActressNameSyncController({ client });
	controllers.push(c);
	const ui = render(Panel, { controller: c });
	return { c, api, ...ui };
}
async function choices(h: ReturnType<typeof setup>) {
	h.c.open([actor]);
	await waitFor(() => expect(h.api.status).toHaveBeenCalled());
	await tick();
	await h.c.searchSources();
	h.c.chooseSource(source);
	await tick();
}
describe('explicit offline name review', () => {
	it('wraps Tab both ways among visible enabled controls without issuing writes', async () => {
		const h = setup();
		await choices(h);
		const dialog = h.container.querySelector('dialog')!;
		vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(function (
			this: HTMLElement,
		) {
			return (this.hidden ? [] : [{}]) as unknown as DOMRectList;
		});
		const controls = Array.from(
			dialog.querySelectorAll<HTMLElement>('a[href], button, input, select, textarea, [tabindex]'),
		).filter((el) => el.tabIndex >= 0 && !el.matches(':disabled'));
		const first = controls[0];
		const last = controls[controls.length - 1];
		const disabled = document.createElement('button');
		disabled.disabled = true;
		dialog.append(disabled);
		const hidden = document.createElement('button');
		hidden.hidden = true;
		dialog.append(hidden);
		last.focus();
		await fireEvent.keyDown(last, { key: 'Tab' });
		expect(document.activeElement).toBe(first);
		first.focus();
		await fireEvent.keyDown(first, { key: 'Tab', shiftKey: true });
		expect(document.activeElement).toBe(last);
		dialog.querySelector<HTMLElement>('[data-sync-title]')!.focus();
		await fireEvent.keyDown(dialog, { key: 'Tab' });
		expect(document.activeElement).toBe(first);
		expect(h.api.review).not.toHaveBeenCalled();
		expect(h.api.confirm).not.toHaveBeenCalled();
		expect(h.api.cancel).not.toHaveBeenCalled();
	});
	it('does not close an unopened dialog or issue writes on unmount', () => {
		const h = setup();
		h.unmount();
		expect(closeSpy).not.toHaveBeenCalled();
		expect(h.api.review).not.toHaveBeenCalled();
		expect(h.api.confirm).not.toHaveBeenCalled();
		expect(h.api.cancel).not.toHaveBeenCalled();
	});
	it('requires a selected source, full review and separate consent before confirmation', async () => {
		const h = setup();
		h.c.open([actor]);
		await tick();
		expect(
			(h.getByRole('button', { name: 'Review proposed changes' }) as HTMLButtonElement).disabled,
		).toBe(true);
		await choices(h);
		await fireEvent.click(h.getByRole('button', { name: 'Review proposed changes' }));
		await waitFor(() =>
			expect(h.getByRole('heading', { name: 'Review exact changes' })).toBeTruthy(),
		);
		expect(h.getByText('旧名')).toBeTruthy();
		expect(
			(h.getByRole('button', { name: 'Confirm one-shot sync' }) as HTMLButtonElement).disabled,
		).toBe(true);
		expect(h.api.confirm).not.toHaveBeenCalled();
		await fireEvent.click(h.getByRole('checkbox'));
		await fireEvent.click(h.getByRole('button', { name: 'Confirm one-shot sync' }));
		await waitFor(() => expect(h.api.confirm).toHaveBeenCalledTimes(1));
		expect(h.api.confirm.mock.calls[0][0]).toBe('a'.repeat(64));
	});
	it('Back discards consent and review without writes', async () => {
		const h = setup();
		await choices(h);
		await h.c.requestReview();
		await tick();
		await fireEvent.click(h.getByRole('checkbox'));
		await fireEvent.click(h.getByRole('button', { name: 'Back to source choices' }));
		expect(h.c.review).toBeNull();
		expect(h.c.consent).toBe(false);
		expect(h.api.confirm).not.toHaveBeenCalled();
		expect(h.api.cancel).not.toHaveBeenCalled();
	});
	it.each(['close', 'escape'])(
		'%s dismisses without writes and restores opener focus',
		async (action) => {
			const opener = document.createElement('button');
			document.body.append(opener);
			opener.focus();
			const h = setup();
			await choices(h);
			await h.c.requestReview();
			await tick();
			if (action === 'close') await fireEvent.click(h.getByRole('button', { name: 'Close' }));
			else await fireEvent(h.getByRole('dialog'), new Event('cancel', { cancelable: true }));
			await tick();
			expect(h.c.isOpen).toBe(false);
			expect(document.activeElement).toBe(opener);
			expect(h.api.confirm).not.toHaveBeenCalled();
			expect(h.api.cancel).not.toHaveBeenCalled();
		},
	);
	it.each([
		{ enabled: false, available: false },
		{ enabled: true, available: false },
	])('keeps history readable when unavailable %j', async (info) => {
		const h = setup(info);
		h.c.open();
		await waitFor(() =>
			expect(h.getByRole('heading', { name: 'Durable sync history' })).toBeTruthy(),
		);
		await waitFor(() => expect(h.getByRole('button', { name: 'View results' })).toBeTruthy());
		await fireEvent.click(h.getByRole('button', { name: 'View results' }));
		await waitFor(() => expect(h.getByText('Completed')).toBeTruthy());
		expect(h.api.confirm).not.toHaveBeenCalled();
		expect(h.getByRole('link', { name: 'Read offline provisioning instructions' })).toBeTruthy();
	});
	it('restored submitted reference never automatically resubmits', async () => {
		sessionStorage.setItem('javinizer.offline-name-sync.submitted-review', 'a'.repeat(64));
		const h = setup();
		h.c.open();
		await waitFor(() =>
			expect(h.getByRole('button', { name: 'Retry same confirmation' })).toBeTruthy(),
		);
		expect(h.api.confirm).not.toHaveBeenCalled();
		await fireEvent.click(h.getByRole('button', { name: 'Forget recovery reference' }));
		expect(h.c.recoveryReviewId).toBe('a'.repeat(64));
		await fireEvent.click(h.getByRole('button', { name: 'Keep recovery reference' }));
		expect(h.c.recoveryReviewId).toBe('a'.repeat(64));
		expect(h.api.cancel).not.toHaveBeenCalled();
	});
	it('requires confirmation to cancel remaining work', async () => {
		const h = setup();
		h.api.job.mockResolvedValue({ ...job, status: 'running' });
		h.c.open();
		await h.c.openJob(job.id);
		await tick();
		await fireEvent.click(h.getByRole('button', { name: 'Cancel remaining work' }));
		expect(h.api.cancel).not.toHaveBeenCalled();
		await fireEvent.click(h.getByRole('button', { name: 'Keep job running' }));
		expect(h.api.cancel).not.toHaveBeenCalled();
		await fireEvent.click(h.getByRole('button', { name: 'Cancel remaining work' }));
		await fireEvent.click(h.getByRole('button', { name: 'Confirm cancellation' }));
		await waitFor(() => expect(h.api.cancel).toHaveBeenCalledTimes(1));
	});
});
it.each(['javascript:alert(1)', 'data:text/html,bad', 'https://user:secret@example.test/'])(
	'renders unsafe provenance only as text: %s',
	(url) => {
		const h = render(Evidence, { source: { ...source, source_url: url } });
		expect(h.queryAllByRole('link')).toHaveLength(0);
		expect(h.getByText(url)).toBeTruthy();
	},
);
