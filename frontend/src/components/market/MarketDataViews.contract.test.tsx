// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { KLine, MarketFundFlow, MarketIndustryMomentum, MarketIndexSnapshot, SourceMeta } from '../../lib/backend';
import { CoreIndexView, FundFlowView, IndustryMomentumView, ResearchView } from './MarketDataViews';

beforeEach(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true));
afterEach(() => vi.unstubAllGlobals());

const meta = (fields: string[]): SourceMeta => ({ source: 'test', fetched_at: '', stale: false, latency_ms: 0, fields_known: true, available_fields: fields });
const industry = (name: string, fields: string[], score = 50): MarketIndustryMomentum => ({ code: name, name, score, change_percent: 0, five_day_change_percent: 0, twenty_day_change_percent: 0, turnover_rate: 0, rising_count: 0, falling_count: 0, main_net_inflow: 0, leader_change_percent: 0, meta: meta(fields) });
const flow = (name: string, fields: string[], net = 0, main = 0): MarketFundFlow => ({ dimension: 'industry', code: name, name, price: 0, change_percent: 0, inflow: 0, outflow: 0, net_inflow: net, net_inflow_ratio: 0, main_inflow: 0, main_outflow: 0, main_net_inflow: main, main_net_inflow_ratio: 0, retail_inflow: 0, retail_outflow: 0, retail_net_inflow: 0, retail_net_inflow_ratio: 0, super_large_net_inflow: 0, super_large_net_inflow_ratio: 0, large_net_inflow: 0, large_net_inflow_ratio: 0, medium_net_inflow: 0, medium_net_inflow_ratio: 0, small_net_inflow: 0, small_net_inflow_ratio: 0, leader_price: 0, leader_change_percent: 0, leader_net_inflow_ratio: 0, meta: meta(fields) });
function root(html: string) { const element = document.createElement('div'); element.innerHTML = html; return element; }

