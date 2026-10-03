package runtime

import (
	"context"
	"time"
)

// PrimaryBudget reserves time for a sequential fallback without extending the
// parent deadline. Short requests reserve one third of their remaining time;
// longer requests reserve at most reserve. It does not perform any retries.
// The caller must bound the overall request and invoke fallback under that same
// parent, not under the primary's child context.
func PrimaryBudget(ctx context.Context, maximum, reserve time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && reserve > 0 {
		remaining := max(time.Until(deadline), 0)
		reserved := min(reserve, remaining/3)
		available := remaining - reserved
		if maximum <= 0 || available < maximum {
			maximum = available
		}
		if maximum <= 0 {
			return context.WithDeadline(ctx, deadline)
		}
	}
	return Budget(ctx, maximum)
}
