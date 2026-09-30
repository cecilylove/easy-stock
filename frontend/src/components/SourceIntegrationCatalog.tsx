import { sourceIntegrationLabel, sourceIntegrations } from '../lib/source-integrations';

export function SourceIntegrationCatalog() {
	return <div className="source-integration-catalog" aria-label="数据源接入清单">
		<p className="settings-field-note">接入状态和最近可用性分别判断。内置来源按功能自动选择；当前没有任意切换供应商的开关。保存预留凭据不会启用新来源或改变降级链路。</p>
		{sourceIntegrations.map(source => <article key={source.id}><div><strong>{source.name}</strong><em>{sourceIntegrationLabel(source.mode)}</em></div><small>{source.usage}</small><p>{source.configuration}</p></article>)}
	</div>;
}
