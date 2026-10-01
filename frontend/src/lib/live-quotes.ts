import type { Quote, SourceMeta } from './backend';
import { shanghaiDayAndMinute } from './stock-intraday';

export type LiveQuote = Pick<Quote, 'price' | 'change' | 'change_percent'> & {
	trade_time?: string; meta?: SourceMeta; received_at?: number; connected?: boolean;
};
export type LiveQuoteLookup = Record<string, LiveQuote>;
const receivedTTL = 30_000;
const tradeTTL = 60_000;

function quoteObservationTime(quote: LiveQuote) { return Date.parse(quote.meta?.fetched_at || ''); }

export function mergeLiveQuotes(current: LiveQuoteLookup, quotes: Quote[], now = Date.now()): LiveQuoteLookup {
	const next = { ...current };
	for (const quote of quotes) {
		if (!Number.isFinite(quote.price) || quote.price <= 0 || !Number.isFinite(quote.change_percent)) continue;
		const timestamp = quoteObservationTime(quote);
		const previous = current[quote.symbol];
		if (!Number.isFinite(timestamp) || (previous && timestamp < quoteObservationTime(previous))) continue;
		next[quote.symbol] = { price: quote.price, change: quote.change, change_percent: quote.change_percent, trade_time: quote.trade_time, meta: quote.meta, received_at: now, connected: true };
	}
	return next;
}

export function disconnectLiveQuotes(current: LiveQuoteLookup): LiveQuoteLookup {
	return Object.fromEntries(Object.entries(current).map(([symbol, quote]) => [symbol, { ...quote, connected: false }]));
}

export function isCurrentLiveQuote(quote: LiveQuote, snapshot: SourceMeta | undefined, now = Date.now()) {
	// Fetch time alone does not prove that a market quote is current.
	const timestamp = Date.parse(quote.trade_time || '');
	const received = quote.received_at;
	if (!quote.connected || quote.meta?.stale || received == null || !Number.isFinite(timestamp)) return false;
	if (now - received < 0 || now - received > receivedTTL || now - timestamp < -5_000 || now - timestamp > tradeTTL) return false;
	if (shanghaiDayAndMinute(new Date(timestamp).toISOString())?.day !== shanghaiDayAndMinute(new Date(now).toISOString())?.day) return false;
	const snapshotAt = Date.parse(snapshot?.fetched_at || '');
	// trade_time is the exchange's last tick; fetched_at is when each provider
	// observed its payload. Compare like timestamps when choosing an overlay.
	const observedAt = quoteObservationTime(quote);
	return Number.isFinite(observedAt) && (!Number.isFinite(snapshotAt) || observedAt >= snapshotAt);
}
