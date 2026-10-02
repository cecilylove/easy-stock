import { HelpCircle, LoaderCircle } from 'lucide-react';
import { useId } from 'react';
import type { SourceHealth, SourceProbeResult } from '../lib/backend';
import { normalizeSourceHealth, sourceCategoryLabel, sourceHealthDetail, sourceHealthLabel, sourceProbeExpired } from '../lib/source-health';
import { sourceIntegrationLabel, sourceIntegrations, sourceKindLabel, type SourceIntegration } from '../lib/source-integrations';

export function SourceIntegrationCatalog({ sources = [], probes = [], catalog = sourceIntegrations, now = Date.now(), checking = false, probeRetained = false, retained = false, recordsRead = false }: { sources?: SourceHealth[]; probes?: SourceProbeResult[]; catalog?: SourceIntegration[]; now?: number; checking?: boolean; probeRetained?: boolean; retained?: boolean; recordsRead?: boolean }) {
	const tooltipPrefix = useId();
	const observations = normalizeSourceHealth(sources, catalog);
	return <div className="source-integration-catalog" aria-label="数据源说明">
		<p className="settings-field-note">{catalog.every(source => source.mode === 'public') ? '按具体功能自动调用，无需填写凭据或手动接入。' : '来源接入方式与启用状态由下列目录说明。'}能力标签表示用途，观测记录表示最近实际请求的结果；某家来源成功不代表它的所有功能正常，也不保证后续请求成功。</p>
		{catalog.length === 0 && <p>当前服务没有注册数据源。</p>}
		{catalog.map((source, index) => {
			const observation = observations[index];
			const tooltipId = `${tooltipPrefix}-${source.id}`;
			const categories = sourceCategoryLabel(observation.category, source.id);
			const probeEnabled = source.implemented !== false && source.enabled !== false && source.probeScope !== '';
			const rowChecking = checking && probeEnabled;
			const probe = probeEnabled ? probes.find(item => item.id === source.id) : undefined;
			const probeStatus = rowChecking ? 'checking' : !probe ? 'unknown' : probeRetained ? 'retained' : sourceProbeExpired(probe, now) ? 'expired' : probe.status;
			const probeLabel = !probeEnabled ? source.implemented === false ? '尚未实现' : source.enabled === false ? '当前未启用' : '暂无代表接口检测' : rowChecking ? '检测中…' : !probe ? '未主动检测' : probeRetained ? '检测结果未更新 · 上次检测记录' : sourceProbeExpired(probe, now) ? '检测已过期' : probe.status === 'available' ? '代表接口返回有效数据' : '代表接口失败 · 仅此检测范围';
			return <article key={source.id} data-source={source.id} aria-busy={rowChecking}>
				<div><span className="source-catalog-name"><strong>{source.name}</strong><span className="source-help-wrap"><button className="source-help" type="button" aria-label={`${source.name}用途说明`} aria-describedby={tooltipId}><HelpCircle size={15} aria-hidden="true" /></button><span className="source-help-tooltip" id={tooltipId} role="tooltip">用途：{source.usage}。{source.configuration}</span></span></span><em>{sourceIntegrationLabel(source.mode)}{source.enabled === false ? ' · 未启用' : ''}</em></div>
				<small className="source-kind-labels" aria-label={source.implemented === false ? '规划能力' : '已实现能力'}>{source.kinds.map(kind => <b key={kind}>{sourceKindLabel(kind)}</b>)}</small>
				{Boolean(source.capabilities?.length) && <small>能力：{source.capabilities!.map(capability => sourceCategoryLabel(capability, source.id) || capability).join(' · ')}</small>}
				<small>{source.usage}</small>
				<p className={`source-probe-status ${probeStatus}`} role="status">{rowChecking && <LoaderCircle size={13} className="spin" aria-hidden="true" />}{probeLabel}</p>
				{rowChecking ? <small>正在实际请求代表接口，请等待本次结果。</small> : probe ? <>
					<small>检测时间：{new Date(probe.checked_at).toLocaleString('zh-CN')} · 耗时 {probe.latency_ms} ms</small>
					<small>检测用途：{probe.scope}</small>
					<small>{probeRetained || sourceProbeExpired(probe, now) ? '上次检测说明：' : '检测说明：'}{probe.message}</small>
				</> : probeEnabled ? <small>点击刷新检测可获取代表接口的实际结果。</small> : null}
				<p className={`source-observation-status ${observation.status}`}>业务最近观测：{sourceHealthLabel(observation)}{retained && observation.checked_at ? ' · 上次读取记录' : ''}</p>
				{categories && <small>已实现功能范围：{categories}（状态只代表来源最近一次请求，不代表全部功能）</small>}
				<small>{recordsRead || sources.some(item => item.id === source.id && item.status !== 'unconfigured') ? sourceHealthDetail(observation) : '尚未读取到该来源的观测记录'}</small>
				{Boolean(observation.capabilities?.length) && <details className="source-capabilities"><summary>按能力查看实际请求（{observation.capabilities!.length}）</summary>{observation.capabilities!.map(capability => <p key={capability.capability}><strong>{capability.capability}</strong> · {now - Date.parse(capability.checked_at) >= 10 * 60_000 ? '观测已过期' : capability.status === 'available' ? '最近返回有效数据' : capability.status === 'degraded' ? '最近失败，仅影响此能力' : '暂无新观测'}<small>{new Date(capability.checked_at).toLocaleString('zh-CN')} · {capability.message}</small></p>)}</details>}
				{observation.last_success && <small>最近成功：{new Date(observation.last_success).toLocaleString('zh-CN')}</small>}
				{observation.last_failure && <small>最近失败：{new Date(observation.last_failure).toLocaleString('zh-CN')}</small>}
			</article>;
		})}
	</div>;
}
