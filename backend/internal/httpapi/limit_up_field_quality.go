package httpapi

import (
	"easy-stock/backend/internal/foundation"
	"fmt"
	"math"
)

func hasUnknownLadderStreak(day limitUpLadderDay) bool {
	for _, field := range day.MissingFields {
		if field == "streak" {
			return true
		}
	}
	return false
}
func validateEmotionEventFields(events []foundation.LimitUpEvent) error {
	for _, event := range events {
		if !event.Meta.FieldsKnown {
			continue
		} // Historical fixtures retain their old contract.
		for _, field := range []string{"streak", "open_count", "amount", "change_percent"} {
			if !foundation.FieldAvailable(event.Meta, field) {
				return fmt.Errorf("emotion event %s missing %s; unknown cannot be scored as zero", event.Symbol, field)
			}
		}
		if event.Streak < 1 || event.OpenCount < 0 || math.IsNaN(event.Amount) || math.IsInf(event.Amount, 0) || event.Amount < 0 {
			return fmt.Errorf("emotion event %s invalid required fields", event.Symbol)
		}
	}
	return nil
}
