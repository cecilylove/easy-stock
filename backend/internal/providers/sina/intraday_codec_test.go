package sina

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

func minuteFixtures(t *testing.T) map[string]string {
	t.Helper()
	body, err := os.ReadFile("testdata/historical_minute.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]string
	if err := json.Unmarshal(body, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func TestDecodeHistoricalMinuteRealArchive(t *testing.T) {
	// Public price-data fixtures from Sina's sz000001/2026/09.js archive.
	// Volume checksums and closes were independently checked against daily bars.
	fixtures := minuteFixtures(t)
	tests := []struct {
		date                                                 string
		previous, open, close, shares, middayShares, average float64
	}{
		{"2026-09-18", 11.61, 11.59, 11.70, 85303779, 46200, 11.720},
		{"2026-09-23", 11.71, 11.68, 11.60, 90672565, 39900, 11.599},
		{"2026-09-30", 11.35, 11.36, 11.57, 104535745, 169600, 11.534},
	}
	for _, tc := range tests {
		t.Run(tc.date, func(t *testing.T) {
			d, err := decodeHistoricalMinute(fixtures[tc.date])
			if err != nil {
				t.Fatal(err)
			}
			if d.Date != tc.date || d.PreviousClose != tc.previous || len(d.Points) != 241 {
				t.Fatalf("invalid date/header/count: %+v", d)
			}
			first, midday, afternoon, last := d.Points[0], d.Points[120], d.Points[121], d.Points[240]
			if first.Time != "09:30" || first.Price != tc.open || midday.Time != "11:30" || midday.Volume != tc.middayShares || afternoon.Time != "13:01" || last.Time != "15:00" || last.Price != tc.close || math.Abs(last.AveragePrice-tc.average) > 1e-9 {
				t.Fatalf("invalid grid/prices: first=%+v midday=%+v afternoon=%+v last=%+v", first, midday, afternoon, last)
			}
			var shares float64
			for i, p := range d.Points {
				if p.Price <= 0 || p.AveragePrice <= 0 || p.Volume < 0 || i > 0 && p.Time <= d.Points[i-1].Time {
					t.Fatalf("invalid point %d: %+v", i, p)
				}
				shares += p.Volume
			}
			if shares != tc.shares {
				t.Fatalf("volume shares=%v want %v", shares, tc.shares)
			}
		})
	}
}

func TestDecodeHistoricalMinuteRejectsMalformed(t *testing.T) {
	valid := minuteFixtures(t)["2026-09-18"]
	for _, input := range []string{"", "IC+!", "not=base64", valid[:len(valid)/2], valid[:len(valid)-8], strings.Repeat("A", 16385), valid + strings.Repeat("A", 100)} {
		if _, err := decodeHistoricalMinute(input); err == nil {
			t.Fatalf("accepted malformed payload length %d", len(input))
		}
	}
	// Reject every truncated prefix, including strings that end on padding bits.
	for end := 1; end < len(valid); end++ {
		if _, err := decodeHistoricalMinute(valid[:end]); err == nil {
			t.Fatalf("accepted truncated prefix %d/%d", end, len(valid))
		}
	}
}

func TestMinuteBitReaderSignedAndBounds(t *testing.T) {
	r, err := newMinuteBitReader("/B")
	if err != nil {
		t.Fatal(err)
	}
	if v := r.bits(6, true); v != -1 {
		t.Fatalf("signed value=%d", v)
	}
	if v := r.bits(6, false); v != 1 {
		t.Fatalf("unsigned value=%d", v)
	}
	r.bits(1, false)
	if r.err == nil {
		t.Fatal("did not reject EOF")
	}
	r, _ = newMinuteBitReader(strings.Repeat("/", 20))
	r.bits(54, false)
	if r.err == nil {
		t.Fatal("did not reject precision overflow")
	}
	r, _ = newMinuteBitReader(strings.Repeat("/", 20))
	r.widthDelta()
	if r.err == nil {
		t.Fatal("did not reject unbounded width delta")
	}
}

func TestDecodeHistoricalMinuteRejectsUnsupportedTypeAndVersion(t *testing.T) {
	valid := minuteFixtures(t)["2026-09-18"]
	// First 12 bits encode type, followed by the complemented 6-bit version.
	for _, prefix := range []string{"AC+", "ICA"} {
		if _, err := decodeHistoricalMinute(prefix + valid[3:]); err == nil {
			t.Fatalf("accepted unsupported format %s", prefix)
		}
	}
}

func replaceMinuteBits(encoded string, start, width int, value int64) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	data := []byte(encoded)
	for bit := 0; bit < width; bit++ {
		position := start + bit
		character := strings.IndexByte(alphabet, data[position/6])
		mask := 1 << uint(position%6)
		character = character &^ mask
		if value&(int64(1)<<uint(bit)) != 0 {
			character |= mask
		}
		data[position/6] = alphabet[character]
	}
	return string(data)
}

func TestDecodeHistoricalMinuteRejectsNumericHeaderOverflow(t *testing.T) {
	valid := minuteFixtures(t)["2026-09-18"]
	// In version 1 the three-bit price precision follows the 36-bit prefix,
	// three four-bit field widths, and three flags. Precision seven is invalid.
	if _, err := decodeHistoricalMinute(replaceMinuteBits(valid, 51, 3, 7)); err == nil {
		t.Fatal("accepted overflowing price precision")
	}
	// The subsequent three-bit previous-price width controls an unsigned value;
	// max-width/max-value must fail instead of producing an enormous price.
	oversized := replaceMinuteBits(valid, 57, 3, 7)
	oversized = replaceMinuteBits(oversized, 60, 42, (1<<42)-1)
	if _, err := decodeHistoricalMinute(oversized); err == nil {
		t.Fatal("accepted overflowing previous price")
	}
}

func FuzzDecodeHistoricalMinute(f *testing.F) {
	body, err := os.ReadFile("testdata/historical_minute.json")
	if err != nil {
		f.Fatal(err)
	}
	var fixtures map[string]string
	if err := json.Unmarshal(body, &fixtures); err != nil {
		f.Fatal(err)
	}
	for _, fixture := range fixtures {
		f.Add(fixture)
	}
	f.Add("IC+AAAA")
	f.Fuzz(func(t *testing.T, encoded string) {
		day, err := decodeHistoricalMinute(encoded)
		if err != nil {
			return
		}
		if day.Date == "" || day.PreviousClose <= 0 || len(day.Points) < 241 || len(day.Points) > 242 {
			t.Fatal("accepted invalid minute day")
		}
		for _, point := range day.Points {
			if point.Price <= 0 || point.AveragePrice <= 0 || point.Volume < 0 || !finiteMinuteNumber(point.Price) || !finiteMinuteNumber(point.AveragePrice) || !finiteMinuteNumber(point.Volume) {
				t.Fatal("accepted invalid numeric point")
			}
		}
	})
}
