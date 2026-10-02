import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SourceIntegrationCatalog } from './SourceIntegrationCatalog';
import { sourceIntegrations, sourceKindLabel, sourceName, type SourceIntegration } from '../lib/source-integrations';

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
	it('renders a supplied registry and uses its display names without restoring removed providers', () => {
		const catalog: SourceIntegration[] = [{ id: 'new_vendor', name: '新的行情供应商', mode: 'public', kinds: ['market'], usage: '报价', configuration: '自动取数', capabilities: ['quote'], probeScope: '报价代表接口', implemented: true, enabled: true }];
		const html = renderToStaticMarkup(<SourceIntegrationCatalog catalog={catalog} sources={[{ id: 'new_vendor', name: 'raw name', category: 'quote', ok: true, status: 'available' }]} />);
		expect(html).toContain('<strong>新的行情供应商</strong>'); expect(html).toContain('最近可用');
		expect(html.match(/<article /g)).toHaveLength(1); expect(html).not.toContain('新浪财经');
		expect(sourceName('new_vendor:quote+sina:kline', catalog)).toBe('新的行情供应商 + 新浪财经');
	});
	it('does not claim a disabled or unimplemented source is being checked', () => {
		const catalog: SourceIntegration[] = [{ id: 'disabled', name: '停用来源', mode: 'credential', kinds: ['market'], usage: '报价', configuration: '凭据接入', capabilities: ['quote'], probeScope: '报价', implemented: true, enabled: false }, { id: 'planned', name: '规划来源', mode: 'archive', kinds: ['information'], usage: '文章', configuration: '', capabilities: [], probeScope: '', implemented: false, enabled: false }];
		const html = renderToStaticMarkup(<SourceIntegrationCatalog catalog={catalog} checking />);
		expect(html).toContain('当前未启用'); expect(html).toContain('尚未实现'); expect(html).not.toContain('检测中…');
		expect(html).toContain('凭据接入'); expect(html).toContain('规划能力');
	});
	it('shows an empty registry explicitly', () => {
		const html = renderToStaticMarkup(<SourceIntegrationCatalog catalog={[]} />);
		expect(html).toContain('当前服务没有注册数据源'); expect(html).not.toContain('<article');
	});
});
