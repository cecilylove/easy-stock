import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SourceHealthPanel } from './SourceHealthPanel';

describe('source diagnostics', () => {
	it('shows provider identity, failure explanation and observation times without hover', () => {
		const html = renderToStaticMarkup(<SourceHealthPanel id="sources" context="market" onRefresh={() => {}} sources={[
			{ id: 'eastmoney', name: '东方财富', category: 'kline', ok: false, status: 'degraded', message: '已切换备用来源', checked_at: '2026-09-30T01:00:00Z', last_failure: '2026-09-30T01:00:00Z' },
			{ id: 'tushare', name: 'Tushare', category: 'daily', ok: false, status: 'unconfigured' },
		]} />);
		expect(html).toContain('东方财富');
		expect(html).toContain('已切换备用来源');
		expect(html).toContain('最近失败');
		expect(html).toContain('当前版本未接入');
		expect(html).toContain('不是当前模块用了 7 个来源');
		expect(html).toContain('观测有效期 10 分钟');
	});
	it('keeps theme step failures separate from the global supplier status', () => {
		const html = renderToStaticMarkup(<SourceHealthPanel id="sources" context="themes" onRefresh={() => {}} sources={[]} steps={{ industry: 'ready', kaipanla: 'error', strength: 'error' }} stepErrors={{ kaipanla: '开盘啦暂无有效题材快照', strength: '题材强度暂不可用' }} />);
		expect(html).toContain('行业强度：已完成');
		expect(html).toContain('开盘啦题材 / 涨停池：失败');
		expect(html).toContain('开盘啦暂无有效题材快照');
		expect(html).toContain('题材强度暂不可用');
	});
	it('offers source settings and explains exhaustion rather than promising universal fallback', () => {
		const html = renderToStaticMarkup(<SourceHealthPanel id="sources" context="market" onRefresh={() => {}} onOpenSettings={() => {}} sources={[
			{ id: 'tushare', name: 'Tushare', category: 'daily', ok: false, status: 'unconfigured' },
		]} />);
		expect(html).toContain('数据源接入与配置');
		expect(html).toContain('当前没有取数实现');
		expect(html).toContain('不会参与失败回退');
		expect(html).toContain('失败后如何降级，什么时候会不可用');
		expect(html).toContain('没有统一备用供应商');
		expect(html).toContain('无快照时该模块不可用');
		expect(html).toContain('标记陈旧');
	});
});
