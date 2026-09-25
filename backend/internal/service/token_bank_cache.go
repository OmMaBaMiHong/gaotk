package service

import (
	"context"
	"log/slog"
)

// Credits bypass recharge/affiliate side effects. Only cached balances and auth
// snapshots need refreshing after the shared transaction commits.
func finalizeRentalRevenue(ctx context.Context, p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	if result == nil || len(result.RevenueUserIDs) == 0 {
		return
	}
	ids := append([]int64{p.User.ID}, result.RevenueUserIDs...)
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if deps.billingCacheService != nil {
			if err := deps.billingCacheService.InvalidateUserBalance(ctx, id); err != nil {
				slog.Error("rental balance cache invalidation failed", "user_id", id, "error", err)
			}
		}
		if invalidator, ok := p.APIKeyService.(interface{ InvalidateAuthCacheByUserID(context.Context, int64) }); ok {
			invalidator.InvalidateAuthCacheByUserID(ctx, id)
		}
	}
}
