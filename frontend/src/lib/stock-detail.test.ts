import { describe, expect, it } from 'vitest';
import { resolveStockDetailSymbol, stockDetailPath, stockDetailPeriods, stockDetailSymbolFromHash } from './stock-detail';

const directory = [
	{ symbol: '600519.SH', code: '600519', name: '贵州茅台' },
	{ symbol: '000001.SZ', code: '000001', name: '平安银行' },
];

describe('stock detail navigation and lookup', () => {
	it('restores only a valid A-share symbol from the URL', () => {
		expect(stockDetailPath('600519.SH')).toBe('#stock-detail/600519.SH');
		expect(stockDetailSymbolFromHash('#stock-detail/600519.SH')).toBe('600519.SH');
		expect(stockDetailSymbolFromHash('#stock-detail/000001.sz')).toBe('000001.SZ');
		expect(stockDetailSymbolFromHash('#stock-detail/830799')).toBe('830799.BJ');
		expect(stockDetailSymbolFromHash('#stock-detail/../../secret')).toBe('');
		expect(stockDetailSymbolFromHash('#stock-detail/000001.SH')).toBe('');
		expect(stockDetailSymbolFromHash('#stock-detail/900001.SH')).toBe('');
		expect(stockDetailSymbolFromHash('#stock-detail/399001.SZ')).toBe('');
		expect(stockDetailSymbolFromHash('#stock-detail/%ZZ')).toBe('');
	});
	it('searches by name or code and still resolves codes when directory is offline', () => {
		expect(resolveStockDetailSymbol('贵州茅台', directory)).toBe('600519.SH');
		expect(resolveStockDetailSymbol('600519', [])).toBe('600519.SH');
		expect(resolveStockDetailSymbol('000001', [])).toBe('000001.SZ');
		expect(resolveStockDetailSymbol('830799', [])).toBe('830799.BJ');
		expect(resolveStockDetailSymbol('920799', [])).toBe('920799.BJ');
		expect(resolveStockDetailSymbol('贵州', directory)).toBe('');
		expect(resolveStockDetailSymbol('000001.SH', directory)).toBe('');
		expect(resolveStockDetailSymbol('399001', [])).toBe('');
		expect(resolveStockDetailSymbol('900001', [])).toBe('');
		expect(resolveStockDetailSymbol('not-a-stock', [])).toBe('');
	});
	it('uses five-minute samples for the five-day tab and loads one period at a time', () => {
		expect(stockDetailPeriods.map(item => item.key)).toEqual(['intraday', 'five-day', 'day', 'week', 'month', 'year', 'minute5', 'minute15', 'minute30', 'minute60']);
		expect(stockDetailPeriods.find(item => item.key === 'five-day')?.apiPeriod).toBe('5');
	});
});
