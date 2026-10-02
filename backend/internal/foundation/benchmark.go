package foundation

// BenchmarkIndexID maps canonical research benchmark identities independently
// of any supplier's native quote codes.
func BenchmarkIndexID(symbol string) (string, bool) {
	normalized, err := NormalizeSymbol(symbol)
	if err != nil {
		return "", false
	}
	ids := map[string]string{"000001.SH": "sse", "000300.SH": "csi300", "000016.SH": "sse50", "000852.SH": "csi1000", "000688.SH": "star50", "399001.SZ": "szse", "399006.SZ": "chinext"}
	id, ok := ids[normalized.Canonical]
	return id, ok
}
