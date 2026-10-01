import type { Quote } from '../lib/backend';

const price = (value?: number) => value != null && Number.isFinite(value) && value > 0 ? value.toFixed(2) : '--';
const compact = (value?: number) => value != null && Number.isFinite(value) && value >= 0 ? value >= 1e8 ? `${(value / 1e8).toFixed(2)}亿` : value >= 1e4 ? `${(value / 1e4).toFixed(2)}万` : value.toFixed(0) : '--';

export function StockQuoteSidebar({ quote, stale, symbol }: { quote: Quote | null; stale: boolean; symbol: string }) {
	const base = quote?.previous_close || 0;
	const tone = (value?: number) => value && base ? value > base ? 'up' : value < base ? 'down' : '' : '';
	const amount = quote?.amount, volume = quote?.volume;
	const average = amount != null && volume != null && volume > 0 ? amount / volume : undefined;
	const amplitude = base && quote?.high && quote?.low ? (quote.high - quote.low) / base * 100 : null;
	const asks = quote?.asks?.length === 5 ? quote.asks : null, bids = quote?.bids?.length === 5 ? quote.bids : null;
	const bidVolume = bids?.reduce((sum, level) => sum + level.volume, 0) ?? 0, askVolume = asks?.reduce((sum, level) => sum + level.volume, 0) ?? 0;
	const imbalance = asks && bids && bidVolume + askVolume > 0 ? (bidVolume - askVolume) / (bidVolume + askVolume) * 100 : null;
	const maxVolume = Math.max(...(asks || []).map(level => level.volume), ...(bids || []).map(level => level.volume), 1);
	const code = symbol.split('.')[0];
	const market = symbol.split('.')[1]?.toLowerCase();
	const external = market === 'sh' || market === 'sz' ? `https://quote.eastmoney.com/${market}${code}.html` : null;
	return <aside className="stock-quote-sidebar" aria-label="行情与买卖五档">
		<header><strong>买卖五档</strong><span className={stale ? 'quote-stale' : ''}>{stale ? '旧快照 · 非实时' : 'L1 报价快照'}</span></header>
		<div className="quote-orderbook"><div className="quote-orderbook-head"><span>档位</span><span>价格</span><span>委托量（手）</span></div>
		{[4, 3, 2, 1, 0].map(index => <div className="quote-book-row sell" key={`sell-${index}`}><i style={{ width: `${(asks?.[index].volume || 0) / maxVolume * 100}%` }} /><span>卖{['一', '二', '三', '四', '五'][index]}</span><b className={tone(asks?.[index].price)}>{price(asks?.[index].price)}</b><span>{asks ? compact(asks[index].volume / 100) : '--'}</span></div>)}
		<div className="quote-book-price"><strong className={tone(quote?.price)}>{price(quote?.price)}</strong><span className={tone(quote?.price)}>{quote?.change_percent != null ? `${quote.change_percent > 0 ? '+' : ''}${quote.change_percent.toFixed(2)}%` : '--'}</span></div>
		{[0, 1, 2, 3, 4].map(index => <div className="quote-book-row buy" key={`buy-${index}`}><i style={{ width: `${(bids?.[index].volume || 0) / maxVolume * 100}%` }} /><span>买{['一', '二', '三', '四', '五'][index]}</span><b className={tone(bids?.[index].price)}>{price(bids?.[index].price)}</b><span>{bids ? compact(bids[index].volume / 100) : '--'}</span></div>)}
		</div>
		<div className="quote-book-summary"><span>委比 <b>{imbalance == null ? '--' : `${imbalance.toFixed(2)}%`}</b></span><span>委差 <b>{asks && bids ? compact(Math.abs(bidVolume - askVolume) / 100) + (bidVolume < askVolume ? '卖优' : '买优') : '--'}</b></span></div>
		{(!asks || !bids) && <p className="quote-data-note">当前来源没有可用五档，不用分钟线推算盘口。</p>}
		<header><strong>行情统计</strong><small>与报价同一快照</small></header>
		<dl className="quote-statistics">{[
			['今开', price(quote?.open), tone(quote?.open)], ['昨收', price(base), ''],
			['最高', price(quote?.high), tone(quote?.high)], ['最低', price(quote?.low), tone(quote?.low)],
			['均价', price(average), tone(average)], ['振幅', amplitude == null ? '--' : `${amplitude.toFixed(2)}%`, ''],
			['总量（手）', volume == null ? '--' : compact(volume / 100), ''], ['成交额', compact(amount), ''],
		].map(([label, value, color]) => <div key={label}><dt>{label}</dt><dd className={color}>{value}</dd></div>)}</dl>
		<section className="quote-unavailable"><strong>逐笔成交 / Level-2</strong><p>未接入逐笔数据。当前仅提供报价快照与分钟量价，不将分钟记录标成真实成交明细。</p>{external && <a href={external} target="_blank" rel="noreferrer">在东方财富查看原始行情 ↗</a>}</section>
		<footer><span>{quote?.meta.source || '来源待确认'}</span><time>{quote?.trade_time ? new Date(quote.trade_time).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) : '报价时间待确认'}</time><small>来源可能延迟；五档快照不是逐笔推送。</small></footer>
	</aside>;
}
