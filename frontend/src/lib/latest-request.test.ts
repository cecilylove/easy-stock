import { describe, expect, it } from 'vitest';
import { LatestRequest } from './latest-request';

describe('request generations', () => {
	it('rejects a late response after switching dimensions or repeating a query', async () => {
		const requests = new LatestRequest();
		const old = requests.begin('industry');
		const next = requests.begin('stock');
		expect(old.signal.aborted).toBe(true);
		expect(requests.isCurrent(old, 'stock')).toBe(false);
		expect(requests.isCurrent(next, 'stock')).toBe(true);
		const repeat = requests.begin('stock');
		expect(requests.isCurrent(next, 'stock')).toBe(false);
		expect(requests.isCurrent(repeat, 'stock')).toBe(true);
	});
	it('hides the old query before its cancellation effect runs and after unmount', () => {
		const requests = new LatestRequest();
		const request = requests.begin('old');
		expect(requests.isCurrent(request, 'new')).toBe(false);
		requests.cancel();
		expect(requests.isCurrent(request, 'old')).toBe(false);
	});
});
