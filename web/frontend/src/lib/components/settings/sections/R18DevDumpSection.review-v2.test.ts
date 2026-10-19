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


function queued<T>() { let resolve!:(v:T)=>void; let reject!:(e:Error)=>void; const promise=new Promise<T>((yes,no)=>{resolve=yes;reject=no;});return {promise,resolve,reject}; }
const orders = [[0,1,2],[0,2,1],[1,0,2],[1,2,0],[2,0,1],[2,1,0]];
describe('independent v2 ordering and lifetime',()=>{
 beforeEach(()=>{
  vi.useFakeTimers();vi.resetAllMocks();ws.messages=[];ws.subscribers.clear();
  api.r18dev.getDumpStatus.mockResolvedValue(status());api.config.getConfig.mockResolvedValue({metadata:{}});api.request.mockResolvedValue({});
  api.r18dev.uploadDump.mockResolvedValue({message:'staged'});api.r18dev.downloadDump.mockResolvedValue(undefined);api.r18dev.clearDump.mockResolvedValue(undefined);
 });
 afterEach(()=>{cleanup();vi.clearAllTimers();vi.useRealTimers();});
 it.each(orders)('latest of three Refresh requests owns a real completed failure: %s %s %s',async(a,b,c)=>{
  const requests=[queued<DumpStatus>(),queued<DumpStatus>(),queued<DumpStatus>()];
  api.r18dev.getDumpStatus.mockResolvedValueOnce(status());
  for(const req of requests)api.r18dev.getDumpStatus.mockReturnValueOnce(req.promise);
  await mount();const refresh=screen.getByRole('button',{name:/^Refresh$/});
  await act(()=>{refresh.click();refresh.click();refresh.click();});await settle();
  expect(api.r18dev.getDumpStatus).toHaveBeenCalledTimes(4);
  let latest=false;
  for(const i of [a,b,c]){
   requests[i].resolve(status({last_error:`ordered failure ${i}`,last_error_kind:'reload',enabled:i!==2}));await settle();
   if(i===2)latest=true;
   if(latest){expect(screen.getByText('[reload] ordered failure 2')).toBeTruthy();expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('false');}
   else expect(screen.queryByRole('button',{name:/^Refresh$/})).toBeNull();
   expect(screen.queryByText('[reload] ordered failure 0')).toBeNull();expect(screen.queryByText('[reload] ordered failure 1')).toBeNull();
  }
 });
 it('a current status rejection stays current after an older successful reply',async()=>{
  const old=queued<DumpStatus>(),fresh=queued<DumpStatus>();
  api.r18dev.getDumpStatus.mockResolvedValueOnce(status()).mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
  await mount();const refresh=screen.getByRole('button',{name:/^Refresh$/});
  await act(()=>{refresh.click();refresh.click();});await settle();
  fresh.reject(new Error('current status request failed'));await settle();
  expect(screen.getByText('current status request failed')).toBeTruthy();
  old.resolve(status({last_error:'obsolete successful payload'}));await settle();
  expect(screen.getByText('current status request failed')).toBeTruthy();expect(screen.queryByText('obsolete successful payload')).toBeNull();
 });
 it('a download still surfaces its real completed failure after current polling',async()=>{
  api.r18dev.getDumpStatus.mockResolvedValueOnce(status()).mockResolvedValue(status({last_error:'download validation failed',last_error_kind:'validation'}));
  await mount();await fireEvent.click(screen.getByRole('button',{name:/Check for Updates/i}));await settle();
  expect(api.r18dev.downloadDump).toHaveBeenCalledWith(true);await poll();
  expect(screen.getByText('[validation] download validation failed')).toBeTruthy();await poll();expect(api.r18dev.getDumpStatus).toHaveBeenCalledTimes(2);
 });
 it.each(['upload','download'])('unmount before %s acknowledgement must not restart a poll loop',async(operation)=>{
  const ack=queued<void>();
  if(operation==='upload')api.r18dev.uploadDump.mockReturnValueOnce(ack.promise);else api.r18dev.downloadDump.mockReturnValueOnce(ack.promise);
  const view=await mount();
  api.r18dev.getDumpStatus.mockResolvedValue(status({running:true}));
  if(operation==='upload')await fireEvent.change(document.querySelector('input[type="file"]') as HTMLInputElement,{target:{files:[new File(['synthetic'],'test.db')]}});
  else await fireEvent.click(screen.getByRole('button',{name:/Check for Updates/i}));
  await settle();expect(operation==='upload'?api.r18dev.uploadDump:api.r18dev.downloadDump).toHaveBeenCalledTimes(1);
  view.unmount();ack.resolve();await settle();await poll();await poll();await poll();
  expect(api.r18dev.getDumpStatus).toHaveBeenCalledTimes(1);
 });
 it('unmount after upload acknowledgement stops its sleeping poll loop',async()=>{
  const view=await mount();await fireEvent.change(document.querySelector('input[type="file"]') as HTMLInputElement,{target:{files:[new File(['synthetic'],'test.db')]}});await settle();
  view.unmount();await poll();expect(api.r18dev.getDumpStatus).toHaveBeenCalledTimes(1);
 });
 it('a status rejection after unmount does not restart polling or publish an unhandled rejection',async()=>{
  const initial=queued<DumpStatus>();api.r18dev.getDumpStatus.mockReturnValueOnce(initial.promise);
  const view=await mount();view.unmount();initial.reject(new Error('late status failure'));await settle();await poll();expect(api.r18dev.getDumpStatus).toHaveBeenCalledTimes(1);
 });
});
