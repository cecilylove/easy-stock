import type { AuctionTrace } from './backend';
import { shanghaiDayAndMinute } from './stock-intraday';

const keyPrefix = 'easy-stock.auction-trace.v1:';

export function readAuctionTrace(symbol: string, today: string, storage?: Pick<Storage, 'getItem'>): AuctionTrace | null {
	try {
		const raw = (storage ?? window.localStorage).getItem(keyPrefix + symbol);
		if (!raw) return null;
		const trace = JSON.parse(raw) as AuctionTrace;
		if (trace.symbol !== symbol || trace.trade_date !== today || !Array.isArray(trace.points) || !trace.points.length || !trace.meta?.fetched_at) return null;
		if (trace.points.some(point => !Number.isFinite(point.price) || point.price <= 0 || shanghaiDayAndMinute(point.time)?.day !== today)) return null;
		return trace;
	} catch { return null; }
}

export function saveAuctionTrace(trace: AuctionTrace, storage?: Pick<Storage, 'setItem'>) {
	try { (storage ?? window.localStorage).setItem(keyPrefix + trace.symbol, JSON.stringify(trace)); }
	catch { /* Storage is optional; the active page still has the snapshot. */ }
}
