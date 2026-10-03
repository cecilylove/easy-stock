# Live Tests

Normal tests do not hit public data sources:

```bash
cd backend
go test ./...
```

Live tests are opt-in. The routine command below selects current business paths
and avoids the legacy EastMoney stock K-line diagnostic:

```bash
cd backend
A_STOCK_LIVE_TEST=1 go test ./internal/httpapi ./internal/providers -run '^(TestLiveAPI.*|TestLiveSina.*|TestLiveCLS.*)$' -count=1 -v
```

On Windows PowerShell, from the repository root:

```powershell
$env:A_STOCK_LIVE_TEST = '1'
Push-Location backend
try {
  & ../.runtime/tools/go/bin/go.exe test ./internal/httpapi ./internal/providers -run '^(TestLiveAPI.*|TestLiveSina.*|TestLiveCLS.*)$' -count=1 -v
} finally {
  Pop-Location
  Remove-Item Env:A_STOCK_LIVE_TEST
}
```

Use an installed `go` if the repository runtime has not been prepared. These are
available commands, not a statement that they were executed in a particular run.

Current live checks:

| Test | Expected Real Data |
| --- | --- |
| `TestLiveAPIRealtimeReturnsRealQuotesAcrossMarkets` | Unified HTTP API returns positive realtime prices for `000001.SZ`, `600000.SH`, and `300750.SZ`. |
| `TestLiveAPIKLineReturnsRealBarsWithFallback` | Unified HTTP API returns daily K-line bars with source evidence through current routing (source-default prefers Sina, with supported Tencent fallback; explicit adjustment uses Tencent). The historical test name does not verify every fallback or adjustment mode. |
| `TestLiveAPINewsReturnsRealItems` | Unified HTTP API returns CLS news items with source evidence. |
| `TestLiveSinaRealtimeReturnsQuote` | `000001.SZ` realtime quote from Sina has a positive price. |
| `TestLiveSinaKLineReturnsBars` | `000001.SZ` daily K-line from Sina returns at least one bar with positive close. |
| `TestLiveCLSNewsReturnsItems` | CLS latest news returns at least one item. |

## Company-source migration check

The opt-in company check uses the current registered Sina business/fundamental
adapters and default company routes, with in-memory stores and deterministic
fixtures for unrelated price/news/theme data. It verifies quick-analysis HTTP,
research disclosure evidence and the holding shared analyzer without invoking AI
or reading personal settings. It permits only the Sina official hosts, bounds
requests and spaces them by at least 1.2 seconds; ordinary tests skip it.

```powershell
# repository root; this is an explicit public-network sample, not SLA certification
$env:A_STOCK_LIVE_COMPANY_TEST = '1'
try {
  & ./.runtime/tools/go/bin/go.exe -C backend test ./internal/httpapi -run '^TestLiveSinaCompanyMigrationReachesHTTPResearchAndHolding$' -count=1 -v
} finally {
  Remove-Item Env:A_STOCK_LIVE_COMPANY_TEST
}
```

Optional `A_STOCK_COMPANY_EVIDENCE_DIR` names an existing ignored directory for
public sample output; do not point it at installed-version user data. This check
validates company-source wiring, not the unrelated synthetic price data, AI
synthesis quality, every security's coverage or long-term availability.

## Notes

External financial endpoints can fail because of network restrictions, rate limits, anti-bot changes, or upstream schema changes. A live test failure should be treated as a data-source health signal, not necessarily as a deterministic code regression.

`TestLiveEastMoneyKLineReportsHealthSignal` still exists as a legacy standalone
diagnostic. EastMoney stock K-line is retired from current business routing; do
not include this diagnostic in routine business validation or interpret its
logged failure and PASS as a healthy source or an active API fallback.

HTTP success alone does not verify field meaning, market coverage, or data
freshness. Representative source probes cover only their stated scope. Validate
auction separately from the directory probe, compare margin histories only at
the same market coverage, and do not treat native billboard seat values as
counts without a verified mapping. For the latest recorded sample and its
limitations, see [the 2026-10-03 functional validation](../../docs/data-source-functional-validation-2026-10-03.md).
