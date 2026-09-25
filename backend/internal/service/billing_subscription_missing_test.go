package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type missingBillingSubscriptionRepo struct {
	UserSubscriptionRepository
	err error
}

func (r *missingBillingSubscriptionRepo) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, r.err
}

func TestBillingSubscriptionMissingIsNotServiceFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		repoErr  error
		want     error
		failures int
	}{
		{"expired_or_missing", ErrSubscriptionNotFound, ErrSubscriptionInvalid, 0},
		{"database_unavailable", errors.New("database unavailable"), ErrBillingServiceUnavailable, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			breaker := newBillingCircuitBreaker(config.CircuitBreakerConfig{Enabled: true})
			svc := &BillingCacheService{subRepo: &missingBillingSubscriptionRepo{err: tc.repoErr}, circuitBreaker: breaker}
			err := svc.checkSubscriptionEligibility(context.Background(), 1, &Group{ID: 2}, nil)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.failures, breaker.failures)
		})
	}
}
