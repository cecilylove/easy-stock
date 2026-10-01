// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { AuctionTrace, KLine } from '../lib/backend';
import { StockIntradayChart } from './StockIntradayChart';

const auction: AuctionTrace = {
	symbol: '600519.SH', trade_date: '2026-09-30',
	points: [{ time: '2026-09-30T09:15:00+08:00', price: 10 }, { time: '2026-09-30T09:25:00+08:00', price: 10.2 }],
	meta: { source: 'eastmoney:pre-open', fetched_at: '2026-09-30T09:25:00+08:00', latency_ms: 5, stale: false },
};

const line = (time: string, close: number) => ({ time, close, open: close, high: close, low: close, volume: 2 }) as KLine;
const minuteLine = (minute: number): KLine => ({ symbol: '600519.SH', time: `2026-09-30T${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}:00+08:00`, open: 10, high: 10, low: 10, close: 10, volume: 100, amount: 1000, meta: { source: 'sina', fields_known: true, available_fields: ['open', 'high', 'low', 'close', 'volume', 'amount'], volume_unit: 'shares', amount_currency: 'CNY', fetched_at: '2026-10-01T10:00:00+08:00', stale: false, latency_ms: 0 } });
const render = (lines: KLine[]) => renderToStaticMarkup(<StockIntradayChart symbol="600519.SH" tradeDay="2026-09-30" lines={lines} showAuction={false} />);
function elements(html: string, selector: string) { const host = document.createElement('div'); host.innerHTML = html; return [...host.querySelectorAll(selector)]; }
const paths = (html: string, className: string) => [...html.matchAll(new RegExp(`<path d="([^"]+)" class="${className}"`, 'g'))].map(match => match[1]);

