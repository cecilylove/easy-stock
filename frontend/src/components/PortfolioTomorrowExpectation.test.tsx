import { describe, expect, it } from 'vitest';
import { PortfolioTomorrowExpectation } from './PortfolioTomorrowExpectation';

describe('portfolio expectation session isolation', () => {
	it('starts a new React session when review date or backend changes', () => {
		const config = { backendUrl: 'http://127.0.0.1:20081', token: '' };
		const first = PortfolioTomorrowExpectation({ config, summaryDate: '2026-09-29' });
		const nextDate = PortfolioTomorrowExpectation({ config, summaryDate: '2026-09-30' });
		const nextBackend = PortfolioTomorrowExpectation({ config: { ...config, backendUrl: 'http://127.0.0.1:20082' }, summaryDate: '2026-09-29' });
		expect(first.key).not.toEqual(nextDate.key);
		expect(first.key).not.toEqual(nextBackend.key);
	});
});
