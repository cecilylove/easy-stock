// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { KLine } from '../lib/backend';
import { ProfessionalKLineChart } from './ProfessionalKLineChart';

const lines: KLine[] = Array.from({ length: 160 }, (_, index) => ({ symbol: '000002.SZ', time: new Date(Date.UTC(2026, 0, index + 1)).toISOString(), open: 10 + index / 100, close: 10.01 + index / 100, high: 10.2 + index / 100, low: 9.9 + index / 100, volume: 100 + index, amount: 1000, meta: { source: 'fixture', fetched_at: '2026-09-30T10:00:00+08:00', stale: false, latency_ms: 0 } }));
let host: HTMLDivElement, root: Root;
beforeEach(() => {
	vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
	vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} });
	host = document.createElement('div'); document.body.append(host); root = createRoot(host);
	act(() => root.render(<ProfessionalKLineChart lines={lines} symbol="000002.SZ" periodLabel="日K" state="ready" />));
});
afterEach(() => { act(() => root.unmount()); host.remove(); vi.unstubAllGlobals(); });
function click(name: string) { const button = [...host.querySelectorAll<HTMLButtonElement>('button')].find(item => item.getAttribute('aria-label') === name || item.textContent === name)!; act(() => button.click()); }
function key(name: string) { act(() => host.querySelector('.professional-kline')!.dispatchEvent(new KeyboardEvent('keydown', { key: name, bubbles: true }))); }
function pointer(type: string, x = 100) {
	const layer = host.querySelector<SVGRectElement>('.kline-hover-layer')!;
	layer.getBoundingClientRect = () => ({ x: 0, y: 0, left: 0, top: 0, width: 830, height: 300, right: 830, bottom: 300, toJSON() {} });
	act(() => layer.dispatchEvent(new MouseEvent(type, { clientX: x, button: 0, bubbles: true })));
}
function select(x = 100) { pointer('pointerdown', x); pointer('pointerup', x); }
describe('professional chart navigation', () => {
	it('zooms, browses history and restores the latest viewport', () => {
		expect(host.querySelectorAll('.kline-body')).toHaveLength(80);
		expect(host.innerHTML).not.toContain('NaN');
		click('放大K线'); expect(host.querySelectorAll('.kline-body')).toHaveLength(64);
		const latest = host.querySelector('.chart-value-strip strong')!.textContent;
		click('向前查看历史K线'); expect(host.querySelector('.chart-value-strip strong')!.textContent).not.toBe(latest);
		click('回到最新'); expect(host.querySelectorAll('.kline-body')).toHaveLength(80);
		expect(host.innerHTML).not.toContain('NaN');
		expect(host.querySelector('.chart-value-strip strong')!.textContent).toBe(latest);
	});
	it('links keyboard inspection to all panes and switches indicators', () => {
		key('ArrowLeft'); expect(host.querySelector('.professional-crosshair')).not.toBeNull();
		const previous = host.querySelector('.chart-value-strip strong')!.textContent;
		key('ArrowLeft'); expect(host.querySelector('.chart-value-strip strong')!.textContent).not.toBe(previous);
		key('Escape'); expect(host.querySelector('.professional-crosshair')).toBeNull();
		click('KDJ'); expect(host.textContent).toContain('KDJ (9,3,3)'); expect(host.querySelectorAll('.professional-macd-up')).toHaveLength(0);
		click('仅量价'); expect(host.textContent).not.toContain('KDJ (9,3,3)');
	});
	it('renders legal negative adjusted prices and excludes malformed candles', () => {
		const valid = [
			{ ...lines[0], open: -2, high: 1, low: -3, close: -1 },
			{ ...lines[1], open: 1, high: 4, low: 0, close: 3 },
		];
		act(() => root.render(<ProfessionalKLineChart lines={[...valid, { ...lines[2], volume: NaN }, { ...lines[3], high: Infinity }]} symbol="000002.SZ" periodLabel="年K" state="ready" />));
		expect(host.querySelectorAll('.kline-body')).toHaveLength(2);
		expect(host.innerHTML).not.toMatch(/NaN|Infinity/);
		const ticks = [...host.querySelectorAll('.professional-chart-svg .kline-axis-label')].map(item => Number(item.textContent));
		expect(ticks.some(value => value < 0)).toBe(true);
	});

	it.each([-1, 0])('draws a %s previous adjusted close but does not use it as a percentage denominator', previous => {
		const sample = [
			{ ...lines[0], open: previous, close: previous, high: previous + 1, low: previous - 1 },
			{ ...lines[1], open: 1, close: 3, high: 4, low: 0, previous_close: 2, change_percent: 0, meta: { ...lines[1].meta, fields_known: true, available_fields: ['open', 'high', 'low', 'close', 'volume', 'previous_close'] } },
		];
		act(() => root.render(<ProfessionalKLineChart lines={sample} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelectorAll('.kline-body')).toHaveLength(2);
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('涨幅 --');
		expect(host.innerHTML).not.toMatch(/NaN|Infinity/);
	});
	it('uses only finite masked supplier change without a positive previous price', () => {
		const sample = { ...lines[0], previous_close: 0, change_percent: 7.25, meta: { ...lines[0].meta, fields_known: true, available_fields: ['close', 'change_percent'] } };
		const render = (line: KLine) => act(() => root.render(<ProfessionalKLineChart lines={[line]} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		render(sample); expect(host.querySelector('.chart-value-strip')?.textContent).toContain('涨幅 7.25%');
		render({ ...sample, change_percent: NaN }); expect(host.querySelector('.chart-value-strip')?.textContent).toContain('涨幅 --');
		render({ ...sample, meta: { ...sample.meta, available_fields: ['close'] } }); expect(host.querySelector('.chart-value-strip')?.textContent).toContain('涨幅 --');
	});
	it('does not compute using a masked previous close', () => {
		const sample = [
			{ ...lines[0], meta: { ...lines[0].meta, fields_known: true, available_fields: ['volume'] } },
			{ ...lines[1], change_percent: 0, meta: { ...lines[1].meta, fields_known: true, available_fields: ['close', 'volume'] } },
		];
		act(() => root.render(<ProfessionalKLineChart lines={sample} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('涨幅 --');
	});
	it('preserves a panned viewport and SVG while appending new data', () => {
		click('向前查看历史K线');
		const svg = host.querySelector('svg'), candle = host.querySelector('.kline-body');
		const date = host.querySelector('.chart-value-strip strong')?.textContent;
		act(() => root.render(<ProfessionalKLineChart lines={[...lines, { ...lines.at(-1)!, time: '2026-10-01T15:00:00+08:00' }]} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelector('svg')).toBe(svg); expect(host.querySelector('.kline-body')).toBe(candle);
		expect(host.querySelector('.chart-value-strip strong')?.textContent).toBe(date);
	});
	it('uses compact MA5/10/20 and volume by default, with optional MA60 and subpanes', () => {
		act(() => root.render(<ProfessionalKLineChart key="compact" compact lines={lines} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelector('.professional-kline-compact')).not.toBeNull();
		expect(host.querySelector('.chart-ma-values')?.textContent).toContain('MA20:');
		expect(host.querySelector('.chart-ma-values')?.textContent).not.toContain('MA60:');
		expect(host.querySelector('.chart-ma-values')?.textContent).toContain('量MA5:');
		expect(host.querySelectorAll('.kline-volume')).toHaveLength(80);
		expect(host.querySelector('.professional-macd-up')).toBeNull();
		const toggle = [...host.querySelectorAll('label')].find(item => item.textContent === 'MA60')!.querySelector('input')!;
		act(() => toggle.click()); expect(host.querySelector('.chart-ma-values')?.textContent).toContain('MA60:');
		click('MACD'); expect(host.querySelector('.professional-macd-up')).not.toBeNull();
		click('仅量价'); expect(host.querySelector('.professional-macd-up')).toBeNull();
	});
	it('hovers dates and locks inspection across pointer leave and same-context refresh', () => {
		const latest = host.querySelector('.chart-value-strip strong')!.textContent;
		pointer('pointermove'); const hovered = host.querySelector('.chart-value-strip strong')!.textContent;
		expect(hovered).not.toBe(latest); expect(host.querySelector('.professional-crosshair')).not.toBeNull();
		pointer('pointerout'); expect(host.querySelector('.chart-value-strip strong')!.textContent).toBe(latest);
		select(); const lockedDate = host.querySelector('.chart-value-strip strong')!.textContent;
		pointer('pointerout'); pointer('pointermove', 400);
		expect(host.querySelector('.chart-value-strip strong')!.textContent).toBe(lockedDate);
		expect(host.textContent).toContain('日期已锁定');
		const svg = host.querySelector('svg');
		act(() => root.render(<ProfessionalKLineChart lines={lines.map(line => ({ ...line }))} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelector('svg')).toBe(svg); expect(host.querySelector('.chart-value-strip strong')!.textContent).toBe(lockedDate);
		click('解锁日期'); expect(host.querySelector('.professional-crosshair')).toBeNull();
		expect(host.querySelector('.chart-value-strip strong')!.textContent).toBe(latest);
	});
	it('does not lock a pan gesture or small pointer jitter until pointer release', () => {
		pointer('pointerdown', 400); pointer('pointermove', 402);
		expect(host.textContent).not.toContain('日期已锁定');
		pointer('pointerup', 402); expect(host.textContent).toContain('日期已锁定');
		click('解锁日期');
		pointer('pointerdown', 400); pointer('pointermove', 600); pointer('pointerup', 600);
		expect(host.textContent).not.toContain('日期已锁定');
		expect(host.querySelector('.professional-crosshair')).toBeNull();
	});
	it('does not lock canceled pointer gestures or open intraday after a drag', () => {
		const open = vi.fn();
		act(() => root.render(<ProfessionalKLineChart lines={lines} symbol="000002.SZ" periodLabel="日K" state="ready" onOpenIntraday={open} />));
		pointer('pointerdown'); pointer('pointercancel'); pointer('pointerup');
		expect(host.textContent).not.toContain('日期已锁定');
		pointer('pointerdown', 400); pointer('pointermove', 600); pointer('pointerup', 600); pointer('dblclick', 100);
		expect(open).not.toHaveBeenCalled();
	});
	it('fits a 290px compact container with three short, separated date ticks and a complete price axis', () => {
		vi.stubGlobal('ResizeObserver', class {
			constructor(private resize: (entries: Array<{ contentRect: { width: number } }>) => void) {}
			observe() { this.resize([{ contentRect: { width: 290 } }]); }
			disconnect() {}
		});
		act(() => root.render(<ProfessionalKLineChart key="narrow" compact lines={lines} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelector('svg')?.getAttribute('viewBox')).toBe('0 0 290 320');
		expect(host.querySelector('.stock-detail-chart-scroll-hint')).toBeNull();
		const labels = [...host.querySelectorAll<SVGTextElement>('.kline-date-label')];
		expect(labels).toHaveLength(3);
		for (const label of labels) expect(label.textContent).toMatch(/^\d{2}\/\d{2}$/);
		const positions = labels.map(label => Number(label.getAttribute('x')));
		expect(positions[1] - positions[0]).toBeGreaterThan(40);
		expect(positions[2] - positions[1]).toBeGreaterThan(40);
		expect(labels.at(-1)?.getAttribute('text-anchor')).toBe('end');
		const axis = [...host.querySelectorAll<SVGTextElement>('.kline-axis-label')].filter(label => label.getAttribute('x') === '250');
		expect(axis).toHaveLength(5);
		expect(host.querySelector('.chart-value-strip strong')?.textContent).toContain('2026');
		act(() => root.render(<ProfessionalKLineChart key="narrow" compact lines={lines.slice(0, 2)} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelectorAll('.kline-date-label')).toHaveLength(1);
	});
	it('retains full daily dates and five axis ticks in the normal chart', () => {
		const labels = [...host.querySelectorAll('.kline-date-label')];
		expect(labels).toHaveLength(5);
		for (const label of labels) expect(label.textContent).toContain('2026');
	});
	it.each(['loading', 'error'] as const)('preserves a locked chart while %s retains existing data', state => {
		select(); const date = host.querySelector('.chart-value-strip strong')!.textContent, svg = host.querySelector('svg');
		act(() => root.render(<ProfessionalKLineChart lines={lines} symbol="000002.SZ" periodLabel="日K" state={state} />));
		expect(host.querySelector('svg')).toBe(svg); expect(host.querySelector('.kline-chart-placeholder')).toBeNull();
		expect(host.querySelector('.chart-value-strip strong')!.textContent).toBe(date);
		expect(host.textContent).toContain('日期已锁定');
	});
	it.each(['symbol', 'period', 'basis'])('clears locked inspection and historic viewport when %s changes', change => {
		click('向前查看历史K线'); select(); expect(host.textContent).toContain('日期已锁定');
		const changedLines = change === 'basis' ? lines.map(line => ({ ...line, meta: { ...line.meta, basis_id: 'new-basis' } })) : lines;
		act(() => root.render(<ProfessionalKLineChart lines={changedLines} symbol={change === 'symbol' ? '600000.SH' : '000002.SZ'} periodLabel={change === 'period' ? '周K' : '日K'} state="ready" />));
		expect(host.querySelector('.professional-crosshair')).toBeNull();
		expect(host.textContent).not.toContain('日期已锁定');
		expect([...host.querySelectorAll<HTMLButtonElement>('button')].find(button => button.getAttribute('aria-label') === '向后查看K线')!.disabled).toBe(true);
	});
	it('opens the inspected Shanghai trading date from the explicit action and double click', () => {
		const open = vi.fn();
		const daily = [{ ...lines[0], time: '2026-09-29T16:30:00Z' }, { ...lines[1], time: '2026-09-30T16:30:00Z' }];
		act(() => root.render(<ProfessionalKLineChart lines={daily} symbol="000002.SZ" periodLabel="日K" state="ready" onOpenIntraday={open} />));
		click('查看当日分时'); expect(open).toHaveBeenLastCalledWith('2026-10-01');
		pointer('dblclick', 5); expect(open).toHaveBeenLastCalledWith('2026-09-30');
		expect(host.textContent).toContain('日期已锁定');
	});
	it.each(['周K', '月K', '年K', '5分'])('does not expose history intraday actions for %s', label => {
		const open = vi.fn();
		act(() => root.render(<ProfessionalKLineChart lines={lines} symbol="000002.SZ" periodLabel={label} state="ready" onOpenIntraday={open} />));
		expect(host.textContent).not.toContain('查看当日分时'); pointer('dblclick'); expect(open).not.toHaveBeenCalled();
	});
	it('retains missing-field markers and supplied volume units in the fixed inspection panel', () => {
		const sample = { ...lines[0], volume: 123400, meta: { ...lines[0].meta, volume_unit: 'shares' as const, fields_known: true, available_fields: ['close', 'volume'] } };
		act(() => root.render(<ProfessionalKLineChart lines={[sample]} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('开 --');
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('量 1234（手）');
	});
	it('distinguishes unavailable amount and turnover from valid zero and masks amplitude inputs', () => {
		const sample = { ...lines[0], previous_close: 10, high: 11, low: 9, amount: 0, turnover_rate: 0, meta: { ...lines[0].meta, fields_known: true, available_fields: ['close', 'volume', 'previous_close', 'high', 'low'] } };
		const render = (line: KLine) => act(() => root.render(<ProfessionalKLineChart lines={[line]} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		render(sample);
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('额 --');
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('换手 --');
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('振幅 20.00%');
		render({ ...sample, meta: { ...sample.meta, available_fields: ['close', 'volume', 'amount', 'turnover_rate', 'previous_close', 'high'] } });
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('额 0');
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('换手 0.00%');
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('振幅 --');
		render({ ...sample, amount: NaN, turnover_rate: Infinity, meta: { ...sample.meta, available_fields: ['amount', 'turnover_rate', 'high', 'low'] } });
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('额 --');
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('换手 --');
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('振幅 --');
	});
	it('does not compute amplitude from a previous close in a different price basis', () => {
		const sample = [
			{ ...lines[0], meta: { ...lines[0].meta, basis_id: 'qfq' } },
			{ ...lines[1], previous_close: 10, meta: { ...lines[1].meta, basis_id: 'hfq', fields_known: true, available_fields: ['high', 'low', 'close', 'previous_close'] } },
		];
		act(() => root.render(<ProfessionalKLineChart lines={sample} symbol="000002.SZ" periodLabel="日K" state="ready" />));
		expect(host.querySelector('.chart-value-strip')?.textContent).toContain('振幅 --');
	});
});
