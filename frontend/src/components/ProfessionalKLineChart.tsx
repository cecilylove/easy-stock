import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import type { KLine } from '../lib/backend';
import { calculateIndicators, resolveChartWindow } from '../lib/technical-indicators';
import { useChartViewport } from '../lib/use-chart-viewport';
import { useChartBaseline, useExpandingChartValue } from '../lib/use-stable-chart-scale';
import { sourceFieldAvailable, sourceVolumeInShares, priceBasis } from '../lib/source-fields';

type Props = { lines: KLine[]; symbol: string; periodLabel: string; state: 'idle' | 'loading' | 'ready' | 'error' };
type Indicator = 'MACD' | 'KDJ' | '无';
const colors = ['#d3a72b', '#be79d6', '#3e9ee7', '#46a992'];
const priceText = (value?: number | null) => value != null && Number.isFinite(value) ? value.toFixed(2) : '--';
const dateText = (time: string, label: string) => label.endsWith('分') ? new Date(time).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }) : new Date(time).toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', ...(label === '年K' ? {} : { month: '2-digit' }), ...(label === '年K' || label === '月K' ? {} : { day: '2-digit' }) });
const amountText = (value: number) => value >= 1e8 ? `${(value / 1e8).toFixed(2)}亿` : value >= 1e4 ? `${(value / 1e4).toFixed(1)}万` : value.toFixed(0);

