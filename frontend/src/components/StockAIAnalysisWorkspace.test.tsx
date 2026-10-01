// @vitest-environment jsdom
import { renderToStaticMarkup } from 'react-dom/server';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { BackendConfig, HotStockRankData } from '../lib/backend';
import { StockAIAnalysisWorkspace } from './StockAIAnalysisWorkspace';

const { request } = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock('../lib/backend', () => ({ requestJSON: request }));
const config: BackendConfig = { backendUrl: 'http://127.0.0.1:20081', token: 'fixture-only' };
let host: HTMLDivElement;
let root: Root;
let ranks: HotStockRankData;
beforeEach(() => {
	window.localStorage.clear(); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
	host = document.createElement('div'); document.body.append(host); root = createRoot(host);
	ranks = { stocks: [{ symbol: '000002.SZ', code: '000002', name: 'Fixture stock', source_count: 1, consensus_score: 99, ranks: { ths: 1 } }], sources: [{ id: 'ths', name: '同花顺', available: true, count: 1 }], total: 1, updated_at: '2026-09-30T10:00:00+08:00', expires_at: '2026-09-30T10:02:00+08:00', stale: false };
	request.mockReset(); request.mockImplementation((_config: BackendConfig, route: string) => {
		if (route === '/api/v1/settings') return Promise.resolve({ data: { llm_profiles: [] } });
		if (route === '/api/v1/settings/agent') return Promise.resolve({ data: { reasoning_effort: 'medium' } });
		if (route === '/api/v1/stocks/directory') return Promise.resolve({ data: { stocks: [], stale: false } });
		if (route === '/api/v1/stocks/research') return Promise.resolve({ data: [] });
		if (route.startsWith('/api/v1/stocks/hot-ranks')) return Promise.resolve({ data: ranks });
		throw new Error(`Unexpected fixture request: ${route}`);
	});
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); vi.unstubAllGlobals(); window.localStorage.clear(); });
async function render() {
	await act(async () => root.render(<StockAIAnalysisWorkspace config={config} refreshKey={0} mode="analysis" onAskAI={vi.fn()} onOpenSettings={vi.fn()} />));
}
describe('research popularity source presentation', () => {
	it('names both implemented popularity sources before fetching without inventing a dual-source result', () => {
		const html = renderToStaticMarkup(<StockAIAnalysisWorkspace config={null} refreshKey={0} mode="analysis" onAskAI={vi.fn()} onOpenSettings={vi.fn()} />);
		expect(html).toContain('同花顺 · 东方财富'); expect(html).toContain('全部');
		expect(html).not.toContain('同花顺和东方财富人气股');
		expect(html).not.toContain('并集');
	});
	it('shows the actual current single-source list and rank', async () => {
		await render(); const sidebar = host.querySelector('.stock-hot-sidebar')!;
		expect(sidebar.textContent).toContain('Fixture stock'); expect(sidebar.textContent).toContain('同花顺');
		expect(sidebar.querySelector('[title="同花顺第 1 名"]')).not.toBeNull();
		expect(sidebar.textContent).toContain('最近人气榜快照');
		expect(sidebar.textContent).not.toMatch(/东方财富|并集|共识|实时人气榜/);
	});
	it('marks stale provider snapshots without declaring every EastMoney result historical', async () => {
		ranks = { ...ranks, stale: true, stocks: [{ ...ranks.stocks[0], ranks: { eastmoney: 1 } }], sources: [{ id: 'eastmoney', name: '东方财富', available: true, count: 1 }] };
		await render(); const sidebar = host.querySelector('.stock-hot-sidebar')!;
		expect(sidebar.textContent).toContain('东方财富');
		expect(sidebar.textContent).toContain('历史榜单快照');
		expect(sidebar.querySelector('[title="东方财富第 1 名"]')).not.toBeNull();
		expect(sidebar.textContent).not.toContain('实时人气榜');
	});
	it('renders fresh combined ranks from both platforms and preserves the per-platform labels', async () => {
		ranks = { ...ranks, stocks: [{ ...ranks.stocks[0], source_count: 2, ranks: { ths: 1, eastmoney: 2 } }], sources: [...ranks.sources, { id: 'eastmoney', name: '东方财富', available: true, count: 1 }] };
		await render(); const sidebar = host.querySelector('.stock-hot-sidebar')!;
		expect(sidebar.textContent).toContain('同花顺 · 东方财富');
		expect(sidebar.querySelector('[title="东方财富第 2 名"]')).not.toBeNull(); expect(sidebar.querySelector('[title="同花顺第 1 名"]')).not.toBeNull();
		expect(sidebar.textContent).toContain('最近人气榜快照'); expect(sidebar.textContent).not.toContain('历史快照');
	});
});
