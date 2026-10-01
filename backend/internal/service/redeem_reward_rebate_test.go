//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBookRewardNeverEntersAffiliateRecharge(t *testing.T) {
	repo := &settingRepoStub{values: map[string]string{SettingKeyAffiliateEnabled: "true"}}
	svc := &RedeemService{affiliateService: &AffiliateService{settingService: &SettingService{settingRepo: repo}}}
	svc.tryAccrueAffiliateRebateForRedeem(context.Background(), 1, &RedeemCode{Type: AdjustmentTypeTemplateUpload, Value: 5})
	require.Zero(t, repo.getValueCalls)
	svc.tryAccrueAffiliateRebateForRedeem(context.Background(), 1, &RedeemCode{Type: RedeemTypeBalance, Value: 5})
	require.Positive(t, repo.getValueCalls)
}
