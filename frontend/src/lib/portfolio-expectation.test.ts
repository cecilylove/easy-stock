import { describe, expect, it } from 'vitest';
import type { PortfolioExpectationJob } from './backend';
import { matchingPortfolioExpectation, portfolioExpectationIdentity } from './portfolio-expectation';
import { type PortfolioDraft } from './portfolio-draft';

const draft: PortfolioDraft = { profile: 'balanced', holdings: [{ symbol: '000001.SZ', name: 'fixture', weight: 20, costPrice: '10' }] };
const job = { request: { summary_date: '2026-09-29', trader_profile: 'balanced', holdings: [{ symbol: '000001.SZ', weight_percent: 20, cost_price: 10 }] } } as PortfolioExpectationJob;

describe('portfolio expectation identity', () => {
	it('cannot reopen an earlier review date or mismatching holdings', () => {
		expect(matchingPortfolioExpectation(job, '2026-09-29', draft)).toBe(true);
		expect(matchingPortfolioExpectation(job, '2026-09-30', draft)).toBe(false);
		expect(matchingPortfolioExpectation(null, '2026-09-30', draft)).toBe(false);
		for (const change of [{ weight: 30 }, { costPrice: '11' }, { symbol: '600001.SH' }]) {
			expect(matchingPortfolioExpectation(job, '2026-09-29', { ...draft, holdings: [{ ...draft.holdings[0], ...change }] })).toBe(false);
		}
		expect(matchingPortfolioExpectation(job, '2026-09-29', { ...draft, profile: 'steady' })).toBe(false);
	});
	it('uses stable identities independent of holding order and display names', () => {
		const second = { symbol: '600001.SH', name: 'other', weight: 10, costPrice: '' };
		expect(portfolioExpectationIdentity('2026-09-29', { ...draft, holdings: [...draft.holdings, second] })).toBe(portfolioExpectationIdentity('2026-09-29', { ...draft, holdings: [{ ...second, name: 'changed' }, ...draft.holdings] }));
	});
});