describe('market row field contracts', () => {
 it('shows actual disclosure fallback and distinguishes platform text from full report', () => {
  const item={kind:'stock',id:'sina-1',title:'第三方研报',published_at:'2026-09-22T00:00:00+08:00',url:'https://stock.finance.sina.com.cn/report',content_status:'unavailable' as const,content_scope:'platform-readable',content_issue:'正文未取得，非PDF全文',meta:{...meta(['title']),source:'sina:reports',query_coverage:'bounded' as const,fallback_reason:'备用失败保留有界列表'}};
  const html=renderToStaticMarkup(<ResearchView items={[item]} kind="stock" queryDraft="" onQueryDraft={()=>{}} onSearch={()=>{}} category="all" onCategory={()=>{}} meta={item.meta} />);
  expect(html).toContain('新浪财经');expect(html).toContain('有界列表');expect(html).toContain('正文未取得');expect(html).toContain('非PDF全文');
 });
	it('keeps foreign unknown-offset snapshot time unknown rather than replacing it with history time', () => {
		const item: MarketIndexSnapshot = { id: 'foreign', secid: '', code: '', name: '海外指数', region: 'US', market: 'US', currency: 'USD', price: 123, change: 0, change_percent: 0, status: 'unknown', trade_time: '0001-01-01T00:00:00Z', meta: { ...meta(['price']), time_zone: 'unknown', native_timestamp: '20261001093000' } };
		const line: KLine = { symbol: 'foreign', time: '2026-10-01T00:00:00Z', open: 120, high: 125, low: 119, close: 123, volume: 0, amount: 0, meta: { ...meta(['open', 'high', 'low', 'close']), time_zone: 'UTC' } };
		const element = root(renderToStaticMarkup(<CoreIndexView indexes={[item]} selectedID="foreign" onSelect={() => {}} series={{ index: { ...item, trade_time: line.time, meta: line.meta }, lines: [line], meta: line.meta }} seriesLoading={false} meta={null} />));
		expect(element.querySelector('.market-index-chart-wrap aside')?.textContent).toContain('行情时间未知（来源时间 20261001093000；时区偏移未确认）');
		expect(element.textContent).not.toContain('0001');
		expect(element.querySelector('.market-kline-table article > span')?.textContent).toBe('10-01');
	});
	it('enables industry columns from any valid masked row despite an empty intersection schema', () => {
		const missing = industry('缺值', []);
		const valid = industry('有效', ['score', 'five_day_change_percent'], 0); valid.five_day_change_percent = -3;
		const invalid = industry('非有限', ['score', 'five_day_change_percent'], NaN); invalid.five_day_change_percent = Infinity;
		const element = root(renderToStaticMarkup(<IndustryMomentumView items={[missing, valid, invalid]} meta={meta([])} />));
		expect(element.querySelector<HTMLButtonElement>('button[aria-label="动能降序"]')?.disabled).toBe(false);
		expect(element.querySelector<HTMLButtonElement>('button[aria-label="5 日排序"]')?.disabled).toBe(false);
		expect(element.querySelector<HTMLButtonElement>('button[aria-label="20 日不可排序"]')?.disabled).toBe(true);
		const empty = root(renderToStaticMarkup(<IndustryMomentumView items={[missing, invalid]} meta={meta(['score'])} />));
		expect(empty.querySelector<HTMLButtonElement>('button[aria-label="动能不可排序"]')?.disabled).toBe(true);
	});
	it('enables independent stock money columns from valid rows, not list intersection', () => {
		const valid = flow('有效', ['net_inflow', 'main_net_inflow'], 0, -100); valid.dimension = 'stock';
		const element = root(renderToStaticMarkup(<FundFlowView items={[flow('缺值', []), valid]} dimension="stock" meta={meta([])} />));
		expect(element.querySelector<HTMLButtonElement>('button[aria-label="总净流入降序"]')?.disabled).toBe(false);
		expect(element.querySelector<HTMLButtonElement>('button[aria-label="主力净流入排序"]')?.disabled).toBe(false);
	});
	it('uses one named valid total sector metric even when list intersection excludes it', () => {
		const element = root(renderToStaticMarkup(<FundFlowView items={[flow('总额行业', ['net_inflow'], 100_000_000), flow('主力行业', ['main_net_inflow'], 0, 200_000_000)]} dimension="industry" meta={meta([])} />));
		expect(element.querySelector<HTMLButtonElement>('button[aria-label="总净流入降序"]')?.disabled).toBe(false);
		expect(element.textContent).toContain('榜首总净流入');
		expect(element.textContent).toContain('1.00亿');
		expect(element.textContent).not.toContain('2.00亿');
	});
	it('keeps the parent list metric fixed while filtering to a main-only row', () => {
		const container = document.createElement('div'); const reactRoot = createRoot(container);
		act(() => reactRoot.render(<FundFlowView items={[flow('总额行业', ['net_inflow'], 100_000_000), flow('主力行业', ['main_net_inflow'], 0, 200_000_000)]} dimension="industry" meta={meta([])} />));
		const input = container.querySelector('input')!;
		act(() => {
			Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '主力行业');
			input.dispatchEvent(new Event('input', { bubbles: true }));
		});
		expect(container.querySelectorAll('.flow.sector article')).toHaveLength(1);
		expect(container.querySelector('.flow.sector header')?.textContent).toContain('总净流入');
		expect(container.querySelector('.flow.sector header')?.textContent).not.toContain('主力净流入');
		expect(container.querySelector('.flow.sector article')?.textContent).not.toContain('2.00亿');
		act(() => reactRoot.unmount());
	});
	it('does not manufacture a flat global index or interval return without data', () => {
		const item: MarketIndexSnapshot = { id: 'missing', secid: '', code: '', name: '缺失指数', region: 'US', market: 'US', currency: 'USD', price: 123, change: 0, change_percent: 0, status: 'unavailable', meta: meta([]) };
		const element = root(renderToStaticMarkup(<CoreIndexView indexes={[item]} selectedID="missing" onSelect={() => {}} series={null} seriesLoading={false} meta={meta(['price', 'change_percent'])} />));
		expect(element.textContent).not.toContain('123.00');
		expect(element.textContent).not.toContain('0.00%');
		expect(element.textContent).toContain('区间收益--');
	});
	it('keeps masked rows below real negative values after toggling to ascending', () => {
		const container = document.createElement('div');
		const reactRoot = createRoot(container);
		act(() => reactRoot.render(<IndustryMomentumView items={[industry('缺字段行业', []), industry('负值行业', ['score'], -5), industry('零值行业', ['score'], 0)]} meta={meta(['score'])} />));
		const button = container.querySelector('button[aria-label="动能降序"]')!;
		act(() => button.dispatchEvent(new MouseEvent('click', { bubbles: true })));
		expect([...container.querySelectorAll('.momentum article')].map(row => row.querySelector('span strong')?.textContent)).toEqual(['负值行业', '零值行业', '缺字段行业']);
		act(() => reactRoot.unmount());
	});
	it('does not render or sort a known-empty industry score as the legacy default 50', () => {
		const element = root(renderToStaticMarkup(<IndustryMomentumView items={[industry('缺字段行业', []), industry('有效行业', ['score'], -5)]} meta={meta(['score', 'change_percent', 'rising_count', 'falling_count', 'main_net_inflow'])} />));
		const rows = element.querySelectorAll('.momentum article');
		expect(rows[0].textContent).toContain('有效行业');
		expect(rows[1].textContent).toContain('缺字段行业');
		expect(rows[1].textContent).not.toContain('50.0');
		expect(rows[1].querySelector('b')).toBeNull();
		expect(rows[1].textContent).not.toContain('0.00%');
		expect(rows[1].textContent).not.toContain('0 / 0');
	});

	it('disables a known-empty industry list schema while legacy rows remain compatible', () => {
		const html = renderToStaticMarkup(<IndustryMomentumView items={[industry('缺字段', [])]} meta={meta([])} />);
		expect(html).toContain('动能不可排序');
		const legacy = industry('旧响应', []); legacy.meta = { source: 'legacy', fetched_at: '', stale: false, latency_ms: 0 };
		expect(renderToStaticMarkup(<IndustryMomentumView items={[legacy]} meta={null} />)).toContain('50.0');
	});

	it('keeps missing total values last and never silently substitutes main values', () => {
		const element = root(renderToStaticMarkup(<FundFlowView items={[flow('只有主力', ['main_net_inflow'], 0, 900_000_000), flow('有效总净额', ['net_inflow'], -20_000_000)]} dimension="industry" meta={meta(['net_inflow', 'main_net_inflow'])} />));
		const rows = element.querySelectorAll('.flow.sector article');
		expect(rows[0].textContent).toContain('有效总净额');
		expect(rows[1].textContent).toContain('只有主力');
		expect(element.textContent).not.toContain('9.00亿');
		expect(element.textContent).toContain('榜首总净流入');
		expect(element.textContent).toContain('有效 1 项');
	});

	it('names eastmoney main-only sector flow and masks its ratio independently', () => {
		const element = root(renderToStaticMarkup(<FundFlowView items={[flow('东财行业', ['main_net_inflow'], 0, 123_000_000)]} dimension="industry" meta={{ ...meta(['main_net_inflow', 'main_net_inflow_ratio']), source: 'eastmoney:bkzj' }} />));
		expect(element.textContent).toContain('榜首主力净流入');
		expect(element.textContent).toContain('主力净流入率');
		expect(element.textContent).not.toContain('0.00%');
		expect(element.querySelector('.flow.sector article')?.textContent).toContain('1.23亿');
	});

	it('masks stock cells per row and does not report absent zero funds in summaries', () => {
		const masked = flow('缺资金个股', [], 990_000_000, 880_000_000); masked.dimension = 'stock';
		const element = root(renderToStaticMarkup(<FundFlowView items={[masked]} dimension="stock" meta={meta(['net_inflow', 'main_net_inflow'])} />));
		expect(element.textContent).not.toContain('9.90亿');
		expect(element.textContent).not.toContain('8.80亿');
		expect(element.textContent).toContain('当前口径未提供');
		expect(element.textContent).toContain('有效 0 项');
	});
});
