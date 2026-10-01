import type { PortfolioExpectationJob } from './backend';
import { portfolioDraftToHoldings, type PortfolioDraft } from './portfolio-draft';

export function portfolioExpectationIdentity(summaryDate: string, draft: PortfolioDraft) {
	return JSON.stringify([summaryDate, draft.profile, portfolioDraftToHoldings(draft.holdings)
		.map(item => ({ symbol: item.symbol, weight_percent: item.weight_percent, cost_price: item.cost_price || 0 }))
		.sort((a, b) => a.symbol.localeCompare(b.symbol))]);
}

export function matchingPortfolioExpectation(job: PortfolioExpectationJob | null, summaryDate: string, draft: PortfolioDraft) {
	if (!job) return false;
	return portfolioExpectationIdentity(job.request.summary_date, { profile: job.request.trader_profile, holdings: job.request.holdings.map(item => ({ symbol: item.symbol, name: item.name || '', weight: item.weight_percent, costPrice: item.cost_price?.toString() || '' })) }) === portfolioExpectationIdentity(summaryDate, draft);
}
