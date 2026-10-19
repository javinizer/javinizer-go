import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { tick } from 'svelte';
import R18DevDumpSection from './R18DevDumpSection.svelte';
import type { DumpStatus } from '$lib/api/types';

type Frame = { job_id: string; message: string; status: string; progress: number; error?: string };
const api = vi.hoisted(() => ({
  r18dev: { getDumpStatus: vi.fn(), uploadDump: vi.fn(), downloadDump: vi.fn(), clearDump: vi.fn() },
  config: { getConfig: vi.fn() },
  request: vi.fn(),
}));
const ws = vi.hoisted(() => ({ messages: [] as Frame[], subscribers: new Set<(s: { messages: Frame[] }) => void>() }));
vi.mock('$lib/api/client', () => ({ apiClient: api }));
vi.mock('$lib/stores/websocket', () => ({ websocketStore: {
  subscribe: (fn: (s: { messages: Frame[] }) => void) => {
    ws.subscribers.add(fn); fn({ messages: ws.messages }); return () => ws.subscribers.delete(fn);
  },
  clearMessages: () => { ws.messages=[]; for (const fn of ws.subscribers) fn({messages:[]}); },
} }));

if (!window.matchMedia) window.matchMedia = (query: string) => ({ matches:false, media:query, onchange:null, addEventListener(){}, removeEventListener(){}, addListener(){}, removeListener(){}, dispatchEvent:()=>false });
if (!Element.prototype.animate) Element.prototype.animate = function () {
  const animation = { onfinish:null as (()=>void)|null, oncancel:null, effect:null, playState:'finished', currentTime:0, cancel(){}, finish(){animation.onfinish?.();}, addEventListener(){}, removeEventListener(){} };
  queueMicrotask(()=>animation.onfinish?.()); return animation as unknown as Animation;
};
const status = (values: Partial<DumpStatus> = {}): DumpStatus => ({ present:true, enabled:true, running:false, path:'/synthetic/dump.db', ...values });
const oldError = 'independent obsolete saved failure';
const currentError = 'independent current configuration rejected';
function deferred<T>() { let resolve!: (v:T)=>void; const promise = new Promise<T>(r=>{resolve=r;}); return {promise,resolve}; }
async function settle() { await act(async()=>{ await Promise.resolve(); await tick(); await Promise.resolve(); }); }
async function mount() { const view=render(R18DevDumpSection); await settle(); await fireEvent.click(document.querySelector('button[aria-expanded]') as HTMLElement); await settle(); return view; }
async function poll() { await act(async()=>{await vi.advanceTimersByTimeAsync(3000);}); }

describe('independent completed-status precedence',()=>{
  beforeEach(()=>{
    vi.useFakeTimers(); vi.resetAllMocks(); ws.messages=[]; ws.subscribers.clear();
    api.r18dev.getDumpStatus.mockResolvedValue(status()); api.config.getConfig.mockResolvedValue({metadata:{}}); api.request.mockResolvedValue({});
    api.r18dev.uploadDump.mockResolvedValue({message:'staged'}); api.r18dev.clearDump.mockResolvedValue(undefined);
  });
  afterEach(()=>{cleanup();vi.clearAllTimers();vi.useRealTimers();});

  it.each(['mount','refresh'])('a delayed %s response cannot replace a newer config failure',async(mode)=>{
    const pending=deferred<DumpStatus>();
    if(mode==='mount') api.r18dev.getDumpStatus.mockReturnValueOnce(pending.promise);
    await mount();
    if(mode==='refresh') {
      api.r18dev.getDumpStatus.mockReturnValueOnce(pending.promise);
      await fireEvent.click(screen.getByRole('button',{name:/^Refresh$/})); await settle();
    }
    // This switch is outside the loading branch and remains visibly enabled.
    const toggle=screen.getByRole('switch'); expect(toggle.hasAttribute('disabled')).toBe(false);
    api.request.mockRejectedValueOnce(new Error(currentError));
    await fireEvent.click(toggle); await settle();
    expect(api.request).toHaveBeenCalledTimes(1);
    pending.resolve(status({last_error:oldError,last_error_kind:'validation'})); await settle();
    expect(screen.getByText(currentError)).toBeTruthy();
    expect(screen.queryByText(new RegExp(oldError))).toBeNull();
  });

  it('an older mount response cannot resurrect an error after a newer config status succeeds',async()=>{
    const pending=deferred<DumpStatus>();
    api.r18dev.getDumpStatus.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(status());
    await mount();
    await fireEvent.click(screen.getByRole('switch')); await settle();
    expect(api.request).toHaveBeenCalledTimes(1); expect(api.r18dev.getDumpStatus).toHaveBeenCalledTimes(2);
    expect(screen.queryByText(new RegExp(oldError))).toBeNull();
    pending.resolve(status({last_error:oldError,last_error_kind:'validation'})); await settle();
    expect(screen.queryByText(new RegExp(oldError))).toBeNull();
  });

  it('a current config failure remains visible when no older status request is pending',async()=>{
    await mount(); api.request.mockRejectedValueOnce(new Error(currentError));
    await fireEvent.click(screen.getByRole('switch')); await settle();
    expect(screen.getByText(currentError)).toBeTruthy();
  });

  it('current polled failure wins over presence and an old saved error',async()=>{
    api.r18dev.getDumpStatus.mockResolvedValueOnce(status({running:true,last_error:oldError})).mockResolvedValue(status({present:true,last_error:'independent new reload failure',last_error_kind:'reload'}));
    await mount(); expect(screen.queryByText(new RegExp(oldError))).toBeNull(); await poll();
    expect(screen.getByText('[reload] independent new reload failure')).toBeTruthy();
    await poll(); expect(api.r18dev.getDumpStatus).toHaveBeenCalledTimes(2);
  });

  it('current WebSocket error text survives its internal status refresh',async()=>{
    await mount();
    await fireEvent.change(document.querySelector('input[type="file"]') as HTMLInputElement,{target:{files:[new File(['synthetic'],'test.db')]}}); await settle();
    api.r18dev.getDumpStatus.mockResolvedValue(status({last_error:oldError,last_error_kind:'validation'}));
    ws.messages=[{job_id:'r18dev-dump-download',message:'error',status:'error',progress:0,error:'independent current socket failure'}];
    for(const fn of ws.subscribers)fn({messages:ws.messages}); await settle(); await poll();
    expect(screen.getByText('[validation] independent current socket failure')).toBeTruthy();
    expect(screen.queryByText(new RegExp(oldError))).toBeNull();
  });
});
