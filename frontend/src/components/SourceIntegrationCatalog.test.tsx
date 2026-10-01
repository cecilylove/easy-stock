import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SourceIntegrationCatalog } from './SourceIntegrationCatalog';
import { sourceIntegrations, sourceKindLabel, sourceName } from '../lib/source-integrations';

describe('source integration settings', () => {
	it('lists only implemented automatic sources with their actual capability labels', () => {
		const html = renderToStaticMarkup(<SourceIntegrationCatalog />);
		const articles = html.match(/<article[^>]*>.*?<\/article>/g) || [];
		expect(articles).toHaveLength(sourceIntegrations.length);
		for (const source of sourceIntegrations) {
			const article = articles.find(item => item.includes(`<strong>${source.name}</strong>`))!;
			expect(article).toBeDefined();
			for (const kind of source.kinds) expect(article).toContain(sourceKindLabel(kind));
			expect(article).toContain('已内置 · 自动使用');
		}
		for (const text of ['Tushare', 'TradingView', '预留凭据', '未接入']) expect(html).not.toContain(text);
	});
	it('distinguishes information from market capabilities without offering credential configuration', () => {
		const html = renderToStaticMarkup(<SourceIntegrationCatalog />);
		const articles = html.match(/<article[^>]*>.*?<\/article>/g) || [];
		const cffex = articles.find(item => item.includes('<strong>中国金融期货交易所</strong>'))!;
		expect(cffex).toContain('行情数据'); expect(cffex).not.toContain('资讯信息'); expect(cffex).toContain('单日持仓快照'); expect(cffex).toContain('不提供价格、基差');
		const cls = articles.find(item => item.includes('<strong>财联社</strong>'))!;
		expect(cls).toContain('资讯信息'); expect(cls).not.toContain('行情数据');
		const ths = articles.find(item => item.includes('<strong>同花顺</strong>'))!;
		expect(ths).toContain('行情数据'); expect(ths).toContain('人气榜');
		const eastmoney = articles.find(item => item.includes('<strong>东方财富</strong>'))!;
		expect(eastmoney).toContain('行情数据'); expect(eastmoney).toContain('资讯信息'); expect(eastmoney).toContain('已停用个股K、复权和指数取数'); expect(eastmoney).toContain('代表接口成功/失败都不代表所有行情与资讯功能');
		expect(html).toContain('实际请求的结果'); expect(html).toContain('无需填写凭据');
	});
	it('names actual source variants and fusion lists without inventing unknown providers', () => {
		expect(sourceName('sina:stock-money-flow')).toBe('新浪财经');
		expect(sourceName('tencent:industry-momentum+duanxianxia+sina:kline')).toBe('腾讯财经 + 短线侠 / 开盘啦 + 新浪财经');
		expect(sourceName('cffex:daily')).toBe('中国金融期货交易所');
		expect(sourceName('eastmoney:history+unknown')).toBe('东方财富 + unknown');
	});
});
