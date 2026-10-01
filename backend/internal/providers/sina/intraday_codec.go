package sina

import (
	"fmt"
	"math"
	"time"
)

type historicalMinutePoint struct {
	Time         string
	Price        float64
	AveragePrice float64
	Volume       float64 // Shares, not lots; each point contains incremental volume.
}

type historicalMinuteDay struct {
	Date          string
	PreviousClose float64
	Points        []historicalMinutePoint
}

// The monthly archive uses six-bit characters, read least significant bit first.
// This implements only its minute-record format (136), without executing the
// archive or depending on the browser SDK and its unrelated formats.
type minuteBitReader struct {
	values []byte
	pos    int
	err    error
}

func newMinuteBitReader(encoded string) (*minuteBitReader, error) {
	if len(encoded) == 0 || len(encoded) > 16384 {
		return nil, fmt.Errorf("invalid historical minute payload size")
	}
	r := &minuteBitReader{values: make([]byte, len(encoded))}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := range encoded {
		found := false
		for j := range alphabet {
			if encoded[i] == alphabet[j] {
				r.values[i], found = byte(j), true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("invalid historical minute character at %d", i)
		}
	}
	return r, nil
}

func (r *minuteBitReader) bits(width int, signed bool) int64 {
	if r.err != nil {
		return 0
	}
	// Integers above 53 bits cannot be represented exactly in source quantities.
	if width < 0 || width > 53 || r.pos+width > len(r.values)*6 {
		r.err = fmt.Errorf("invalid or truncated historical minute bit field")
		return 0
	}
	var value int64
	for bit := 0; bit < width; bit++ {
		value |= int64((r.values[r.pos/6]>>uint(r.pos%6))&1) << uint(bit)
		r.pos++
	}
	if signed && width > 0 && value&(int64(1)<<uint(width-1)) != 0 {
		value -= int64(1) << uint(width)
	}
	return value
}

func (r *minuteBitReader) flag() bool { return r.bits(1, false) != 0 }

func (r *minuteBitReader) widthDelta() int {
	positive := r.flag()
	for count := 1; count <= 20 && r.err == nil; count++ {
		if !r.flag() {
			if positive {
				return count
			}
			return -count
		}
	}
	r.err = fmt.Errorf("historical minute width delta exceeds limit")
	return 0
}

func (r *minuteBitReader) finished(record, marker int) bool {
	character := r.pos / 6
	return character >= len(r.values) || character == len(r.values)-1 && (marker^record)&7 == 0
}

func decodeHistoricalMinute(encoded string) (historicalMinuteDay, error) {
	var result historicalMinuteDay
	r, err := newMinuteBitReader(encoded)
	if err != nil {
		return result, err
	}
	kind, version := r.bits(12, false), 63^r.bits(6, false)
	if r.err != nil || kind != 136 || version > 1 {
		return result, fmt.Errorf("unsupported historical minute format %d version %d", kind, version)
	}
	dayOffset := r.bits(18, true)
	date := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7657+int(dayOffset))
	if date.Year() < 1990 || date.Year() > 2100 || date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
		return result, fmt.Errorf("invalid historical minute date")
	}
	fieldWidth, decimalWidth := 4, 3
	if version == 0 {
		fieldWidth, decimalWidth = 3, 5
	}
	// Separate widths encode average-price corrections, price deltas and volume.
	widths := [3]int{int(r.bits(fieldWidth, false)), int(r.bits(fieldWidth, false)), int(r.bits(fieldWidth, false))}
	variableWidths, mixedVolume, explicitZero := r.flag(), r.flag(), r.flag()
	precision := r.bits(decimalWidth, false)
	marker, previousWidth := 2, 5
	if version == 1 {
		marker, previousWidth = int(r.bits(3, false)), int(r.bits(3, false))
	}
	previousRaw := r.bits(6*previousWidth, false)
	if r.err != nil || precision > 6 || previousRaw <= 0 {
		return result, fmt.Errorf("invalid historical minute price header")
	}
	divisor := math.Pow10(int(precision))
	previousClose := float64(previousRaw) / divisor
	if previousClose > 1e7 {
		return result, fmt.Errorf("historical minute price exceeds limit")
	}
	priceRaw := previousRaw
	var volumeSum, priceVolumeSum, averageCorrection float64
	points := make([]historicalMinutePoint, 0, 243)
	for record := 0; !r.finished(record, marker); record++ {
		if record >= 243 {
			return result, fmt.Errorf("historical minute record count exceeds limit")
		}
		changeWidths := !variableWidths || r.flag()
		var volume, priceDelta, averageDelta int64
		averagePresent := false
		// Field order on the wire is volume, price, average correction.
		for field := 0; field < 3; field++ {
			widthIndex := [3]int{2, 1, 0}[field]
			if changeWidths && r.flag() {
				widths[widthIndex] += r.widthDelta()
			}
			if widths[widthIndex] < 0 || widths[widthIndex] > 15 {
				return result, fmt.Errorf("historical minute field width exceeds limit")
			}
			inShares := field != 0 || !mixedVolume || r.flag()
			bitCount := 3 * widths[widthIndex]
			if field == 0 && inShares {
				bitCount += 7
			}
			value := r.bits(bitCount, field != 0)
			if !inShares {
				if value > (1<<53)/100 {
					return result, fmt.Errorf("historical minute volume exceeds limit")
				}
				value *= 100
			}
			if r.err != nil {
				return result, r.err
			}
			switch field {
			case 0:
				volume = value
				if volume == 0 && record < 241 && (!explicitZero || !r.flag()) {
					field = 3 // Zero-volume records can omit their price fields.
				}
			case 1:
				priceDelta = value
			case 2:
				averageDelta, averagePresent = value, true
			}
		}
		priceRaw += priceDelta
		if priceRaw < 0 || priceRaw > 1<<53 || volume < 0 {
			return result, fmt.Errorf("invalid historical minute numeric value")
		}
		if version == 0 {
			averageCorrection = float64(averageDelta)
		} else {
			averageCorrection += float64(averageDelta)
		}
		volumeSum += float64(volume)
		priceVolumeSum += float64(volume) * float64(priceRaw)
		point := historicalMinutePoint{Price: float64(priceRaw) / divisor, Volume: float64(volume)}
		if !averagePresent {
			point.AveragePrice = point.Price
			if len(points) > 0 {
				point.AveragePrice = points[len(points)-1].AveragePrice
			}
		} else if volumeSum > 0 {
			// Round the volume-weighted price to the source's mill precision,
			// then apply the independently encoded average-price correction.
			rounded := math.Floor((priceVolumeSum*(2000/divisor) + volumeSum) / volumeSum)
			point.AveragePrice = (math.Floor(rounded/2) + averageCorrection) / 1000
		} else {
			point.AveragePrice = point.Price + averageCorrection/1000
		}
		if !finiteMinuteNumber(point.Price) || !finiteMinuteNumber(point.AveragePrice) || !finiteMinuteNumber(volumeSum) || volumeSum > 1<<53 {
			return result, fmt.Errorf("historical minute numeric overflow")
		}
		points = append(points, point)
	}
	if r.err != nil || len(points) != 243 {
		return result, fmt.Errorf("incomplete historical minute records")
	}
	// The first decoded record stores the date/previous-close metadata, with
	// zero price and volume. Remaining 242 records include both session starts.
	if points[0].Price != 0 || points[0].Volume != 0 {
		return result, fmt.Errorf("invalid historical minute metadata record")
	}
	points = points[1:]
	for i := range points {
		if points[i].Price <= 0 || points[i].Price > 1e7 || points[i].AveragePrice <= 0 || points[i].AveragePrice > 1e7 {
			return result, fmt.Errorf("invalid historical minute price")
		}
		minute := 9*60 + 30 + i
		if i >= 121 {
			minute = 13*60 + i - 121
		}
		points[i].Time = fmt.Sprintf("%02d:%02d", minute/60, minute%60)
	}
	// Preserve the real 11:30 volume. The zero-volume 13:00 duplicate can be
	// omitted losslessly; the browser SDK instead drops the real 11:30 record.
	if points[121].Volume == 0 && points[121].Price == points[120].Price && points[121].AveragePrice == points[120].AveragePrice {
		points = append(points[:121], points[122:]...)
	}
	return historicalMinuteDay{Date: date.Format("2006-01-02"), PreviousClose: previousClose, Points: points}, nil
}

func finiteMinuteNumber(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
