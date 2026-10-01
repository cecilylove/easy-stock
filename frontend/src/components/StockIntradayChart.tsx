import { type MouseEvent, type TouchEvent, useState } from 'react';
import type { AuctionTrace, KLine } from '../lib/backend';
import { auctionFraction, intradaySampleCoverage, minuteText, shanghaiDayAndMinute, tradingFraction, tradingSessionForDay } from '../lib/stock-intraday';
import { useChartViewport } from '../lib/use-chart-viewport';
import { useChartBaseline, useExpandingChartValue } from '../lib/use-stable-chart-scale';
import { intradayAveragePrices } from '../lib/chart-updates';
import { priceBasis, sourceFieldAvailable, sourceVolumeInShares } from '../lib/source-fields';

type Props = {
	lines: KLine[];
	auction?: AuctionTrace | null;
	showAuction: boolean;
	symbol: string;
	tradeDay: string;
	previousClose?: number;
};

export function StockIntradayChart({ lines, auction, showAuction, symbol, tradeDay, previousClose }: Props) {
	const [hoveredPoint, setHoveredPoint] = useState<{ context: string; time: string; auction: boolean } | null>(null);
	const context = JSON.stringify([symbol, tradeDay]);
	const scaleContext = JSON.stringify([symbol, tradeDay, priceBasis(lines.at(-1)?.meta)]);
	const { containerRef, width, scrollable } = useChartViewport();
	const session = tradingSessionForDay(lines, tradeDay);
	const day = tradeDay;
	const auctionPoints = showAuction && auction?.trade_date === day ? (auction.points || []).map(point => ({ point, stamp: shanghaiDayAndMinute(point.time) })).filter(item => item.stamp?.day === day && auctionFraction(item.stamp.minute) != null) : [];
	const hoveredIndex = hoveredPoint?.context === context && !hoveredPoint.auction ? session.findIndex(item => item.line.time === hoveredPoint.time) : -1;
	const hoveredAuctionIndex = hoveredPoint?.context === context && hoveredPoint.auction ? auctionPoints.findIndex(item => item.point.time === hoveredPoint.time) : -1;
	const height = 400, left = 68, right = 84, top = 25, priceBottom = 300, volumeTop = 327, volumeBottom = 366;
	const chartWidth = width - left - right;
	const auctionWidth = showAuction ? chartWidth * .19 : 0;
	const gap = showAuction ? 12 : 0;
	const tradeLeft = left + auctionWidth + gap;
	const tradeWidth = chartWidth - auctionWidth - gap;
	const averageBasis = priceBasis(session[0]?.line.meta);
	const sameAverageBasis = session.every(({ line }) => priceBasis(line.meta) === averageBasis);
	// A native day average already includes trades preceding a truncated sample.
	// Require every returned point to explicitly verify it in the same price basis.
	const nativeAverage = session.length > 0 && sameAverageBasis && session.every(({ line }) => line.meta?.available_fields?.includes('average_price') && sourceFieldAvailable(line.meta, 'average_price') && Number.isFinite(line.average_price) && line.average_price! > 0);
	const averages = nativeAverage ? session.map(({ line }) => line.average_price!) : sameAverageBasis ? intradayAveragePrices(session.map(item => item.line)) : session.map(() => null);
	const coverage = intradaySampleCoverage(session.map(item => item.instant.minute));
	const sampleAverage = !coverage.startsAtOpen || coverage.hasGaps;
	const averageLabel = nativeAverage ? '黄线：来源当日成交均价'
		: averages.some(price => price != null)
			? (sampleAverage ? `黄线：样本成交均价（从${minuteText(coverage.first!)}起，非全日均价）` : '黄线：当日成交均价（截至已覆盖分钟，量额加权）')
			: sameAverageBasis ? '成交均价暂不可用（来源量额不足）' : '成交均价暂不可用（价格口径不一致）';
	const allPrices = [...session.map(item => item.line.close), ...averages.filter((price): price is number => price != null), ...auctionPoints.map(item => item.point.price)].filter(price => Number.isFinite(price) && price > 0);
	const firstLine = session[0]?.line;
	const knownPreviousClose = firstLine && sourceFieldAvailable(firstLine.meta, 'previous_close') && Number.isFinite(firstLine.previous_close) && firstLine.previous_close! > 0 ? firstLine.previous_close! : Number.isFinite(previousClose) && previousClose! > 0 ? previousClose! : null;
	const baseline = useChartBaseline(scaleContext, knownPreviousClose || allPrices[0] || 1, knownPreviousClose != null);
	const maxDeviation = Math.max(...allPrices.map(price => Math.abs((price / baseline - 1) * 100)), 0);
	const range = useExpandingChartValue(scaleContext, maxDeviation, .5, .5, 1.18);
	const priceY = (price: number) => top + ((range - (price / baseline - 1) * 100) / (2 * range)) * (priceBottom - top);
	const tradeX = (minute: number) => tradeLeft + (tradingFraction(minute) || 0) * tradeWidth;
	const auctionX = (minute: number) => left + (auctionFraction(minute) || 0) * auctionWidth;
	const morning = session.filter(item => item.instant.minute <= 11 * 60 + 30);
	const afternoon = session.filter(item => item.instant.minute >= 13 * 60);
	// Do not draw a price through minutes for which the source returned no point.
	// Even the source's known closing-auction jump is conservatively left open.
	const draw = (items: typeof session) => items.map((item, index) => `${index && item.instant.minute - items[index - 1].instant.minute === 1 ? 'L' : 'M'} ${tradeX(item.instant.minute).toFixed(1)} ${priceY(item.line.close).toFixed(1)}`).join(' ');
	const auctionPath = auctionPoints.map((item, index) => `${index ? 'L' : 'M'} ${auctionX(item.stamp!.minute).toFixed(1)} ${priceY(item.point.price).toFixed(1)}`).join(' ');
	const requiredVolume = Math.max(...session.filter(item => sourceFieldAvailable(item.line.meta, 'volume')).map(item => item.line.volume), 1);
	const maxVolume = useExpandingChartValue(scaleContext, requiredVolume, 1, 1, 1.15);
	const averagePath = session.map((item, index) => {
		if (averages[index] == null) return '';
		const starts = index === 0 || averages[index - 1] == null || item.instant.minute - session[index - 1].instant.minute !== 1;
		return `${starts ? 'M' : 'L'} ${tradeX(item.instant.minute).toFixed(1)} ${priceY(averages[index]!).toFixed(1)}`;
	}).filter(Boolean).join(' ');
	// A path's isolated M command has no visible stroke. Preserve gaps while
	// showing real singleton samples independently (including average-only gaps).
	const isolatedAt = (index: number, valid: (index: number) => boolean) => valid(index)
		&& !(index > 0 && valid(index - 1) && session[index].instant.minute - session[index - 1].instant.minute === 1)
		&& !(index + 1 < session.length && valid(index + 1) && session[index + 1].instant.minute - session[index].instant.minute === 1);
	const isolatedPrices = session.filter((_, index) => isolatedAt(index, () => true));
	const isolatedAverages = session.flatMap((item, index) => isolatedAt(index, position => averages[position] != null && Number.isFinite(averages[position]) && averages[position]! > 0) ? [{ item, price: averages[index]! }] : []);
	const volumeWidth = Math.max(2, Math.min(8, tradeWidth / 240 * .65));
	const timeTicks = [{ minute: 9 * 60 + 30, label: '09:30', shift: 0 }, { minute: 11 * 60 + 30, label: '11:30', shift: -25 }, { minute: 13 * 60, label: '13:00', shift: 25 }, { minute: 15 * 60, label: '15:00', shift: 0 }];
	const selectedAuction = hoveredAuctionIndex >= 0 ? auctionPoints[hoveredAuctionIndex] : null;
	const selectedLine = hoveredIndex >= 0 ? session[hoveredIndex] : null;
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
			setHoveredPoint(null); return;
		}
		if (showAuction && pointerX < tradeLeft - gap / 2) {
			const closest = auctionPoints.reduce((best, item, index) => !auctionPoints[best] || Math.abs(auctionX(item.stamp!.minute) - pointerX) < Math.abs(auctionX(auctionPoints[best].stamp!.minute) - pointerX) ? index : best, -1);
			setHoveredPoint(closest >= 0 ? { context, time: auctionPoints[closest].point.time, auction: true } : null); return;
		}
		const closest = session.reduce((best, item, index) => !session[best] || Math.abs(tradeX(item.instant.minute) - pointerX) < Math.abs(tradeX(session[best].instant.minute) - pointerX) ? index : best, -1);
		// Empty future space is not today's price. Only snap near a real data point.
		const scale = 1 / Math.hypot(transform.a, transform.b);
		const minuteWidth = tradeWidth / 240;
		setHoveredPoint(closest >= 0 && Math.abs(tradeX(session[closest].instant.minute) - pointerX) <= Math.min(minuteWidth * 1.5, Math.max(minuteWidth * .5, 8 * scale)) ? { context, time: session[closest].line.time, auction: false } : null);
	};
	const onChartMove = (event: MouseEvent<SVGSVGElement>) => moveToPoint(event.currentTarget, event.clientX, event.clientY);
	const onChartTouch = (event: TouchEvent<SVGSVGElement>) => {
		const touch = event.touches[0];
		if (touch) moveToPoint(event.currentTarget, touch.clientX, touch.clientY);
	};
	const hoveredPrice = selectedLine?.line.close ?? selectedAuction?.point.price;
	const selectedVolume = selectedLine && sourceFieldAvailable(selectedLine.line.meta, 'volume') ? sourceVolumeInShares(selectedLine.line.volume, selectedLine.line.meta) : null;
	const volumeText = selectedLine && sourceFieldAvailable(selectedLine.line.meta, 'volume') ? selectedVolume == null ? `${selectedLine.line.volume.toLocaleString('zh-CN')}（来源单位）` : `${(selectedVolume / 100).toLocaleString('zh-CN')} 手` : '—';
	const amountText = selectedLine && sourceFieldAvailable(selectedLine.line.meta, 'amount') && Number.isFinite(selectedLine.line.amount) && selectedLine.line.amount >= 0 ? `${selectedLine.line.amount.toLocaleString('zh-CN')}${selectedLine.line.meta?.amount_currency === 'CNY' ? ' 元' : ''}` : '—';
	const selectedAverage = hoveredIndex >= 0 ? averages[hoveredIndex] : null;
	const change = knownPreviousClose && hoveredPrice != null ? hoveredPrice - knownPreviousClose : null;
	const signed = (value: number, digits = 2) => `${value > 0 ? '+' : ''}${value.toFixed(digits)}`;
	return <div className="stock-intraday-wrap">
		{scrollable && <p className="stock-intraday-mobile-hint">左右滑动查看固定交易时段，触摸走势查看已有分钟数据。</p>}
		<div ref={containerRef} className="stock-intraday-scroll"><svg viewBox={`0 0 ${width} ${height}`} className="stock-intraday-chart" role="img" aria-label={`${symbol} ${day} 固定交易时段分时图${showAuction ? '，含集合竞价参考价' : ''}`} onMouseMove={onChartMove} onMouseLeave={() => setHoveredPoint(null)} onTouchStart={onChartTouch} onTouchMove={onChartTouch}>
			{showAuction && <><rect x={left} y={top} width={auctionWidth} height={volumeBottom - top} className="stock-intraday-auction-zone" /><line x1={tradeLeft - gap / 2} x2={tradeLeft - gap / 2} y1={top} y2={volumeBottom} className="stock-intraday-auction-separator" /><text x={left + auctionWidth / 2} y={height - 7} textAnchor="middle" className="stock-intraday-label">09:15–09:25</text></>}
			{[-range, -range / 2, 0, range / 2, range].map(value => <g key={value}><line x1={left} x2={width - right} y1={priceY(baseline * (1 + value / 100))} y2={priceY(baseline * (1 + value / 100))} className="stock-intraday-grid" />{knownPreviousClose && <text x={left - 9} y={priceY(baseline * (1 + value / 100)) + 4} textAnchor="end" className="stock-intraday-label">{value > 0 ? '+' : ''}{value.toFixed(1)}%</text>}</g>)}
			{[-range, -range / 2, 0, range / 2, range].map(value => <text key={`price-${value}`} x={width - right + 6} y={priceY(baseline * (1 + value / 100)) + 4} className="stock-intraday-label">{(baseline * (1 + value / 100)).toFixed(2)}</text>)}
			{timeTicks.map(tick => <g key={tick.label}><line x1={tradeX(tick.minute)} x2={tradeX(tick.minute)} y1={top} y2={volumeBottom} className="stock-intraday-grid" /><text x={tradeX(tick.minute) + tick.shift} y={height - 7} textAnchor="middle" className="stock-intraday-label">{tick.label}</text></g>)}
			<line x1={left} x2={width - right} y1={volumeTop - 9} y2={volumeTop - 9} className="stock-intraday-grid" />
			<rect x={left} y={top} width={chartWidth} height={volumeBottom - top} className="stock-intraday-hit-zone" />
			{showAuction && auctionPath && <path d={auctionPath} className="stock-intraday-auction-line" />}
			{averagePath && <path d={averagePath} className="stock-intraday-average-line" />}
			{morning.length > 0 && <path d={draw(morning)} className="stock-intraday-price-line" />}
			{afternoon.length > 0 && <path d={draw(afternoon)} className="stock-intraday-price-line" />}
			{session.filter(item => sourceFieldAvailable(item.line.meta, 'volume')).map(item => <rect key={item.line.time} x={tradeX(item.instant.minute) - volumeWidth / 2} y={volumeBottom - (item.line.volume / maxVolume) * (volumeBottom - volumeTop)} width={volumeWidth} height={(item.line.volume / maxVolume) * (volumeBottom - volumeTop)} className="stock-intraday-volume" />)}
			<rect x={tradeX(11 * 60 + 30) - 3} y={top} width={6} height={volumeBottom - top} className="stock-intraday-lunch-gap" />
			{isolatedAverages.map(({ item, price }) => <circle key={`average-${item.line.time}`} cx={tradeX(item.instant.minute)} cy={priceY(price)} r={6} className="stock-intraday-average-dot" />)}
			{isolatedPrices.filter(item => item !== latestLine).map(item => <circle key={`price-${item.line.time}`} cx={tradeX(item.instant.minute)} cy={priceY(item.line.close)} r={3} className="stock-intraday-price-dot" />)}
			{latestLine && <circle cx={tradeX(latestLine.instant.minute)} cy={priceY(latestLine.line.close)} r={4} className={`stock-intraday-last-dot${isolatedPrices.includes(latestLine) ? ' stock-intraday-price-dot' : ''}`} />}
			{showAuction && auctionPoints.map(item => <circle key={item.point.time} cx={auctionX(item.stamp!.minute)} cy={priceY(item.point.price)} r={3} className="stock-intraday-auction-dot"><title>{`${item.point.time} 参考价 ${item.point.price.toFixed(2)}（非成交价）`}</title></circle>)}
			{selectedLine && <g className="stock-intraday-crosshair"><line x1={tradeX(selectedLine.instant.minute)} x2={tradeX(selectedLine.instant.minute)} y1={top} y2={volumeBottom} /><circle cx={tradeX(selectedLine.instant.minute)} cy={priceY(selectedLine.line.close)} r={5} /></g>}
			{selectedAuction && <g className="stock-intraday-crosshair"><line x1={auctionX(selectedAuction.stamp!.minute)} x2={auctionX(selectedAuction.stamp!.minute)} y1={top} y2={volumeBottom} /><circle cx={auctionX(selectedAuction.stamp!.minute)} cy={priceY(selectedAuction.point.price)} r={5} /></g>}
		</svg></div>
		<div className="stock-intraday-legend"><span>{knownPreviousClose ? `昨收参考 ${baseline.toFixed(2)}` : '昨收暂不可用，涨跌幅轴已隐藏'} · 蓝线：已返回分钟价格，缺口处断开 · 柱体：成交量</span><span>{averageLabel}</span>{showAuction && <span>淡色区域：竞价参考价，不代表逐分钟成交</span>}</div>
		{(selectedLine || selectedAuction) && <div className={`kline-hover-card stock-intraday-hover-card ${selectedLine && hoveredIndex != null && tradeX(selectedLine.instant.minute) > tradeLeft + tradeWidth / 2 ? 'left' : 'right'}`} role="status" aria-live="polite">
			<strong>{selectedLine ? `${tradeDay} ${minuteText(selectedLine.instant.minute)}` : `${tradeDay} ${minuteText(selectedAuction!.stamp!.minute)} · 竞价参考`}</strong>
			<div><span>{selectedLine ? '价格' : '参考价'}</span><b>{hoveredPrice!.toFixed(2)}</b></div>
			{selectedLine && <div><span>{nativeAverage ? '来源当日均价' : sampleAverage ? '样本均价' : '当日均价'}</span><b>{selectedAverage == null ? '—' : selectedAverage.toFixed(2)}</b></div>}
			<div><span>涨跌额</span><b className={change != null ? change > 0 ? 'up' : change < 0 ? 'down' : '' : ''}>{change == null ? '—' : signed(change)}</b></div>
			<div><span>涨跌幅</span><b className={change != null ? change > 0 ? 'up' : change < 0 ? 'down' : '' : ''}>{change == null ? '—' : `${signed((change / knownPreviousClose!) * 100)}%`}</b></div>
			<div><span>成交量</span><b>{volumeText}</b></div>
			<div><span>成交额</span><b>{amountText}</b></div>
			{selectedAuction && <small>竞价参考价，非逐分钟成交；量额不可推算。</small>}
		</div>}
	</div>;
}
