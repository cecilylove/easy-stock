import type { StockDirectoryEntry } from './backend';

const storageKey = 'easy-stock.stock-directory.v1';
const ttl = 24 * 60 * 60 * 1000;

export function loadCachedStockDirectory(storage: Pick<Storage, 'getItem'> = window.localStorage, now = Date.now()) {
	try {
		const raw = storage.getItem(storageKey);
		if (!raw) return [];
		const cached = JSON.parse(raw) as { cachedAt?: number; stocks?: StockDirectoryEntry[] };
		if (!cached.cachedAt || now - cached.cachedAt > ttl || !Array.isArray(cached.stocks)) return [];
		return cached.stocks.filter(stock => stock && typeof stock.symbol === 'string' && typeof stock.code === 'string' && typeof stock.name === 'string');
	} catch { return []; }
}

export function saveCachedStockDirectory(stocks: StockDirectoryEntry[], storage: Pick<Storage, 'setItem'> = window.localStorage, now = Date.now()) {
	try { storage.setItem(storageKey, JSON.stringify({ cachedAt: now, stocks })); }
	catch { /* In-memory directory remains available if storage is full. */ }
}