describe('stock intraday fixed-axis view', () => {
	it('renders auction-only before continuous trading without fabricated previous close', () => {
		const html = renderToStaticMarkup(<StockIntradayChart symbol="600519.SH" tradeDay="2026-09-30" lines={[]} auction={auction} showAuction />);
		expect(html).toContain('09:15–09:25');
		expect(html).toContain('stock-intraday-auction-line');
		expect(html).not.toContain('class="stock-intraday-price-line"');
		expect(html).toContain('昨收暂不可用');
	});
	it('uses a verified prior close only when supplied and separates afternoon from morning', () => {
		const html = renderToStaticMarkup(<StockIntradayChart symbol="600519.SH" tradeDay="2026-09-30" lines={[
			line('2026-09-29T09:31:00+08:00', 20), line('2026-09-30T09:31:00+08:00', 10), line('2026-09-30T13:01:00+08:00', 10.4),
		]} auction={auction} showAuction previousClose={9.8} />);
		expect(html).toContain('昨收参考 9.80');
		expect((html.match(/class="stock-intraday-price-line"/g) || []).length).toBe(2);
		expect(html).not.toContain('2026-09-29 固定');
	});
	it('does not draw an auction path when the switch is off', () => {
		const html = renderToStaticMarkup(<StockIntradayChart symbol="600519.SH" tradeDay="2026-09-30" lines={[line('2026-09-30T09:31:00+08:00', 10)]} auction={auction} showAuction={false} />);
		expect(html).not.toContain('stock-intraday-auction-line');
		expect(html).not.toContain('09:15–09:25');
	});
	it('labels a truncated afternoon weighted average as a sample and retains blank earlier hours', () => {
		const html = render([815, 816, 817].map(minuteLine));
		expect(html).toContain('样本成交均价（从13:35起，非全日均价）');
		expect(html).not.toContain('黄线：当日成交均价');
		const price = paths(html, 'stock-intraday-price-line')[0];
		expect(Number(price.match(/^M ([\d.]+)/)![1])).toBeGreaterThan(550);
		expect(html).toContain('09:30'); expect(html).toContain('15:00');
	});
	it('breaks both price and average paths across missing minutes without fabricating a bridge', () => {
		const html = render([571, 573, 574].map(minuteLine));
		for (const className of ['stock-intraday-price-line', 'stock-intraday-average-line']) {
			const path = paths(html, className)[0];
			expect(path.match(/M /g)).toHaveLength(2);
			expect(path.match(/L /g)).toHaveLength(1);
		}
		expect(html).toContain('样本成交均价（从09:31起，非全日均价）');
	});
	it.each(['calculated', 'native'] as const)('shows every isolated real price and %s average without joining missing minutes', kind => {
		const samples = [571, 573, 575].map(minute => {
			const sample = minuteLine(minute);
			return kind === 'native' ? { ...sample, amount: 0, average_price: 9.8, meta: { ...sample.meta, basis_id: 'archive:raw', available_fields: ['close', 'volume', 'average_price'] } } : sample;
		});
		const html = render(samples);
		const prices = elements(html, '.stock-intraday-price-dot');
		const averages = elements(html, '.stock-intraday-average-dot');
		expect(prices).toHaveLength(3); expect(averages).toHaveLength(3);
		expect(new Set(prices.map(dot => dot.getAttribute('cx'))).size).toBe(3);
		expect(prices.map(dot => dot.getAttribute('cx'))).toEqual(averages.map(dot => dot.getAttribute('cx')));
		expect(elements(html, '.stock-intraday-last-dot')).toHaveLength(1);
		for (const className of ['stock-intraday-price-line', 'stock-intraday-average-line']) {
			expect(paths(html, className)[0].match(/M /g)).toHaveLength(3);
			expect(paths(html, className)[0]).not.toContain('L ');
		}
	});
	it('keeps price singletons visible without manufacturing unavailable average markers', () => {
		const samples = [571, 573, 575].map(minute => ({ ...minuteLine(minute), amount: 0, meta: { ...minuteLine(minute).meta, available_fields: ['close', 'volume'] } }));
		const html = render(samples);
		expect(elements(html, '.stock-intraday-price-dot')).toHaveLength(3);
		expect(elements(html, '.stock-intraday-average-dot')).toHaveLength(0);
		expect(paths(html, 'stock-intraday-average-line')).toHaveLength(0);
	});
	it('marks an isolated valid calculated average surrounded by null averages', () => {
		const samples = [571, 572, 573].map(minuteLine);
		samples[0].volume = 0; samples[0].amount = 0;
		samples[2].meta.available_fields = ['close', 'volume'];
		const html = render(samples);
		expect(elements(html, '.stock-intraday-price-dot')).toHaveLength(0);
		expect(elements(html, '.stock-intraday-average-dot')).toHaveLength(1);
		expect(paths(html, 'stock-intraday-average-line')[0]).not.toContain('L ');
	});
	it('does not add redundant singleton markers to continuous price/average segments', () => {
		const html = render([571, 572, 573].map(minuteLine));
		expect(elements(html, '.stock-intraday-price-dot')).toHaveLength(0);
		expect(elements(html, '.stock-intraday-average-dot')).toHaveLength(0);
		expect(elements(html, '.stock-intraday-last-dot')).toHaveLength(1);
	});
	it('renders lunch-boundary singletons above the blank lunch separator without connecting them', () => {
		const html = render([690, 780].map(minuteLine));
		expect(elements(html, '.stock-intraday-price-dot')).toHaveLength(2);
		expect(elements(html, '.stock-intraday-average-dot')).toHaveLength(2);
		expect(paths(html, 'stock-intraday-price-line')).toHaveLength(2);
		expect(paths(html, 'stock-intraday-average-line')[0]).not.toContain('L ');
		expect(html.indexOf('class="stock-intraday-lunch-gap"')).toBeLessThan(html.indexOf('class="stock-intraday-average-dot"'));
	});
	it('keeps the cumulative day average across lunch for a complete source minute grid', () => {
		const full = [...Array.from({ length: 120 }, (_, i) => 571 + i), ...Array.from({ length: 117 }, (_, i) => 781 + i), 900];
		const html = render(full.map(minuteLine));
		expect(html).toContain('黄线：当日成交均价（截至已覆盖分钟，量额加权）');
		expect(html).not.toContain('非全日均价');
		// Lunch and the two source-omitted closing labels remain visual breaks.
		expect(paths(html, 'stock-intraday-average-line')[0].match(/M /g)).toHaveLength(3);
		expect(paths(html, 'stock-intraday-price-line')).toHaveLength(2);
	});
	it('hides the quantity bar and weighted average when volume is explicitly absent', () => {
		const sample = minuteLine(571); sample.meta.available_fields = ['open', 'high', 'low', 'close', 'amount'];
		const html = render([sample]);
		expect(html).not.toContain('class="stock-intraday-volume"');
		expect(html).not.toContain('class="stock-intraday-average-line"');
		expect(html).toContain('来源量额不足');
	});
	it('uses verified native day averages even when a truncated archive has no amount or OHLC', () => {
		const archive = [815, 816].map(minute => ({ ...minuteLine(minute), open: 0, high: 0, low: 0, amount: 0, average_price: 9.8, meta: { ...minuteLine(minute).meta, source: 'sina:historical-archive', basis_id: 'sina:archive:raw', available_fields: ['close', 'volume', 'average_price'] } }));
		const html = render(archive);
		expect(html).toContain('黄线：来源当日成交均价');
		expect(html).not.toContain('样本成交均价');
		expect(paths(html, 'stock-intraday-average-line')).toHaveLength(1);
	});
	it.each(['mask', 'zero', 'nonfinite'])('rejects a native average when any point has invalid %s and falls back to verified sample amounts', reason => {
		const samples = [571, 573].map(minute => ({ ...minuteLine(minute), average_price: 9.8, meta: { ...minuteLine(minute).meta, basis_id: 'raw', available_fields: [...minuteLine(minute).meta.available_fields!, 'average_price'] } }));
		if (reason === 'mask') samples[1].meta.available_fields = minuteLine(573).meta.available_fields!;
		if (reason === 'zero') samples[1].average_price = 0;
		if (reason === 'nonfinite') samples[1].average_price = Infinity;
		const html = render(samples);
		expect(html).not.toContain('黄线：来源当日成交均价');
		expect(html).toContain('样本成交均价（从09:31起，非全日均价）');
		expect(paths(html, 'stock-intraday-average-line')).toHaveLength(1);
	});
	it('does not merge either native averages or amount-derived averages across different price bases', () => {
		const samples = [571, 572].map(minute => ({ ...minuteLine(minute), average_price: 9.8, meta: { ...minuteLine(minute).meta, basis_id: minute === 571 ? 'raw' : 'qfq', available_fields: ['close', 'volume', 'amount', 'average_price'] } }));
		const html = render(samples);
		expect(paths(html, 'stock-intraday-average-line')).toHaveLength(0);
		expect(elements(html, '.stock-intraday-average-dot')).toHaveLength(0);
		expect(html).not.toContain('黄线：来源当日成交均价');
		expect(html).toContain('价格口径不一致');
	});
});