export function ProfessionalKLineChart({ lines, symbol, periodLabel, state }: Props) {
	const ordered = useMemo(() => {
		const byTime = new Map<number, KLine>();
		for (const line of lines) {
			const time = Date.parse(line.time);
			if (!Number.isFinite(time) || ![line.open, line.high, line.low, line.close, line.volume].every(Number.isFinite)
				|| line.high < Math.max(line.open, line.close)
				|| line.low > Math.min(line.open, line.close) || line.volume < 0) continue;
			byTime.set(time, line);
		}
		return [...byTime.values()].sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
	}, [lines]);
	const indicators = useMemo(() => calculateIndicators(ordered), [ordered]);
	const [view, setView] = useState<{ count: number; anchor: string | null }>({ count: 80, anchor: null });
	const [selectedTime, setSelectedTime] = useState<string | null>(null);
	const [indicator, setIndicator] = useState<Indicator>('MACD');
	const [showMA, setShowMA] = useState(true);
	const drag = useRef<{ x: number; end: number; width: number } | null>(null);
	const { containerRef, width, scrollable } = useChartViewport(ordered.length > 0);
	useEffect(() => {
		const element = containerRef.current;
		if (!element) return;
		const wheel = (event: WheelEvent) => {
			if (event.ctrlKey || event.metaKey) return;
			event.preventDefault();
			setView(current => ({ ...current, count: Math.max(10, Math.min(400, Math.round(current.count * (event.deltaY > 0 ? 1.25 : .8)))) }));
		};
		element.addEventListener('wheel', wheel, { passive: false });
		return () => element.removeEventListener('wheel', wheel);
	}, [ordered.length > 0, containerRef]);
	const window = resolveChartWindow(ordered, view.count, view.anchor);
	const visible = ordered.slice(window.start, window.end + 1);
	const selection = selectedTime ? ordered.findIndex(line => line.time === selectedTime) : -1;
	const selectedIndex = selection >= window.start && selection <= window.end ? selection : window.end;
	const selected = ordered[selectedIndex];
	const previousLine = ordered[selectedIndex - 1];
	const validPreviousClose = previousLine && sourceFieldAvailable(previousLine.meta, 'close') && Number.isFinite(previousLine.close) && previousLine.close > 0 ? previousLine.close : undefined;
	const previous = previousLine ? validPreviousClose : (selected && sourceFieldAvailable(selected.meta, 'previous_close') && Number.isFinite(selected.previous_close) && selected.previous_close! > 0 ? selected.previous_close : undefined);
	// Legal zero/negative adjusted prices remain drawable, but are not a valid
	// percentage-return denominator. A supplier percentage needs its own mask.
	const computedChange = selected && sourceFieldAvailable(selected.meta, 'close') && selected.close > 0 && previous !== undefined ? (selected.close / previous - 1) * 100 : undefined;
	const change = Number.isFinite(computedChange) ? computedChange : selected && sourceFieldAvailable(selected.meta, 'change_percent') && Number.isFinite(selected.change_percent) ? selected.change_percent : undefined;
	const [viewportHeight, setViewportHeight] = useState(() => typeof globalThis.innerHeight === 'number' ? globalThis.innerHeight : 943);
	useEffect(() => { const resize = () => setViewportHeight(globalThis.innerHeight); globalThis.addEventListener('resize', resize); return () => globalThis.removeEventListener('resize', resize); }, []);
	const height = Math.max(390, Math.min(680, viewportHeight - 450));
	const left = 64, right = 70, top = 28;
	const priceBottom = height * (indicator === '无' ? .71 : .56), volumeTop = priceBottom + 27, volumeBottom = height * (indicator === '无' ? .91 : .73), subTop = volumeBottom + 38, subBottom = height - 12;
	const capacity = useExpandingChartValue(JSON.stringify([symbol, periodLabel, view.count]), Math.min(view.count, ordered.length), 20, 20, 1.08);
	const plotWidth = width - left - right, slots = Math.min(view.count, capacity) + 3, step = plotWidth / slots;
	const bodyWidth = Math.max(1, Math.min(16, step * .64));
	const context = JSON.stringify([symbol, periodLabel, view.count, view.anchor, priceBasis(ordered.at(-1)?.meta)]);
	const baseline = useChartBaseline(context, Math.max(Math.abs(visible[0]?.close || 1), .01), visible.length > 0);
	const maPrices = showMA ? indicators.ma.flatMap(series => series.values.slice(window.start, window.end + 1).filter((value): value is number => value != null)) : [];
	const lowRequired = Math.max(...visible.map(line => (1 - line.low / baseline) * 100), ...maPrices.map(value => (1 - value / baseline) * 100), 0);
	const highRequired = Math.max(...visible.map(line => (line.high / baseline - 1) * 100), ...maPrices.map(value => (value / baseline - 1) * 100), 0);
	const below = useExpandingChartValue(context, lowRequired, .5, .5, 1.08);
	const above = useExpandingChartValue(context, highRequired, .5, .5, 1.08);
	const minimum = baseline * (1 - below / 100), maximum = baseline * (1 + above / 100);
	const volumeMax = useExpandingChartValue(context, Math.max(...visible.map(line => line.volume), 1), 1, 1, 1.15);
	const x = (index: number) => left + (index - window.start + .5) * step;
	const y = (price: number) => top + (maximum - price) / (maximum - minimum) * (priceBottom - top);
	const volY = (volume: number) => volumeBottom - volume / volumeMax * (volumeBottom - volumeTop);
	const ticks = Array.from({ length: 5 }, (_, index) => minimum + (maximum - minimum) * index / 4);
	const subValues = indicator === 'MACD' ? indicators.macd.slice(window.start, window.end + 1).flatMap(value => [value.dif, value.dea, value.histogram]) : indicators.kdj.slice(window.start, window.end + 1).flatMap(value => value ? [value.k, value.d, value.j] : []);
	const subScale = `${context}:${indicator}`;
	const subBelow = useExpandingChartValue(subScale, Math.max(...subValues.map(value => -value), 0), .01, .01, 1.1);
	const subAbove = useExpandingChartValue(subScale, Math.max(...subValues, indicator === 'KDJ' ? 100 : .01), .01, .01, 1.1);
	const subLow = -subBelow, subHigh = subAbove;
	const subY = (value: number) => subTop + (subHigh - value) / Math.max(subHigh - subLow, .01) * (subBottom - subTop);
	const path = (values: Array<number | null>, valueY: (value: number) => number) => {
		let pending = true;
		return values.slice(window.start, window.end + 1).map((value, index) => {
			if (value == null || !Number.isFinite(value)) { pending = true; return ''; }
			const command = pending ? 'M' : 'L'; pending = false;
			return `${command} ${x(index + window.start).toFixed(2)} ${valueY(value).toFixed(2)}`;
		}).join(' ');
	};
	const zoom = (factor: number) => setView(current => ({ ...current, count: Math.max(10, Math.min(400, Math.round(current.count * factor))) }));
	const pan = (bars: number) => setView(current => {
		const currentWindow = resolveChartWindow(ordered, current.count, current.anchor);
		const end = Math.max(Math.min(ordered.length - 1, current.count - 1), Math.min(ordered.length - 1, currentWindow.end + bars));
		return { ...current, anchor: end >= ordered.length - 1 ? null : ordered[end]?.time || null };
	});
	const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
		if (event.key === 'ArrowUp' || event.key === '+') { event.preventDefault(); zoom(.8); }
		if (event.key === 'ArrowDown' || event.key === '-') { event.preventDefault(); zoom(1.25); }
		if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
			event.preventDefault();
			const direction = event.key === 'ArrowLeft' ? -1 : 1;
			const index = Math.max(0, Math.min(ordered.length - 1, (selection >= 0 ? selection : window.end) + direction));
			setSelectedTime(ordered[index]?.time || null);
			if (index < window.start || index > window.end) pan(direction);
		}
		if (event.key === 'Escape') setSelectedTime(null);
		if (event.key === 'End') { event.preventDefault(); setView(current => ({ ...current, anchor: null })); setSelectedTime(null); }
	};
	const pointerIndex = (event: PointerEvent<SVGRectElement>) => {
		const box = event.currentTarget.getBoundingClientRect();
		return window.start + Math.floor((event.clientX - box.left) / Math.max(box.width, 1) * slots);
	};
	const pointerMove = (event: PointerEvent<SVGRectElement>) => {
		if (drag.current) {
			const delta = Math.round((drag.current.x - event.clientX) / drag.current.width * slots);
			const end = Math.max(Math.min(ordered.length - 1, view.count - 1), Math.min(ordered.length - 1, drag.current.end + delta));
			setView(current => ({ ...current, anchor: end >= ordered.length - 1 ? null : ordered[end]?.time || null }));
			return;
		}
		const index = pointerIndex(event);
		setSelectedTime(index >= window.start && index <= window.end ? ordered[index].time : null);
	};
	const hasData = visible.length > 0;
	return <div className="professional-kline" tabIndex={0} role="region" aria-label={`${symbol} ${periodLabel}交互图表`} onKeyDown={onKeyDown}>
		<div className="chart-tools">
			<div className="chart-indicator-tabs" role="group" aria-label="副图指标">{(['MACD', 'KDJ', '无'] as const).map(name => <button type="button" key={name} className={indicator === name ? 'active' : ''} aria-pressed={indicator === name} onClick={() => setIndicator(name)}>{name === '无' ? '仅量价' : name}</button>)}</div>
			<label><input type="checkbox" checked={showMA} onChange={event => setShowMA(event.target.checked)} />MA 5/10/20/60</label>
			<div className="chart-navigation"><button type="button" aria-label="向前查看历史K线" disabled={!hasData || window.start === 0} onClick={() => pan(-Math.max(1, Math.floor(view.count / 4)))}>←</button><button type="button" aria-label="缩小K线" disabled={!hasData || view.count >= 400} onClick={() => zoom(1.25)}>−</button><span>{view.count} 柱</span><button type="button" aria-label="放大K线" disabled={!hasData || view.count <= 10} onClick={() => zoom(.8)}>＋</button><button type="button" aria-label="向后查看K线" disabled={!hasData || view.anchor == null} onClick={() => pan(Math.max(1, Math.floor(view.count / 4)))}>→</button><button type="button" disabled={!hasData} onClick={() => { setView({ count: 80, anchor: null }); setSelectedTime(null); }}>回到最新</button></div>
		</div>
		<div className="chart-value-strip" aria-live="off"><strong>{selected ? dateText(selected.time, periodLabel) : periodLabel}</strong><span>开 {priceText(selected?.open)}</span><span>高 {priceText(selected?.high)}</span><span>低 {priceText(selected?.low)}</span><span>收 {priceText(selected?.close)}</span><span className={change == null ? '' : change >= 0 ? 'up' : 'down'}>涨幅 {change != null ? `${change.toFixed(2)}%` : '--'}</span><span>量 {selected ? amountText(sourceVolumeInShares(selected.volume, selected.meta) == null ? selected.volume : sourceVolumeInShares(selected.volume, selected.meta)! / 100) : '--'}（{selected && sourceVolumeInShares(selected.volume, selected.meta) != null ? '手' : '来源单位'}）</span></div>
		{showMA && <div className="chart-ma-values">{indicators.ma.map((series, index) => <span key={series.period} style={{ color: colors[index] }}>MA{series.period}: {priceText(series.values[selectedIndex])}</span>)}</div>}
		{!hasData ? <div className="kline-chart-placeholder">{state === 'loading' ? `正在加载${periodLabel}数据…` : state === 'error' ? `${periodLabel}数据暂不可用，请稍后重试。` : `暂无${periodLabel}数据。`}</div> : <>
		{scrollable && <p className="stock-detail-chart-scroll-hint">左右滑动图表容器；触摸拖动查看历史。</p>}
		<div className="kline-plot-scroll" ref={containerRef}><svg className="kline-chart professional-chart-svg" style={{ height }} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${periodLabel}价格、均线、成交量和${indicator}图`}>
			{ticks.map((price, index) => <g key={index}><line className="kline-grid" x1={left} x2={width - right} y1={y(price)} y2={y(price)} /><text className="kline-axis-label" x={width - right + 8} y={y(price) + 4}>{price.toFixed(2)}</text></g>)}
			{[0, 1, 2, 3, 4].map(index => { const position = window.start + Math.floor(Math.max(0, visible.length - 1) * index / 4); return <g key={index}><line className="kline-grid" x1={x(position)} x2={x(position)} y1={top} y2={subBottom} /><text className="kline-date-label" x={x(position)} y={volumeBottom + 18} textAnchor={index === 0 ? 'start' : 'middle'}>{dateText(ordered[position].time, periodLabel)}</text></g>; })}
			{visible.map((line, index) => <g key={line.time} className={line.close >= line.open ? 'kline-up' : 'kline-down'}><line className="kline-wick" x1={x(index + window.start)} x2={x(index + window.start)} y1={y(line.high)} y2={y(line.low)} /><rect className="kline-body" x={x(index + window.start) - bodyWidth / 2} y={y(Math.max(line.open, line.close))} width={bodyWidth} height={Math.max(1, Math.abs(y(line.open) - y(line.close)))} /><rect className="kline-volume" x={x(index + window.start) - bodyWidth / 2} y={volY(line.volume)} width={bodyWidth} height={volumeBottom - volY(line.volume)} /></g>)}
			{showMA && indicators.ma.map((series, index) => <path key={series.period} className="professional-indicator-line" stroke={colors[index]} d={path(series.values, y)} />)}
			<line className="kline-divider" x1={left} x2={width - right} y1={volumeTop - 15} y2={volumeTop - 15} /><text x={left} y={volumeTop - 4} className="kline-axis-label">VOL · MA5/10</text>
			{indicators.volumeMA.map((series, index) => <path key={series.period} className="professional-indicator-line" stroke={colors[index]} d={path(series.values, volY)} />)}
			{indicator !== '无' && <><line className="kline-divider" x1={left} x2={width - right} y1={subTop - 15} y2={subTop - 15} /><text x={left} y={subTop - 4} className="kline-axis-label">{indicator === 'MACD' ? 'MACD (12,26,9)' : 'KDJ (9,3,3)'}</text><line className="kline-grid" x1={left} x2={width - right} y1={subY(0)} y2={subY(0)} />
			{indicator === 'MACD' ? <>{indicators.macd.slice(window.start, window.end + 1).map((value, index) => <rect key={ordered[window.start + index].time} className={value.histogram >= 0 ? 'professional-macd-up' : 'professional-macd-down'} x={x(index + window.start) - bodyWidth / 2} y={Math.min(subY(value.histogram), subY(0))} width={bodyWidth} height={Math.max(1, Math.abs(subY(value.histogram) - subY(0)))} />)}{(['dif', 'dea'] as const).map((key, index) => <path key={key} className="professional-indicator-line" stroke={colors[index]} d={path(indicators.macd.map(value => value[key]), subY)} />)}</> : (['k', 'd', 'j'] as const).map((key, index) => <path key={key} className="professional-indicator-line" stroke={colors[index]} d={path(indicators.kdj.map(value => value?.[key] ?? null), subY)} />)}
			<text className="kline-axis-label" x={width - right + 8} y={subTop + 4}>{subHigh.toFixed(2)}</text><text className="kline-axis-label" x={width - right + 8} y={subBottom}>{subLow.toFixed(2)}</text></>}
			{selection >= window.start && selection <= window.end && <g className="professional-crosshair"><line x1={x(selection)} x2={x(selection)} y1={top} y2={indicator === '无' ? volumeBottom : subBottom} /><line x1={left} x2={width - right} y1={y(ordered[selection].close)} y2={y(ordered[selection].close)} /><text x={width - right + 8} y={y(ordered[selection].close) - 4}>{priceText(ordered[selection].close)}</text></g>}
			<rect className="kline-hover-layer" x={left} y={top} width={plotWidth} height={(indicator === '无' ? volumeBottom : subBottom) - top} onPointerMove={pointerMove} onPointerLeave={() => { if (!drag.current) setSelectedTime(null); }} onPointerDown={event => { if (event.button !== 0) return; (event.currentTarget.closest('.professional-kline') as HTMLElement)?.focus(); event.currentTarget.setPointerCapture(event.pointerId); drag.current = { x: event.clientX, end: window.end, width: event.currentTarget.getBoundingClientRect().width }; }} onPointerUp={event => { drag.current = null; if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }} onPointerCancel={() => { drag.current = null; }} aria-label="拖动平移，滚轮缩放，方向键查看行情" />
		</svg></div></>}
		<div className="professional-indicator-values">{indicator === 'MACD' ? <>DIF {priceText(indicators.macd[selectedIndex]?.dif)}　DEA {priceText(indicators.macd[selectedIndex]?.dea)}　MACD {priceText(indicators.macd[selectedIndex]?.histogram)}</> : indicator === 'KDJ' ? <>K {priceText(indicators.kdj[selectedIndex]?.k)}　D {priceText(indicators.kdj[selectedIndex]?.d)}　J {priceText(indicators.kdj[selectedIndex]?.j)}</> : '成交量与价格'}<span>基于已加载样本计算，不是交易信号</span></div>
		<p className="chart-keyboard-hint">拖动平移 · 滚轮或 ↑↓ 缩放 · ←→ 十字光标 · End 回到最新 · Esc 取消光标；F5 分时/K线 · F8 周期；历史范围仅限已加载数据。</p>
	</div>;
}
