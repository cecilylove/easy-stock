package foundation

import "testing"

func TestSeatSpellingNormalizationDoesNotConfirmIdentity(t *testing.T) {
	a := NormalizeBillboardSeatName("中国 银河证券股份有限公司（北京）")
	b := NormalizeBillboardSeatName("中国银河证券有限责任公司(北京)")
	if a != b || a == NormalizeBillboardSeatName("中国银河证券(上海)") {
		t.Fatalf("spelling normalization %q %q", a, b)
	}
}
