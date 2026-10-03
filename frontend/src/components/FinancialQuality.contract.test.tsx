// @vitest-environment jsdom
import { renderToStaticMarkup } from 'react-dom/server';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it, vi } from 'vitest';
import type { MarketMarginPoint, StockAIAnalysis, StockAIFundamental } from '../lib/backend';
import { FundamentalPanel } from './StockAIAnalysisWorkspace';
import { MarginBalanceView } from './market/MarketDataViews';
import { LadderStockChip } from './LimitUpWorkspace';
import type { LimitUpLadderStock } from '../lib/backend';
import { buildMarketModulePrompt } from '../lib/market-overview';

const fundamentals: StockAIFundamental = { available: true, fields_known: true, available_fields: ['revenue','net_profit','recurring_net_profit'], score_available: false, score: 50, quality: '数据不足', report_date: '2026-06-30', report_name: '中报', revenue: 0, revenue_yoy: 0, net_profit: 0, net_profit_yoy: 0, recurring_net_profit_available: true, recurring_net_profit: 0, recurring_net_profit_yoy: 0, non_recurring_profit_ratio: 0, eps: 0, roe: 0, gross_margin: 0, debt_ratio: 0, operating_cash_flow_per_share: 0, summary: '缺失字段不按零评分', source: 'fixture' };
const partial: MarketMarginPoint = { trade_date: '2026-09-30', markets: ['007'], missing_markets: ['001','002'], coverage_known: true, coverage_complete: false, change_available: false, financing_balance: 100, securities_lending_balance: 10, margin_balance: 110, margin_balance_change: -1293577201676, financing_buy_amount: 0, financing_repay_amount: 0, financing_net_buy_amount: 0, securities_lending_sell_volume: 0, securities_lending_repay_volume: 0, meta: { source: 'eastmoney:margin-balance', fetched_at: '', stale: false, latency_ms: 0, partial: true, fields_known: true, available_fields: ['margin_balance','financing_balance','securities_lending_balance'] } };

describe('financial evidence quality contracts', () => {
 it('does not display legacy parent-profit fallback as deducted-profit growth', () => {
  const legacy={...fundamentals,fields_known:undefined,available_fields:undefined,recurring_net_profit_available:false,recurring_net_profit_yoy:123.4};
  const host=document.createElement('div');host.innerHTML=renderToStaticMarkup(<FundamentalPanel analysis={{fundamental:legacy} as StockAIAnalysis} />);
  const deducted=[...host.querySelectorAll('.stock-ai-fundamental-metrics article')].find(node=>node.textContent?.startsWith('扣非净利润'));
  expect(deducted?.textContent).toContain('同比未提供');expect(deducted?.textContent).not.toContain('123.4');
 });
 it('does not interpret unknown open-count as a never-opened board and keeps field provenance', () => {
  const stock: LimitUpLadderStock={name:'fixture',symbol:'600001.SH',price:0,change_percent:0,amount:0,float_market_cap:0,turnover_rate:0,days:0,count:0,limit_regime:'10cm',primary_theme:'',secondary_themes:[],theme_confidence:0,theme_evidence:[],streak:0,open_count:0,raw_concepts:[],industry:'',is_st:false,meta:{source:'duanxianxia',fetched_at:'',latency_ms:0,stale:false,fields_known:true,available_fields:[],field_sources:{price:'new-vendor'},field_fetched_at:{price:'2026-09-30'}},source:'duanxianxia'};
  const html=renderToStaticMarkup(<LadderStockChip stock={stock} tradeDate="2026-09-30" compact={false} showCurrentChange={false} onSelect={()=>{}} onSelectBillboard={()=>{}} />);
  expect(html).toContain('开板次数未知');expect(html).not.toContain('封板未开');expect(html).toContain('new-vendor');
 });
 it('keeps missing net financing buy unknown in the full-market chart tooltip', () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  const host=document.createElement('div');const root=createRoot(host);
  const rows=['2026-09-29','2026-09-30'].map(trade_date => ({...partial,trade_date,markets:['001','002','007'],missing_markets:[],coverage_complete:true}));
  act(()=>root.render(<MarginBalanceView items={rows} limit={30} onLimit={()=>{}} meta={partial.meta} />));
  const layer=host.querySelector<SVGRectElement>('.market-margin-hover-layer')!;
  layer.getBoundingClientRect=()=>({left:0,width:100,right:100,top:0,bottom:100,height:100,x:0,y:0,toJSON(){return {};}});
  act(()=>layer.dispatchEvent(new MouseEvent('mousemove',{bubbles:true,clientX:100})));
  const tooltip=host.querySelector('.market-margin-tooltip');
  expect(tooltip?.textContent).toContain('融资净买入--');
  expect(tooltip?.textContent).toContain('余额变化--');
  act(()=>root.unmount());vi.unstubAllGlobals();
 });
 it('renders valid zero amounts but not missing growth ratios or a synthetic score', () => {
  const html=renderToStaticMarkup(<FundamentalPanel analysis={{fundamental: fundamentals} as StockAIAnalysis} />);
  expect(html).toContain('同比未提供');
  expect(html).not.toContain('50 ·'); expect(html).not.toContain('0.0%');
  expect(html).toContain('数据不足'); expect(html).toContain('扣非净利润');
 });
 it('marks partial market totals and suppresses unavailable changes in view and AI evidence', () => {
  const html=renderToStaticMarkup(<MarginBalanceView items={[partial]} limit={30} onLimit={()=>{}} meta={partial.meta} />);
  expect(html).toContain('部分市场两融余额'); expect(html).toContain('不计算日变化');
  expect(html).not.toContain('12,935'); expect(html).not.toContain('全市场两融余额');
  const prompt=buildMarketModulePrompt('margin-balance',{margins:[partial],meta:partial.meta},'2026-09-30');
  expect(prompt).toContain('仅部分市场'); expect(prompt).toContain('两融余额日变动 未提供/覆盖不足'); expect(prompt).not.toContain('-12935');
 });
});
