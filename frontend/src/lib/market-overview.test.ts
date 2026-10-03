import { describe, expect, it } from 'vitest';
import type { MarketFundFlow, MarketIndustryMomentum, MarketIndexSnapshot, SourceMeta } from './backend';
import { buildMarketBillboardPrompt, buildMarketModulePrompt, buildMarketPulsePrompt, marketOverviewGroups, resolveMarketOverviewView } from './market-overview';

describe('market overview navigation', () => {
	it('uses the approved product names and grouping', () => {
		expect(marketOverviewGroups.map((group) => group.name)).toEqual(['市场总览', '资金结构', '研究信号']);
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).toContain('市场核心指数');
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).toContain('融资融券余额');
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).toContain('龙虎榜');
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).not.toContain('全球温度');
		expect(marketOverviewGroups.flatMap((group) => group.modules.every((module) => module.status === 'ready'))).toEqual([true, true, true]);
	});

	it('resolves supported market hashes and falls back to pulse', () => {
		expect(resolveMarketOverviewView('#market/core-indexes')).toBe('core-indexes');
		expect(resolveMarketOverviewView('#market/billboard')).toBe('billboard');
		expect(resolveMarketOverviewView('#market/margin-balance')).toBe('margin-balance');
		expect(resolveMarketOverviewView('#market/global')).toBe('pulse');
		expect(resolveMarketOverviewView('#market/unknown')).toBe('pulse');
	});
});

