// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { BackendConfig } from '../lib/backend';
import { MarketOverviewWorkspace } from './MarketOverviewWorkspace';

const { request } = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock('../lib/backend', () => ({ requestJSON: request }));
const config: BackendConfig = { backendUrl: 'http://127.0.0.1:20081', token: 'fixture-only' };
const meta = { source: 'tencent', fetched_at: '2026-09-30T10:00:00+08:00', latency_ms: 0, stale: true, fallback_reason: 'fixture previous snapshot' };
let host: HTMLDivElement;
let root: Root;
const settings = vi.fn();

beforeEach(() => {
	vi.useFakeTimers(); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
	host = document.createElement('div'); document.body.append(host); root = createRoot(host);
	settings.mockReset(); request.mockReset();
	request.mockImplementation((_config: BackendConfig, route: string) => {
		if (route.startsWith('/api/v1/market/news')) return Promise.resolve({ data: [] });
		if (route === '/api/v1/themes/overview') return Promise.resolve({ data: [], meta });
		if (/^\/api\/v1\/(?:market\/(?:margin-balance|billboard)|research\/(?:announcements|institution-reports|industries))\?/.test(route)) return Promise.resolve({ data: [], meta: { ...meta, source: 'eastmoney', stale: false, fallback_reason: '' } });
		throw new Error(`Unexpected fixture request: ${route}`);
	});
});
afterEach(async () => {
	await act(async () => root.unmount()); host.remove();
	vi.useRealTimers(); vi.unstubAllGlobals(); window.location.hash = '';
});
async function render(view: string) {
	window.location.hash = `#market/${view}`;
	await act(async () => root.render(<MarketOverviewWorkspace config={config} refreshKey={0} onAskAI={vi.fn()} onOpenSourceSettings={settings} />));
}

describe('market source boundaries', () => {
	it('keeps snapshot provenance but reads global observations only from settings', async () => {
		await render('pulse');
		expect(request).toHaveBeenCalledTimes(2);
		await act(async () => vi.advanceTimersByTimeAsync(60_000));
		expect(request).toHaveBeenCalledTimes(2);
		expect(host.textContent).not.toContain('数据源最近观测');
		expect(host.querySelector('.source-health-panel')).toBeNull();
		expect(host.textContent).toContain('腾讯财经');
		expect(host.textContent).toContain('缓存快照');
		expect(host.textContent).toContain('fixture previous snapshot');
		const button = [...host.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent === '数据源设置')!;
		await act(async () => button.click()); expect(settings).toHaveBeenCalledTimes(1);
	});
	it.each([
		['margin-balance', '/api/v1/market/margin-balance'],
		['billboard', '/api/v1/market/billboard'],
		['announcements', '/api/v1/research/announcements'],
		['institution-reports', '/api/v1/research/institution-reports'],
		['industry-research', '/api/v1/research/industries'],
	])('fetches and allows refresh of the restored %s module', async (view, endpoint) => {
		await render(view);
		expect(request).toHaveBeenCalledTimes(1); expect(request.mock.calls[0][1]).toContain(`${endpoint}?`);
		expect(host.textContent).not.toContain('目前尚无可用替代接口'); expect(host.textContent).toContain('东方财富');
		const button = host.querySelector<HTMLButtonElement>('.market-refresh-button')!;
		await act(async () => button.click()); expect(request).toHaveBeenCalledTimes(2);
		expect(request.mock.calls.some(call => call[1] === '/api/v1/sources')).toBe(false);
		expect(host.querySelector<HTMLButtonElement>('.market-ai-button')?.disabled).toBe(true);
	});
});
