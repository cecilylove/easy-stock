import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SourceIntegrationCatalog } from './SourceIntegrationCatalog';

describe('source integration settings', () => {
	it('lists automatic sources as well as providers with reserved credentials', () => {
		const html = renderToStaticMarkup(<SourceIntegrationCatalog />);
		for (const name of ['短线侠 / 开盘啦', '东方财富', '新浪财经', '腾讯财经', '财联社', 'Tushare', '同花顺', 'TradingView']) expect(html).toContain(name);
		expect(html).toContain('无需填写 Token 或 Cookie');
	});
	it('does not represent saved credentials as functional provider integration', () => {
		const html = renderToStaticMarkup(<SourceIntegrationCatalog />);
		expect(html).toContain('填写后不会接入，也不会参与失败回退');
		expect(html).toContain('不参与行情或题材聚合');
		expect(html).toContain('当前没有取数实现和配置入口');
		expect(html).toContain('保存预留凭据不会启用新来源或改变降级链路');
	});
});
