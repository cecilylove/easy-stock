import { describe, expect, it } from 'vitest';
import { loadCachedStockDirectory, saveCachedStockDirectory } from './stock-directory-cache';

const stocks = [{ symbol: '600519.SH', code: '600519', name: '贵州茅台' }];
const now = 1_780_000_000_000;

function memoryStorage() {
	const values = new Map<string, string>();
	return {
		getItem: (key: string) => values.get(key) || null,
		setItem: (key: string, value: string) => { values.set(key, value); },
	};
}

describe('shared stock directory cache', () => {
	it('uses the existing analysis-page schema for both detail and analysis', () => {
		const storage = memoryStorage();
		saveCachedStockDirectory(stocks, storage, now);
		expect(JSON.parse(storage.getItem('easy-stock.stock-directory.v1') || '{}')).toEqual({ cachedAt: now, stocks });
		expect(loadCachedStockDirectory(storage, now + 60_000)).toEqual(stocks);
	});
	it('rejects expired or incompatible values instead of treating stale names as fresh', () => {
		const storage = memoryStorage();
		saveCachedStockDirectory(stocks, storage, now);
		expect(loadCachedStockDirectory(storage, now + 25 * 60 * 60 * 1000)).toEqual([]);
		storage.setItem('easy-stock.stock-directory.v1', JSON.stringify(stocks));
		expect(loadCachedStockDirectory(storage, now)).toEqual([]);
	});
});
