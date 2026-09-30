import type { StockDirectoryEntry } from './backend';
import { normalizeAnalysisSymbol } from './stock-analysis';

export type StockDetailPeriod = 'intraday' | 'five-day' | 'day' | 'week' | 'month';

export const stockDetailPeriods: { key: StockDetailPeriod; label: string; apiPeriod: string; limit: number; mode: 'intraday' | 'daily' }[] = [
	{ key: 'intraday', label: '分时', apiPeriod: '1', limit: 240, mode: 'intraday' },
	{ key: 'five-day', label: '5日', apiPeriod: '5', limit: 240, mode: 'intraday' },
	{ key: 'day', label: '日K', apiPeriod: 'day', limit: 120, mode: 'daily' },
	{ key: 'week', label: '周K', apiPeriod: 'week', limit: 80, mode: 'daily' },
	{ key: 'month', label: '月K', apiPeriod: 'month', limit: 60, mode: 'daily' },
];

// This route carries only a stock symbol, never account or model data.
export function stockDetailPath(symbol = '') {
	return symbol ? `#stock-detail/${encodeURIComponent(symbol)}` : '#stock-detail';
}

export function stockDetailSymbolFromHash(hash: string) {
	if (!hash.startsWith('#stock-detail/')) return '';
	try {
		const segment = decodeURIComponent(hash.slice('#stock-detail/'.length));
		return resolveStockDetailSymbol(segment, []);
	} catch {
		return '';
	}
}

function inferredAStockMarket(code: string) {
	if (/^6/.test(code)) return 'SH';
	if (/^(?:00|30)/.test(code)) return 'SZ';
	if (/^(?:4|8|92)/.test(code)) return 'BJ';
	return '';
}

export function resolveStockDetailSymbol(input: string, directory: StockDirectoryEntry[]) {
	const normalized = normalizeAnalysisSymbol(input);
	const matched = /^(\d{6})(?:\.(SH|SZ|BJ))?$/.exec(normalized);
	if (matched) {
		const [, code, requestedMarket] = matched;
		const directoryMatches = directory.filter(stock => stock.code === code);
		if (directoryMatches.length) {
			const exact = requestedMarket ? directoryMatches.find(stock => stock.symbol === `${code}.${requestedMarket}`) : directoryMatches[0];
			return exact?.symbol || '';
		}
		const market = inferredAStockMarket(code);
		return market && (!requestedMarket || requestedMarket === market) ? `${code}.${market}` : '';
	}
	const exact = directory.filter(stock => stock.name === input.trim());
	return exact.length === 1 ? exact[0].symbol : '';
}
