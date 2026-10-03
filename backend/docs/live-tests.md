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