describe('minute hover field masks and units', () => {
	let host: HTMLDivElement, root: Root;
	beforeEach(() => {
		vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
		vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} });
		host = document.createElement('div'); document.body.append(host); root = createRoot(host);
	});
	afterEach(() => { act(() => root.unmount()); host.remove(); vi.unstubAllGlobals(); });
	function inspect(sample: KLine) {
		act(() => root.render(<StockIntradayChart symbol="600519.SH" tradeDay="2026-09-30" lines={[sample]} showAuction={false} />));
		const svg = host.querySelector('svg')!;
		svg.getBoundingClientRect = () => ({ x: 0, y: 0, left: 0, top: 0, width: 960, height: 400, right: 960, bottom: 400, toJSON() {} });
		svg.getScreenCTM = () => ({ a: 1, b: 0, inverse: () => ({}) }) as DOMMatrix;
		svg.createSVGPoint = () => ({ x: 0, y: 0, matrixTransform() { return { x: this.x, y: this.y }; } }) as DOMPoint;
		act(() => svg.dispatchEvent(new MouseEvent('mousemove', { clientX: 68 + 808 / 240, clientY: 150, bubbles: true })));
		return host.querySelector('.stock-intraday-hover-card')!;
	}
	it('preserves true zero values while masking absent amount/volume and prior-close fields', () => {
		const sample = { ...minuteLine(571), previous_close: 9, volume: 0, amount: 0 };
		let card = inspect(sample);
		expect(card.textContent).toContain('成交量0 手'); expect(card.textContent).toContain('成交额0 元');
		expect(host.textContent).toContain('昨收暂不可用');
		card = inspect({ ...sample, volume: 1234, amount: 12340, meta: { ...sample.meta, available_fields: ['open', 'high', 'low', 'close'] } });
		expect(card.textContent).toContain('成交量—'); expect(card.textContent).toContain('成交额—');
	});
	it.each([['shares', 123400], ['lots', 1234]])('normalizes %s volumes into the same displayed hands', (unit, volume) => {
		const sample = minuteLine(571);
		const card = inspect({ ...sample, volume, meta: { ...sample.meta, volume_unit: unit } });
		expect(card.textContent).toContain('成交量1,234 手');
	});
	it('identifies a masked native average in the tooltip without inventing a missing amount', () => {
		const sample = minuteLine(571);
		const card = inspect({ ...sample, amount: 0, average_price: 9.87, meta: { ...sample.meta, available_fields: ['close', 'volume', 'average_price'] } });
		expect(card.textContent).toContain('来源当日均价9.87'); expect(card.textContent).toContain('成交额—');
	});
});
