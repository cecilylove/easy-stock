// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AppSettings, BackendConfig } from '../lib/backend';
import { sourceIntegrations } from '../lib/source-integrations';
import { SettingsDrawer } from './SettingsDrawer';

const { request } = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock('../lib/backend', () => ({ requestJSON: request }));

const config: BackendConfig = { backendUrl: 'http://127.0.0.1:20081', token: 'fixture-only' };
const settings: AppSettings = {
	hermes: { available: true, configured: true, api_key_configured: true, version: 'fixture' },
	llm: { provider: 'openai', base_url: 'https://fixture.example/v1', model: 'fixture-model', api_mode: 'chat_completions', response_timeout_seconds: 300, api_key: { configured: true } },
	llm_profiles: [{ id: 'fixture-profile', name: 'Fixture model', provider: 'openai', base_url: 'https://fixture.example/v1', model: 'fixture-model', api_mode: 'chat_completions', api_key: { configured: true } }],
	active_llm_profile_id: 'fixture-profile',
	credentials: {
		tushare_token: { configured: true, masked: 'historical-tushare-mask' },
		ths_cookie: { configured: true, masked: 'historical-ths-mask' },
		eastmoney_cookie: { configured: true, masked: 'historical-eastmoney-mask' },
		xueqiu_cookie: { configured: false }, wechat_api_token: { configured: false },
	},
	review_automation: { profiles: [{ id: 'fixture-xueqiu', source: 'xueqiu', name: 'Fixture review', base_url: 'https://xueqiu.com', credential: { configured: false }, enabled: true, auto_analyze: true, sync_hour: 7 }] },
};

let host: HTMLDivElement;
let root: Root;
let savedBodies: Record<string, unknown>[];
let scrollIntoViewDescriptor: PropertyDescriptor | undefined;
let visibilityDescriptor: PropertyDescriptor | undefined;
let sourceResponse: () => Promise<unknown>;
let probeResponse: () => Promise<unknown>;
const sourceCalls = () => request.mock.calls.filter((call: unknown[]) => call[1] === '/api/v1/sources');
const probeCalls = () => request.mock.calls.filter((call: unknown[]) => call[1] === '/api/v1/sources/check');
const probePayload = () => ({ sources: [], checked_at: new Date().toISOString(), probes: sourceIntegrations.map(source => ({ id: source.id, status: source.id === 'cls' ? 'unavailable' : 'available', checked_at: new Date().toISOString(), scope: `${source.name}代表接口`, message: source.id === 'cls' ? '代表接口返回无效内容' : '返回有效内容', latency_ms: 12 })) });
const refreshButton = () => host.querySelector<HTMLButtonElement>('.source-check-button')!;
const close = vi.fn();
const saved = vi.fn();

beforeEach(async () => {
	vi.useFakeTimers(); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
	vi.setSystemTime(new Date('2026-10-01T01:00:00Z'));
	visibilityDescriptor = Object.getOwnPropertyDescriptor(document, 'visibilityState');
	Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
	sourceResponse = () => Promise.resolve({ sources: [{ id: 'sina', name: '新浪', category: 'quote', ok: true, status: 'available', checked_at: '2026-10-01T00:59:00Z', last_success: '2026-10-01T00:59:00Z' }, { id: 'eastmoney', name: '东方财富', category: 'f10', ok: true, status: 'available' }] });
	probeResponse = () => Promise.resolve(probePayload());
	scrollIntoViewDescriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollIntoView');
	Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() });
	host = document.createElement('div'); document.body.append(host); root = createRoot(host);
	savedBodies = []; close.mockReset(); saved.mockReset(); request.mockReset();
	request.mockImplementation((_config: BackendConfig, route: string, options?: RequestInit) => {
		if (route === '/api/v1/sources') return sourceResponse();
		if (route === '/api/v1/sources/check') return probeResponse();
		if (route === '/api/v1/settings') {
			if (options?.method === 'PUT') savedBodies.push(JSON.parse(String(options.body)));
			return Promise.resolve({ data: settings });
		}
		if (route === '/api/v1/settings/agent') return Promise.resolve({ data: { skills: [], mcp_servers: [] } });
		if (route === '/api/v1/settings/agent/skills/market' || route === '/api/v1/settings/agent/skills/market/sources') return Promise.resolve({ data: [] });
		if (route === '/api/v1/settings/llm/test') return Promise.resolve({ data: { latency_ms: 1, model: 'fixture-model', api_mode: 'chat_completions', response: 'fixture response' } });
		throw new Error(`Unexpected fixture request: ${route}`);
	});
	await act(async () => root.render(<SettingsDrawer config={config} open onClose={close} onSaved={saved} initialSection="data-sources" />));
});
afterEach(async () => {
	await act(async () => root.unmount()); host.remove();
	if (scrollIntoViewDescriptor) Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', scrollIntoViewDescriptor);
	else Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView');
	if (visibilityDescriptor) Object.defineProperty(document, 'visibilityState', visibilityDescriptor);
	else Reflect.deleteProperty(document, 'visibilityState');
	vi.useRealTimers(); vi.unstubAllGlobals();
});

