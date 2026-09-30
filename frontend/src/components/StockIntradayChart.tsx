import { type MouseEvent, type TouchEvent, useState } from 'react';
import type { AuctionTrace, KLine } from '../lib/backend';
import { auctionFraction, shanghaiDayAndMinute, tradingFraction, tradingSessionForDay } from '../lib/stock-intraday';
import { useChartViewport } from '../lib/use-chart-viewport';

type Props = {
	lines: KLine[];
	auction?: AuctionTrace | null;
	showAuction: boolean;
	symbol: string;
	tradeDay: string;
	previousClose?: number;
};

export function StockIntradayChart({ lines, auction, showAuction, symbol, tradeDay, previousClose }: Props) {
	const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
	const [hoveredAuctionIndex, setHoveredAuctionIndex] = useState<number | null>(null);
	const { containerRef, width, scrollable } = useChartViewport();
	const session = tradingSessionForDay(lines, tradeDay);
	const day = tradeDay;
	const auctionPoints = showAuction && auction?.trade_date === day ? (auction.points || []).map(point => ({ point, stamp: shanghaiDayAndMinute(point.time) })).filter(item => item.stamp?.day === day && auctionFraction(item.stamp.minute) != null) : [];
	const height = 400, left = 68, right = 84, top = 25, priceBottom = 300, volumeTop = 327, volumeBottom = 366;
	const chartWidth = width - left - right;
	const auctionWidth = showAuction ? chartWidth * .19 : 0;
	const gap = showAuction ? 12 : 0;
	const tradeLeft = left + auctionWidth + gap;
	const tradeWidth = chartWidth - auctionWidth - gap;
	const allPrices = [...session.map(item => item.line.close), ...auctionPoints.map(item => item.point.price)].filter(price => Number.isFinite(price) && price > 0);
	const knownPreviousClose = session[0]?.line.previous_close && session[0].line.previous_close! > 0 ? session[0].line.previous_close! : previousClose && previousClose > 0 ? previousClose : null;
	const baseline = knownPreviousClose || allPrices[0] || 1;
	const maxDeviation = Math.max(...allPrices.map(price => Math.abs((price / baseline - 1) * 100)), 0);
	const range = Math.max(0.3, Math.ceil(maxDeviation * 1.18 * 10) / 10);
	const priceY = (price: number) => top + ((range - (price / baseline - 1) * 100) / (2 * range)) * (priceBottom - top);
	const tradeX = (minute: number) => tradeLeft + (tradingFraction(minute) || 0) * tradeWidth;
	const auctionX = (minute: number) => left + (auctionFraction(minute) || 0) * auctionWidth;
	const morning = session.filter(item => item.instant.minute <= 11 * 60 + 30);
	const afternoon = session.filter(item => item.instant.minute >= 13 * 60);
	const draw = (items: typeof session) => items.map((item, index) => `${index ? 'L' : 'M'} ${tradeX(item.instant.minute).toFixed(1)} ${priceY(item.line.close).toFixed(1)}`).join(' ');
	const auctionPath = auctionPoints.map((item, index) => `${index ? 'L' : 'M'} ${auctionX(item.stamp!.minute).toFixed(1)} ${priceY(item.point.price).toFixed(1)}`).join(' ');
	const maxVolume = Math.max(...session.map(item => item.line.volume || 0), 1);
	const volumeWidth = Math.max(2, Math.min(8, tradeWidth / 240 * .65));
	const timeTicks = [{ minute: 9 * 60 + 30, label: '09:30', shift: 0 }, { minute: 11 * 60 + 30, label: '11:30', shift: -25 }, { minute: 13 * 60, label: '13:00', shift: 25 }, { minute: 15 * 60, label: '15:00', shift: 0 }];
	const selectedAuction = hoveredAuctionIndex == null ? null : auctionPoints[hoveredAuctionIndex];
	const selectedLine = hoveredIndex == null ? null : session[hoveredIndex];
	const latestLine = session.at(-1);
	const moveToPoint = (svg: SVGSVGElement, clientX: number, clientY: number) => {
		const bounds = svg.getBoundingClientRect();
		const transform = svg.getScreenCTM();
		if (!bounds.width || !bounds.height || !transform) return;
		const point = svg.createSVGPoint();
		point.x = clientX; point.y = clientY;
		const local = point.matrixTransform(transform.inverse());
		const pointerX = local.x;
		const pointerY = local.y;
		if (pointerX < left || pointerX > width - right || pointerY < top || pointerY > volumeBottom) {
			setHoveredIndex(null); setHoveredAuctionIndex(null); return;
		}
		if (showAuction && pointerX < tradeLeft - gap / 2) {
			const closest = auctionPoints.reduce((best, item, index) => !auctionPoints[best] || Math.abs(auctionX(item.stamp!.minute) - pointerX) < Math.abs(auctionX(auctionPoints[best].stamp!.minute) - pointerX) ? index : best, -1);
			setHoveredAuctionIndex(closest >= 0 ? closest : null); setHoveredIndex(null); return;
		}
		const closest = session.reduce((best, item, index) => !session[best] || Math.abs(tradeX(item.instant.minute) - pointerX) < Math.abs(tradeX(session[best].instant.minute) - pointerX) ? index : best, -1);
		// Empty future space is not today's price. Only snap near a real data point.
		const scale = 1 / Math.hypot(transform.a, transform.b);
		const minuteWidth = tradeWidth / 240;
		setHoveredIndex(closest >= 0 && Math.abs(tradeX(session[closest].instant.minute) - pointerX) <= Math.min(minuteWidth * 1.5, Math.max(minuteWidth * .5, 8 * scale)) ? closest : null);
		setHoveredAuctionIndex(null);
	};
	const onChartMove = (event: MouseEvent<SVGSVGElement>) => moveToPoint(event.currentTarget, event.clientX, event.clientY);
	const onChartTouch = (event: TouchEvent<SVGSVGElement>) => {
		const touch = event.touches[0];
		if (touch) moveToPoint(event.currentTarget, touch.clientX, touch.clientY);
	};
	const hoveredPrice = selectedLine?.line.close ?? selectedAuction?.point.price;
	const change = knownPreviousClose && hoveredPrice != null ? hoveredPrice - knownPreviousClose : null;
	const signed = (value: number, digits = 2) => `${value > 0 ? '+' : ''}${value.toFixed(digits)}`;
	return <div className="stock-intraday-wrap">
		{scrollable && <p className="stock-intraday-mobile-hint">左右滑动查看完整交易时段，触摸走势查看分钟数据。</p>}
		<div ref={containerRef} className="stock-intraday-scroll"><svg viewBox={`0 0 ${width} ${height}`} className="stock-intraday-chart" role="img" aria-label={`${symbol} ${day} 固定交易时段分时图${showAuction ? '，含集合竞价参考价' : ''}`} onMouseMove={onChartMove} onMouseLeave={() => { setHoveredIndex(null); setHoveredAuctionIndex(null); }} onTouchStart={onChartTouch} onTouchMove={onChartTouch}>
			{showAuction && <><rect x={left} y={top} width={auctionWidth} height={volumeBottom - top} className="stock-intraday-auction-zone" /><line x1={tradeLeft - gap / 2} x2={tradeLeft - gap / 2} y1={top} y2={volumeBottom} className="stock-intraday-auction-separator" /><text x={left + auctionWidth / 2} y={height - 7} textAnchor="middle" className="stock-intraday-label">09:15–09:25</text></>}
			{[-range, 0, range].map(value => <g key={value}><line x1={left} x2={width - right} y1={priceY(baseline * (1 + value / 100))} y2={priceY(baseline * (1 + value / 100))} className="stock-intraday-grid" />{knownPreviousClose && <text x={left - 9} y={priceY(baseline * (1 + value / 100)) + 4} textAnchor="end" className="stock-intraday-label">{value > 0 ? '+' : ''}{value.toFixed(1)}%</text>}</g>)}
			{[-range, 0, range].map(value => <text key={`price-${value}`} x={width - right + 6} y={priceY(baseline * (1 + value / 100)) + 4} className="stock-intraday-label">{(baseline * (1 + value / 100)).toFixed(2)}</text>)}
			{timeTicks.map(tick => <g key={tick.label}><line x1={tradeX(tick.minute)} x2={tradeX(tick.minute)} y1={top} y2={volumeBottom} className="stock-intraday-grid" /><text x={tradeX(tick.minute) + tick.shift} y={height - 7} textAnchor="middle" className="stock-intraday-label">{tick.label}</text></g>)}
			<line x1={left} x2={width - right} y1={volumeTop - 9} y2={volumeTop - 9} className="stock-intraday-grid" />
			<rect x={left} y={top} width={chartWidth} height={volumeBottom - top} className="stock-intraday-hit-zone" />
			{showAuction && auctionPath && <path d={auctionPath} className="stock-intraday-auction-line" />}
			{morning.length > 0 && <path d={draw(morning)} className="stock-intraday-price-line" />}
			{afternoon.length > 0 && <path d={draw(afternoon)} className="stock-intraday-price-line" />}
			{session.map(item => <rect key={item.line.time} x={tradeX(item.instant.minute) - volumeWidth / 2} y={volumeBottom - (item.line.volume / maxVolume) * (volumeBottom - volumeTop)} width={volumeWidth} height={(item.line.volume / maxVolume) * (volumeBottom - volumeTop)} className="stock-intraday-volume" />)}
			{latestLine && <circle cx={tradeX(latestLine.instant.minute)} cy={priceY(latestLine.line.close)} r={4} className="stock-intraday-last-dot" />}
			<rect x={tradeX(11 * 60 + 30) - 3} y={top} width={6} height={volumeBottom - top} className="stock-intraday-lunch-gap" />
			{showAuction && auctionPoints.map(item => <circle key={item.point.time} cx={auctionX(item.stamp!.minute)} cy={priceY(item.point.price)} r={3} className="stock-intraday-auction-dot"><title>{`${item.point.time} 参考价 ${item.point.price.toFixed(2)}（非成交价）`}</title></circle>)}
			{selectedLine && <g className="stock-intraday-crosshair"><line x1={tradeX(selectedLine.instant.minute)} x2={tradeX(selectedLine.instant.minute)} y1={top} y2={volumeBottom} /><circle cx={tradeX(selectedLine.instant.minute)} cy={priceY(selectedLine.line.close)} r={5} /></g>}
			{selectedAuction && <g className="stock-intraday-crosshair"><line x1={auctionX(selectedAuction.stamp!.minute)} x2={auctionX(selectedAuction.stamp!.minute)} y1={top} y2={volumeBottom} /><circle cx={auctionX(selectedAuction.stamp!.minute)} cy={priceY(selectedAuction.point.price)} r={5} /></g>}
		</svg></div>
		<div className="stock-intraday-legend"><span>{knownPreviousClose ? `昨收参考 ${baseline.toFixed(2)}` : '昨收暂不可用，涨跌幅轴已隐藏'} · 蓝线：连续交易价格 · 柱体：成交量</span>{showAuction && <span>淡色区域：竞价参考价，不代表逐分钟成交</span>}</div>
		{(selectedLine || selectedAuction) && <div className={`kline-hover-card stock-intraday-hover-card ${selectedLine && hoveredIndex != null && tradeX(selectedLine.instant.minute) > tradeLeft + tradeWidth / 2 ? 'left' : 'right'}`} role="status" aria-live="polite">
			<strong>{selectedLine ? `${tradeDay} ${selectedLine.line.time.slice(11, 16)}` : `${tradeDay} ${selectedAuction!.point.time.slice(11, 16)} · 竞价参考`}</strong>
			<div><span>{selectedLine ? '价格' : '参考价'}</span><b>{hoveredPrice!.toFixed(2)}</b></div>
			<div><span>涨跌额</span><b className={change != null ? change > 0 ? 'up' : change < 0 ? 'down' : '' : ''}>{change == null ? '—' : signed(change)}</b></div>
			<div><span>涨跌幅</span><b className={change != null ? change > 0 ? 'up' : change < 0 ? 'down' : '' : ''}>{change == null ? '—' : `${signed((change / knownPreviousClose!) * 100)}%`}</b></div>
			<div><span>成交量</span><b>{selectedLine ? selectedLine.line.volume.toLocaleString('zh-CN') : '—'}</b></div>
			<div><span>成交额</span><b>{selectedLine && Number.isFinite(selectedLine.line.amount) && selectedLine.line.amount > 0 ? selectedLine.line.amount.toLocaleString('zh-CN') : '—'}</b></div>
			{selectedAuction && <small>竞价参考价，非逐分钟成交；量额不可推算。</small>}
		</div>}
	</div>;
}
