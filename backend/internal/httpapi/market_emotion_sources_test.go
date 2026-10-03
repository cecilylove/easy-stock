package httpapi

import (
	"easy-stock/backend/internal/foundation"
	"strings"
	"testing"
)

func TestEmotionSourceUsesActualInputsNotRetiredPrice(t *testing.T) {
	source := marketEmotionInputSources([]foundation.LimitUpEvent{{Meta: foundation.SourceMeta{Source: "duanxianxia:pool"}}}, marketEmotionSidePools{broken: []foundation.MarketLimitEvent{{Meta: foundation.SourceMeta{Source: "eastmoney:broken"}}}}, map[string]foundation.KLine{"600001.SH": {Meta: foundation.SourceMeta{Source: "tencent"}}}, nil)
	for _, expected := range []string{"涨停:duanxianxia:pool", "炸板:eastmoney:broken", "日K:tencent"} {
		if !strings.Contains(source, expected) {
			t.Fatalf("%s missing from %s", expected, source)
		}
	}
	if strings.Contains(source, "东方财富日K") || strings.Contains(source, "日K:eastmoney") {
		t.Fatal(source)
	}
	if unknown := marketEmotionInputSources(nil, marketEmotionSidePools{}, nil, nil); !strings.Contains(unknown, "未记录") {
		t.Fatal(unknown)
	}
}