describe('market module AI prompt', () => {
	it('includes margin balance trend evidence', () => {
		const prompt = buildMarketModulePrompt('margin-balance', {
			margins: [{
				trade_date: '2026-08-11', financing_balance: 2_700_000_000_000,
				securities_lending_balance: 30_000_000_000, margin_balance: 2_730_000_000_000,
				margin_balance_change: 12_000_000_000, financing_buy_amount: 120_000_000_000,
				financing_repay_amount: 108_000_000_000, financing_net_buy_amount: 12_000_000_000,
				securities_lending_sell_volume: 1_000_000, securities_lending_repay_volume: 800_000,
				meta: { source: 'eastmoney:margin-balance', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
			}],
			meta: { source: 'eastmoney:margin-balance', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
		}, '2026-08-11 18:10');
		expect(prompt).toContain('两融余额 2.73万亿');
		expect(prompt).toContain('融资净买入 120.00亿');
		expect(prompt).toContain('eastmoney:margin-balance');
	});

	it('keeps fund-flow values, source and fallback evidence', () => {
		const prompt = buildMarketModulePrompt('stock-flow', {
			flows: [{
				dimension: 'stock', code: '600001', symbol: '600001.SH', name: '样本股份', price: 12.3,
				change_percent: 2.5, main_net_inflow: 120_000_000, main_net_inflow_ratio: 8.2,
				inflow: 230_000_000, outflow: 110_000_000, net_inflow: 120_000_000, net_inflow_ratio: 4.2,
				main_inflow: 160_000_000, main_outflow: 40_000_000,
				retail_inflow: 70_000_000, retail_outflow: 70_000_000, retail_net_inflow: 0, retail_net_inflow_ratio: 0,
				super_large_net_inflow: 80_000_000, super_large_net_inflow_ratio: 5.1,
				large_net_inflow: 40_000_000, large_net_inflow_ratio: 3.1,
				medium_net_inflow: -10_000_000, medium_net_inflow_ratio: -0.6,
				small_net_inflow: -110_000_000, small_net_inflow_ratio: -7.6,
				leader_price: 0, leader_change_percent: 0, leader_net_inflow_ratio: 0,
				meta: { source: 'eastmoney:fund-flow', fetched_at: '2026-08-11T14:30:00+08:00', latency_ms: 10, stale: false },
			}],
			meta: { source: 'eastmoney:fund-flow', fetched_at: '2026-08-11T14:30:00+08:00', latency_ms: 10, stale: true, fallback_reason: '使用最近快照' },
		}, '2026-08-11 15:00');
		expect(prompt).toContain('样本股份（600001.SH）');
		expect(prompt).toContain('1.20亿');
		expect(prompt).toContain('eastmoney:fund-flow');
		expect(prompt).toContain('使用最近快照');
		expect(prompt).toContain('不得补造');
	});
});

describe('market AI source masks', () => {
	const known = (fields: string[]): SourceMeta => ({ source: 'row-source', fetched_at: '', latency_ms: 0, stale: false, fields_known: true, available_fields: fields });
	it('uses the same unknown foreign clock contract as the UI', () => {
		const item = { id: 'x', name: '海外指数', region: 'US', market: 'US', price: 123, change_percent: 0, status: 'unknown', trade_time: '0001-01-01T00:00:00Z', meta: { ...known(['price']), time_zone: 'unknown', native_timestamp: '20261001093000' } } as MarketIndexSnapshot;
		const prompt = buildMarketModulePrompt('core-indexes', { indexes: [item] }, '测试时刻');
		expect(prompt).toContain('行情时间 未知（来源时间 20261001093000；时区偏移未确认）');
		expect(prompt).not.toContain('0001-01-01');
	});
	it('does not describe missing global index fields as a flat price fact', () => {
		const item = { id: 'x', name: '缺失指数', region: 'US', market: 'US', price: 123, change_percent: 0, status: 'unavailable', meta: known([]) } as MarketIndexSnapshot;
		const prompt = buildMarketModulePrompt('core-indexes', { indexes: [item], meta: known(['price', 'change_percent']) }, '测试时刻');
		expect(prompt).toContain('价格 未提供，涨跌 未提供');
		expect(prompt).not.toContain('0.00%');
	});
	it('respects industry row masks over the list and does not invent score or five-day zero', () => {
		const item: MarketIndustryMomentum = { code: 'x', name: '缺字段行业', change_percent: 0, five_day_change_percent: 0, twenty_day_change_percent: 0, turnover_rate: 0, rising_count: 0, falling_count: 0, main_net_inflow: 0, leader_change_percent: 0, score: 50, meta: known(['change_percent']) };
		const prompt = buildMarketModulePrompt('industry-momentum', { industries: [item], meta: known(['score', 'change_percent', 'five_day_change_percent', 'main_net_inflow']) }, '测试时刻');
		expect(prompt).toContain('动能 未提供');
		expect(prompt).toContain('当日 0.00%');
		expect(prompt).toContain('5日 未提供');
		expect(prompt).toContain('上涨/下跌 未提供/未提供');
		expect(prompt).not.toContain('50.0');
	});
	it('checks every money/ratio/leader field independently and honors known-empty masks', () => {
		const item = { dimension: 'industry', code: 'x', name: '资金样本', net_inflow: 990_000_000, net_inflow_ratio: 12.3, main_net_inflow: 120_000_000, main_net_inflow_ratio: 0, outflow: 123, leader_name: '隐藏领涨', meta: known(['main_net_inflow']) } as MarketFundFlow;
		const prompt = buildMarketModulePrompt('industry-flow', { flows: [item], meta: known(['net_inflow', 'main_net_inflow', 'main_net_inflow_ratio', 'leader_name']) }, '测试时刻');
		expect(prompt).toContain('总净流入 未提供');
		expect(prompt).toContain('主力净流入 1.20亿（主力净流入率 未提供）');
		expect(prompt).toContain('流出 未提供');
		expect(prompt).toContain('领涨 未提供');
		expect(prompt).not.toContain('9.90亿');
		expect(prompt).not.toContain('隐藏领涨');
		item.meta = known([]);
		expect(buildMarketModulePrompt('industry-flow', { flows: [item], meta: known(['main_net_inflow']) }, '测试时刻')).not.toContain('1.20亿');
	});
	it('uses list schema for legacy unmasked rows without treating a masked zero as absent', () => {
		const item = { dimension: 'industry', code: 'x', name: '零值样本', net_inflow: 0, main_net_inflow: 99, meta: { source: 'legacy', fetched_at: '', stale: false, latency_ms: 0 } } as MarketFundFlow;
		const prompt = buildMarketModulePrompt('industry-flow', { flows: [item], meta: known(['net_inflow']) }, '测试时刻');
		expect(prompt).toContain('总净流入 0');
		expect(prompt).toContain('主力净流入 未提供');
	});
});

describe('market billboard AI prompt', () => {
 it('does not manufacture zero ladder facts from masked missing fields', () => {
  const meta={source:'fixture',fetched_at:'',stale:false,latency_ms:0,fields_known:true,available_fields:[]};
  const item={trade_date:'2026-09-30',symbol:'600001.SH',name:'样本',close_price:1,change_percent:1,turnover_rate:1,reason:'偏离值',buy_amount:1,sell_amount:1,net_amount:0,institution_buyers:0,buy_seats:0,sell_seats:0,meta};
  const stock={symbol:item.symbol,name:item.name,price:1,change_percent:1,amount:0,float_market_cap:0,turnover_rate:0,streak:0,open_count:0,days:0,count:0,is_st:false,limit_regime:'10cm',raw_concepts:[],primary_theme:'',secondary_themes:[],theme_confidence:0,theme_evidence:[],meta};
  const day={trade_date:item.trade_date,missing_fields:['streak'],limit_up_count:1,board_count:0,first_board_count:0,max_streak:0,reopened_count:0,st_count:0,total_amount:0,levels:[{level:0,label:'板数未知',count:1,stocks:[stock]}]};
  const prompt=buildMarketBillboardPrompt({items:[item],details:{},limitUp:{session_status:'closed',current:day,previous:{...day,trade_date:'2026-09-29',levels:[]},advance:[],industry_heat:[],concept_heat:[],meta}},'测试');
  expect(prompt).toContain('连板高度 未知');expect(prompt).toContain('换手率 未提供');expect(prompt).toContain('成交额 未提供');expect(prompt).toContain('板数覆盖不完整');
  expect(prompt).not.toContain('连板高度 0板');expect(prompt).not.toContain('最高 0 板');expect(prompt).not.toContain('首板 0 家');
 });
	it('includes seats, institution net flow, concentration, themes, streaks and two three-stock lists', () => {
		const item = {
			trade_date: '2026-08-11', symbol: '600001.SH', name: '样本股份', close_price: 12.3,
			change_percent: 10.01, turnover_rate: 18.2, reason: '日涨幅偏离值达7%',
			buy_amount: 100_000_000, sell_amount: 40_000_000, net_amount: 60_000_000,
			institution_buyers: 1, buy_seats: 5, sell_seats: 5,
			meta: { source: 'eastmoney:billboard', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
		};
		const prompt = buildMarketBillboardPrompt({
			items: [item],
			details: {
				'2026-08-11|600001.SH|日涨幅偏离值达7%': {
					trade_date: '2026-08-11', symbol: '600001.SH', reason: item.reason,
					buy_seats: [{ direction: 'buy', rank: 1, name: '机构专用', buy_amount: 30_000_000, buy_ratio: 30, sell_amount: 2_000_000, sell_ratio: 5, net_amount: 28_000_000, institution: true }],
					sell_seats: [{ direction: 'sell', rank: 1, name: '样本营业部', buy_amount: 1_000_000, buy_ratio: 1, sell_amount: 12_000_000, sell_ratio: 30, net_amount: -11_000_000, institution: false }],
					meta: { source: 'eastmoney:billboard-seats', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
				},
			},
			limitUp: {
				session_status: 'closed',
				current: { trade_date: '2026-08-11', limit_up_count: 30, board_count: 8, first_board_count: 22, max_streak: 5, reopened_count: 2, st_count: 0, total_amount: 0, levels: [{ level: 3, label: '3板', count: 1, stocks: [{ symbol: '600001.SH', name: '样本股份', price: 12.3, change_percent: 10.01, amount: 500_000_000, float_market_cap: 0, turnover_rate: 18.2, streak: 3, open_count: 0, industry: '软件', days: 3, count: 3, is_st: false, limit_regime: '10cm', raw_concepts: ['AI应用'], primary_theme: 'AI应用', secondary_themes: ['算力'], theme_confidence: 0.9, theme_evidence: [], theme_leader_role: '龙头' }] }], },
				previous: { trade_date: '2026-08-10', limit_up_count: 0, board_count: 0, first_board_count: 0, max_streak: 0, reopened_count: 0, st_count: 0, total_amount: 0, levels: [] },
				advance: [], industry_heat: [], concept_heat: [{ name: 'AI应用', count: 5, board_count: 2, max_streak: 3, previous_count: 3, heat: 88, leaders: ['样本股份'] }],
				meta: { source: 'limit-up:test', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
			},
			meta: item.meta,
		}, '2026-08-11 18:10');

		expect(prompt).toContain('机构席位净额 2800.0万');
		expect(prompt).toContain('买方买一/前三集中度 30.00% / 30.00%');
		expect(prompt).toContain('连板高度 3板');
		expect(prompt).toContain('题材 AI应用、算力');
		expect(prompt).toContain('明日最值得观察的3个名单');
		expect(prompt).toContain('明日需要优先止损/割肉评估的3个名单');
		expect(prompt).toContain('身份未确认');
	});
});

describe('market pulse AI prompt', () => {
	it('includes timestamps, sources, news and theme evidence', () => {
		const prompt = buildMarketPulsePrompt([
			{
				title: '测试快讯',
				published_at: '2026-08-11T14:30:00+08:00',
				meta: { source: 'cls', fetched_at: '2026-08-11T14:30:01+08:00', latency_ms: 10, stale: false },
			},
		], [
			{
				theme: 'semiconductor',
				name: '半导体',
				change_percent: 2.3,
				main_net_inflow: 120_000_000,
				rising_nodes: 8,
				falling_nodes: 2,
				matched_nodes: 10,
				total_nodes: 10,
				top_node_change_percent: 3.1,
				leaders: ['示例股份'],
			},
		], '2026-08-11 15:00');

		expect(prompt).toContain('测试快讯');
		expect(prompt).toContain('来源：cls');
		expect(prompt).toContain('半导体');
		expect(prompt).toContain('严格区分事实与推断');
	});
});
