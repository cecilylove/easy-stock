import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { SourceHealthPanel } from './SourceHealthPanel';
import { sourceIntegrations } from '../lib/source-integrations';

describe('settings source observations', () => {
	it('merges capabilities and recent results without a global availability score', () => {
		const html = renderToStaticMarkup(<SourceHealthPanel id="sources" onRefresh={() => {}} sources={[
			{ id: 'sina', name: 'raw', category: 'kline', ok: false, status: 'degraded', message: '来源超时', checked_at: '2026-10-01T01:00:00Z', last_failure: '2026-10-01T01:00:00Z' },
			{ id: 'eastmoney', name: '东方财富', category: 'f10', ok: true, status: 'available' },
		]} />);
		expect(html).toContain('新浪财经'); expect(html).toContain('来源超时'); expect(html).toContain('最近失败');
		expect(html).toContain('已实现功能范围：K 线'); expect(html).toContain('东方财富'); expect(html).toContain('已实现功能范围：公司资料');
		expect(html.match(/<header>.*?<\/header>/)?.[0]).not.toMatch(/\d+ 可用 \/|\d+\/\d+/); expect(html).toContain('不代表全部功能');
	});
	it('shows unknown before actual requests and explains passive reads and unavailable functionality', () => {
		const html = renderToStaticMarkup(<SourceHealthPanel id="sources" onRefresh={() => {}} sources={[]} />);
		for (const source of sourceIntegrations) expect(html).toContain(source.name);
		expect((html.match(/source-observation-status unknown/g) || [])).toHaveLength(sourceIntegrations.length);
		expect(html).toContain('每 30 秒读取'); expect(html).toContain('观测有效期 10 分钟'); expect(html).toContain('不自动发起检测'); expect(html).toContain('刷新检测'); expect(html).toContain('未主动检测');
		expect(html).toContain('不复权 / 前复权 / 后复权：腾讯'); expect(html).toContain('指定复权失败时不会混用备用口径');
		expect(html).not.toContain('未接入');
	});
	it('keeps previous observations visibly historical when record reads fail', () => {
		const html = renderToStaticMarkup(<SourceHealthPanel id="sources" onRefresh={() => {}} readAt="2026-10-01T01:00:00Z" error="连接中断" sources={[
			{ id: 'sina', name: '新浪', category: 'quote', ok: true, status: 'available', checked_at: '2026-10-01T00:59:00Z' },
		]} />);
		expect(html).toContain('连接中断'); expect(html).toContain('不代表当前可用性'); expect(html).toContain('上次读取记录');
	});
	it('drops legacy unimplemented categories and keeps CFFEX limitations explicit', () => {
		const html = renderToStaticMarkup(<SourceHealthPanel id="sources" onRefresh={() => {}} sources={[
			{ id: 'cls', name: '财联社', category: 'news,calendar', ok: false, status: 'unknown' },
		]} />);
		expect(html).toContain('已实现功能范围：资讯'); expect(html).not.toContain('calendar'); expect(html).not.toContain('日历');
		expect(html).toContain('不提供价格、基差、实时盘口或完整历史曲线');
	});
});