describe('implemented data-source settings', () => {
	it('loads a registry catalog and accepts probes for its newly registered provider only', async () => {
		const catalog = [{ id: 'new_vendor', name: '新的行情供应商', mode: 'public', kinds: ['market'], usage: '报价', configuration: '自动取数', capabilities: ['quote'], probeScope: '报价代表接口', implemented: true, enabled: true }];
		sourceResponse = () => Promise.resolve({ sources: [], probes: [], catalog });
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		expect(host.querySelectorAll('[data-source]')).toHaveLength(1);
		expect(host.querySelector('[data-source="new_vendor"]')!.textContent).toContain('新的行情供应商');
		expect(host.querySelector('[data-source="sina"]')).toBeNull();
		probeResponse = () => Promise.resolve({ sources: [], catalog, checked_at: new Date().toISOString(), probes: [{ id: 'new_vendor', status: 'available', checked_at: new Date().toISOString(), scope: '报价代表接口', message: '有效报价', latency_ms: 1 }] });
		await act(async () => refreshButton().click());
		expect(host.querySelector('.source-check-error')).toBeNull();
		expect(host.querySelector('[data-source="new_vendor"] .source-probe-status')!.textContent).toBe('代表接口返回有效数据');
	});
	it('shows all sources checking immediately, prevents duplicate posts, and then shows individual results', async () => {
		let finish!: (value: unknown) => void;
		probeResponse = () => new Promise(resolve => { finish = resolve; });
		await act(async () => { refreshButton().click(); refreshButton().click(); });
		expect(probeCalls()).toHaveLength(1); expect(refreshButton().disabled).toBe(true);
		expect(refreshButton().textContent).toBe('检测中…'); expect(refreshButton().querySelector('.spin')).not.toBeNull();
		for (const source of sourceIntegrations) {
			const row = host.querySelector(`[data-source="${source.id}"]`)!;
			expect(row.getAttribute('aria-busy')).toBe('true'); expect(row.querySelector('[role="status"]')!.textContent).toBe('检测中…');
		}
		await act(async () => vi.advanceTimersByTimeAsync(90_000));
		expect(sourceCalls()).toHaveLength(1); expect(probeCalls()).toHaveLength(1);
		await act(async () => finish(probePayload()));
		expect(refreshButton().disabled).toBe(false);
		for (const source of sourceIntegrations) {
			const row = host.querySelector(`[data-source="${source.id}"]`)!;
			expect(row.getAttribute('aria-busy')).toBe('false');
			expect(row.querySelector('.source-probe-status')!.textContent).toBe(source.id === 'cls' ? '代表接口失败 · 仅此检测范围' : '代表接口返回有效数据');
			expect(row.textContent).toContain('检测时间：'); expect(row.textContent).toContain('耗时 12 ms'); expect(row.textContent).toContain('检测用途：');
		}
		expect(host.querySelector('[data-source="cls"]')!.textContent).toContain('代表接口返回无效内容');
		expect(host.textContent).toContain('不代表全部功能'); expect(savedBodies).toHaveLength(0);
	});
	it('retains old probe records visibly after a network failure and clears that error on retry', async () => {
		await act(async () => refreshButton().click());
		const oldTime = host.querySelector('[data-source="sina"]')!.textContent!.match(/检测时间：[^·]+/)![0];
		probeResponse = () => Promise.reject(new Error('检测连接中断'));
		await act(async () => refreshButton().click());
		expect(host.querySelector('.source-check-error[role="alert"]')!.textContent).toContain('本次检测请求失败：检测连接中断');
		const row = host.querySelector('[data-source="sina"]')!;
		expect(row.querySelector('.source-probe-status')!.textContent).toBe('检测结果未更新 · 上次检测记录');
		expect(row.textContent).toContain(oldTime); expect(row.querySelector('.source-probe-status')!.textContent).not.toContain('代表接口返回有效数据');
		let finish!: (value: unknown) => void;
		probeResponse = () => new Promise(resolve => { finish = resolve; });
		await act(async () => refreshButton().click());
		expect(host.querySelector('.source-check-error')).toBeNull(); expect(row.querySelector('.source-probe-status')!.textContent).toBe('检测中…');
		await act(async () => finish(probePayload()));
		expect(row.querySelector('.source-probe-status')!.textContent).toBe('代表接口返回有效数据');
	});
	it('rejects malformed check envelopes instead of displaying invented successful probes', async () => {
		probeResponse = () => Promise.resolve({ data: probePayload() });
		await act(async () => refreshButton().click());
		expect(host.querySelector('.source-check-error')!.textContent).toContain('响应格式异常');
		for (const source of sourceIntegrations) expect(host.querySelector(`[data-source="${source.id}"] .source-probe-status`)!.textContent).toBe('未主动检测');
	});
	it.each(['empty', 'missing', 'duplicate'])('rejects %s source check results and keeps the previous results visibly historical', async kind => {
		await act(async () => refreshButton().click());
		const payload = probePayload();
		payload.probes = kind === 'empty' ? [] : kind === 'missing' ? payload.probes.filter(probe => probe.id !== 'eastmoney') : [...payload.probes, payload.probes[0]];
		probeResponse = () => Promise.resolve(payload);
		await act(async () => refreshButton().click());
		expect(host.querySelector('.source-check-error')!.textContent).toContain('缺少完整且唯一的来源结果');
		for (const source of sourceIntegrations) expect(host.querySelector(`[data-source="${source.id}"] .source-probe-status`)!.textContent).toBe('检测结果未更新 · 上次检测记录');
	});
	it('ignores a late record read that started before the manual check', async () => {
		let finishRead!: (value: unknown) => void;
		sourceResponse = () => new Promise(resolve => { finishRead = resolve; });
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		await act(async () => refreshButton().click());
		await act(async () => finishRead({ sources: [], probes: [] }));
		expect(host.querySelector('[data-source="sina"] .source-probe-status')!.textContent).toBe('代表接口返回有效数据');
	});
	it('aborts a manual check on close and reloads stored probes on reopen without accepting the late response', async () => {
		let finishOld!: (value: unknown) => void;
		probeResponse = () => new Promise(resolve => { finishOld = resolve; });
		await act(async () => refreshButton().click());
		const signal = probeCalls()[0][2].signal as AbortSignal;
		await act(async () => root.render(<SettingsDrawer config={config} open={false} onClose={close} />));
		expect(signal.aborted).toBe(true);
		const stored = probePayload();
		sourceResponse = () => Promise.resolve(stored);
		await act(async () => root.render(<SettingsDrawer config={config} open onClose={close} />));
		await act(async () => finishOld({ ...probePayload(), probes: probePayload().probes.map(probe => ({ ...probe, message: '关闭之前的迟到检测' })) }));
		expect(host.textContent).not.toContain('关闭之前的迟到检测'); expect(host.querySelector('[data-source="sina"] .source-probe-status')!.textContent).toBe('代表接口返回有效数据');
		expect(probeCalls()).toHaveLength(1); expect(sourceCalls()).toHaveLength(2);
	});
	it('aborts a manual check when the backend changes and isolates the old result', async () => {
		let finishOld!: (value: unknown) => void;
		probeResponse = () => new Promise(resolve => { finishOld = resolve; });
		await act(async () => refreshButton().click());
		const signal = probeCalls()[0][2].signal as AbortSignal;
		sourceResponse = () => Promise.resolve({ sources: [], probes: [] });
		await act(async () => root.render(<SettingsDrawer config={{ ...config, token: 'different-backend-token' }} open onClose={close} />));
		expect(signal.aborted).toBe(true); expect(refreshButton().disabled).toBe(false);
		await act(async () => finishOld(probePayload()));
		expect(host.querySelector('[data-source="sina"] .source-probe-status')!.textContent).toBe('未主动检测');
	});
	it('ages stored probes after ten minutes and keeps automatic reads passive', async () => {
		const stored = probePayload();
		sourceResponse = () => Promise.resolve(stored);
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		expect(host.querySelector('[data-source="sina"] .source-probe-status')!.textContent).toBe('代表接口返回有效数据');
		await act(async () => vi.advanceTimersByTimeAsync(570_000));
		expect(host.querySelector('[data-source="sina"] .source-probe-status')!.textContent).toBe('检测已过期');
		expect(probeCalls()).toHaveLength(0);
	});
	it('renders real handler sources envelope statuses instead of treating successful records as undetected', async () => {
		// GET /api/v1/sources returns { sources: [...] }, unlike settings routes' { data: ... }.
		sourceResponse = () => Promise.resolve({ sources: [
			{ id: 'sina', name: '新浪财经', category: 'quote,kline', ok: true, status: 'available', checked_at: '2026-10-01T01:00:00Z' },
			{ id: 'tencent', name: '腾讯财经', category: 'index,sector', ok: true, status: 'available', checked_at: '2026-10-01T01:00:00Z' },
			{ id: 'cls', name: '财联社', category: 'news', ok: false, status: 'degraded', checked_at: '2026-10-01T01:00:00Z', message: '快讯取数失败' },
		] });
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		for (const id of ['sina', 'tencent']) expect(host.querySelector(`[data-source="${id}"] .source-observation-status`)!.textContent).toBe('业务最近观测：最近可用');
		expect(host.querySelector('[data-source="cls"] .source-observation-status')!.textContent).toBe('业务最近观测：最近失败 / 降级');
		expect(host.querySelector('[data-source="cls"]')!.textContent).toContain('快讯取数失败');
		expect(host.querySelector('[data-source="cffex"] .source-probe-status')!.textContent).toBe('未主动检测');
	});
	it('reports a malformed envelope and preserves previously observed results', async () => {
		sourceResponse = () => Promise.resolve({ data: [] });
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		expect(host.textContent).toContain('数据源观测响应格式异常');
		expect(host.querySelector('[data-source="sina"]')!.textContent).toContain('上次读取记录');
	});
	it('aborts an in-flight read on close and ignores its late result after reopening', async () => {
		let resolveOld!: (value: unknown) => void;
		sourceResponse = () => new Promise(resolve => { resolveOld = resolve; });
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		const signal = sourceCalls().at(-1)![2].signal as AbortSignal;
		await act(async () => root.render(<SettingsDrawer config={config} open={false} onClose={close} />));
		expect(signal.aborted).toBe(true);
		sourceResponse = () => Promise.resolve({ sources: [] });
		await act(async () => root.render(<SettingsDrawer config={config} open onClose={close} />));
		await act(async () => resolveOld({ sources: [{ id: 'sina', name: '新浪', category: 'quote', status: 'degraded', message: '关闭之前的迟到记录', ok: false }] }));
		expect(host.textContent).not.toContain('关闭之前的迟到记录'); expect(host.querySelector('[data-source="sina"]')!.textContent).toContain('暂无业务观测');
	});
	it('does not read when reopened hidden and starts on becoming visible', async () => {
		await act(async () => root.render(<SettingsDrawer config={config} open={false} onClose={close} />));
		Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
		await act(async () => root.render(<SettingsDrawer config={config} open onClose={close} />));
		await act(async () => vi.advanceTimersByTimeAsync(60_000)); expect(sourceCalls()).toHaveLength(1);
		Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
		await act(async () => document.dispatchEvent(new Event('visibilitychange'))); expect(sourceCalls()).toHaveLength(2);
	});
	it('only reads observations while open and visible, resumes on visibility, and never probes or writes settings', async () => {
		expect(sourceCalls()).toHaveLength(1);
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		expect(sourceCalls()).toHaveLength(2);
		Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
		await act(async () => vi.advanceTimersByTimeAsync(90_000));
		expect(sourceCalls()).toHaveLength(2);
		Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
		await act(async () => document.dispatchEvent(new Event('visibilitychange')));
		expect(sourceCalls()).toHaveLength(3);
		await act(async () => root.render(<SettingsDrawer config={config} open={false} onClose={close} />));
		await act(async () => vi.advanceTimersByTimeAsync(90_000));
		expect(sourceCalls()).toHaveLength(3); expect(savedBodies).toHaveLength(0);
		for (const call of sourceCalls()) expect(call[2]).toEqual({ signal: expect.any(AbortSignal) });
	});
	it('retains observations after a read failure and ages them without changing their timestamps', async () => {
		const initialTime = host.querySelector('[data-source="sina"] small:last-child')!.textContent!;
		sourceResponse = () => Promise.reject(new Error('连接已断开'));
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		expect(host.textContent).toContain('连接已断开'); expect(host.textContent).toContain('不代表当前可用性');
		expect(host.querySelector('[data-source="sina"]')!.textContent).toContain('上次读取记录');
		await act(async () => vi.advanceTimersByTimeAsync(600_000));
		const row = host.querySelector('[data-source="sina"]')!;
		expect(row.textContent).toContain('观测已过期'); expect(row.textContent).not.toContain('最近可用');
		expect(row.querySelector('small:last-child')!.textContent).toBe(initialTime);
	});
	it('does not overlap slow reads and discards cancelled responses after backend switches', async () => {
		let resolveOld!: (value: unknown) => void;
		sourceResponse = () => new Promise(resolve => { resolveOld = resolve; });
		await act(async () => vi.advanceTimersByTimeAsync(30_000));
		const oldSignal = sourceCalls().at(-1)![2].signal as AbortSignal;
		await act(async () => vi.advanceTimersByTimeAsync(90_000));
		expect(sourceCalls()).toHaveLength(2);
		sourceResponse = () => Promise.resolve({ sources: [{ id: 'cffex', name: 'CFFEX', category: 'futures', status: 'available', ok: true, checked_at: new Date().toISOString() }] });
		await act(async () => root.render(<SettingsDrawer config={{ ...config, backendUrl: 'http://127.0.0.1:29981' }} open onClose={close} />));
		expect(oldSignal.aborted).toBe(true);
		await act(async () => resolveOld({ sources: [{ id: 'sina', name: '新浪', status: 'degraded', message: '旧后端迟到错误', checked_at: new Date().toISOString() }] }));
		expect(host.textContent).not.toContain('旧后端迟到错误'); expect(host.querySelector('[data-source="cffex"]')!.textContent).toContain('最近可用');
		expect(host.querySelector('[data-source="sina"]')!.textContent).toContain('暂无业务观测');
	});
	it('exposes per-source keyboard tooltip and manual record reads without submitting the form', async () => {
		const helper = host.querySelector<HTMLButtonElement>('button[aria-label="中国金融期货交易所用途说明"]')!;
		expect(helper.type).toBe('button');
		await act(async () => helper.focus()); expect(document.activeElement).toBe(helper);
		const tooltip = document.getElementById(helper.getAttribute('aria-describedby')!)!;
		expect(tooltip.getAttribute('role')).toBe('tooltip'); expect(tooltip.textContent).toContain('打开股指期货或会员功能后自动'); expect(tooltip.textContent).toContain('不提供价格、基差、实时盘口');
		await act(async () => refreshButton().click());
		expect(sourceCalls()).toHaveLength(1); expect(probeCalls()).toHaveLength(1); expect(probeCalls()[0][2].method).toBe('POST'); expect(savedBodies).toHaveLength(0);
		expect(host.querySelector('[data-source="eastmoney"]')).not.toBeNull(); expect(host.querySelectorAll('.source-probe-status')).toHaveLength(7);
	});
	it('renders the real drawer without credentials for unimplemented sources', () => {
		expect(host.querySelector('[role="dialog"]')).not.toBeNull();
		const sourceSection = host.querySelector<HTMLElement>('[aria-label="行情与内容数据源"]')!;
		expect(sourceSection).not.toBeNull(); expect(sourceSection.querySelectorAll('input')).toHaveLength(0);
		const labels = [...host.querySelectorAll('label')].map(label => label.textContent).join('\n');
		expect(labels).not.toMatch(/Tushare|同花顺.*(?:Cookie|Token)|东方财富.*Cookie|雪球.*Cookie/);
		expect(host.textContent).not.toMatch(/historical-(?:tushare|ths|eastmoney)-mask/);
		expect(host.textContent).toContain('1 项已配置'); // Only the configured model counts; hidden legacy source keys do not.
		expect(host.querySelectorAll('input[type="password"]')).toHaveLength(1); // Existing model API key remains configurable.
		expect(host.textContent).toContain('使用内置浏览器登录雪球');
	});

	it.each(['保存设置', '保存并测试连接'])('%s never writes or clears hidden legacy market credentials', async label => {
		const button = [...host.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent === label)!;
		expect(button).toBeDefined(); expect(button.disabled).toBe(false);
		await act(async () => button.click());
		expect(savedBodies).toHaveLength(1);
		const body = savedBodies[0];
		expect(body).not.toHaveProperty('credentials');
		const serialized = JSON.stringify(body);
		for (const key of ['tushare_token', 'ths_cookie', 'eastmoney_cookie']) {
			expect(serialized).not.toContain(key);
			expect(body.clear_secrets).not.toContain(key);
		}
		expect(body.clear_secrets).toEqual(['wechat_api_token']); // Existing WeChat migration behavior is separate.
		expect(body.llm_profiles).toEqual([expect.objectContaining({ id: 'fixture-profile', clear_api_key: false })]);
		expect(serialized).not.toContain('historical-');
	});
});
