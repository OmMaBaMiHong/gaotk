//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTokenBankIntegration(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	bank := NewTokenBankRepository(integrationDB)
	accounts := NewAccountRepository(client, integrationDB, nil)
	billing := NewUsageBillingRepository(client, integrationDB)
	owner := mustCreateUser(t, client, &service.User{Balance: 10})
	admin := mustCreateUser(t, client, &service.User{Role: service.RoleAdmin})
	consumer := mustCreateUser(t, client, &service.User{Balance: 100})
	group := mustCreateGroup(t, client, &service.Group{Name: "rental-deepseek-" + uuid.NewString(), Platform: service.PlatformDeepseek})
	secondGroup := mustCreateGroup(t, client, &service.Group{Name: "savings-second-" + uuid.NewString(), Platform: service.PlatformDeepseek})
	channelRepo := &channelRepository{db: integrationDB}
	config := func(bps int, enabled bool) map[string]any {
		return map[string]any{"token_savings": map[string]any{"enabled": enabled, "owner_share_bps": bps, "admin_user_id": admin.ID, "receiving_group_ids": []int64{group.ID}}}
	}
	channel := &service.Channel{Name: "savings-" + uuid.NewString(), Status: service.StatusActive, GroupIDs: []int64{group.ID}, FeaturesConfig: config(8000, true)}
	require.NoError(t, channelRepo.Create(ctx, channel))
	secondConfig := config(3000, true)
	secondConfig["token_savings"].(map[string]any)["receiving_group_ids"] = []int64{secondGroup.ID}
	secondChannel := &service.Channel{Name: "savings-second-" + uuid.NewString(), Status: service.StatusActive, GroupIDs: []int64{secondGroup.ID}, FeaturesConfig: secondConfig}
	require.NoError(t, channelRepo.Create(ctx, secondChannel))
	a := &service.Account{Name: "rented", Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-only-not-real"}, OwnerUserID: &owner.ID, Status: service.StatusActive, Schedulable: true, Concurrency: 1}
	require.NoError(t, accounts.(service.AccountDuplicateRepository).CreateWithAccountGroups(ctx, a, []service.AccountGroup{{GroupID: group.ID, Priority: 50}, {GroupID: secondGroup.ID, Priority: 50}}))
	var err error
	a, err = accounts.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.NotNil(t, a.Rental)
	require.True(t, a.IsSchedulable())
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: consumer.ID, GroupID: &group.ID, Key: "sk-rental-" + uuid.NewString()})
	balance := func(id int64) string {
		var value string
		require.NoError(t, integrationDB.QueryRow(`SELECT balance::text FROM users WHERE id=$1`, id).Scan(&value))
		return value
	}
	command := func(amount float64) *service.UsageBillingCommand {
		return &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, UserID: consumer.ID, AccountID: a.ID, AccountType: a.Type, Model: "deepseek-chat", InputTokens: 100, OutputTokens: 50, BalanceCost: amount, Rental: a.Rental.ForGroup(group.ID)}
	}

	t.Run("atomic_exact_split_retry_and_ledger_guard", func(t *testing.T) {
		cmd := command(1.25)
		result, err := billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.True(t, result.Applied)
		require.ElementsMatch(t, []int64{owner.ID, admin.ID}, result.RevenueUserIDs)
		require.Equal(t, "98.75000000", balance(consumer.ID))
		require.Equal(t, "11.00000000", balance(owner.ID))
		require.Equal(t, "0.25000000", balance(admin.ID))
		result, err = billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.False(t, result.Applied)
		conflict := *cmd
		conflict.RequestFingerprint = ""
		changed := *cmd.Rental
		changed.ChannelID = secondChannel.ID
		conflict.Rental = &changed
		_, err = billing.Apply(ctx, &conflict)
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)

		page, err := bank.Revenue(ctx, owner.ID, a.ID, "", 20, 0)
		require.NoError(t, err)
		require.Equal(t, 1, page.Total)
		require.Equal(t, 1.0, page.Items[0].OwnerAmount)
		showcase, err := bank.Showcase(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, showcase.Recent)
		require.Equal(t, 1.0, showcase.Recent[0].Amount, "showcase publishes the owner credit, not the 1.25 consumer bill")
		// Even if a dedup marker is lost, the independent ledger blocks a second
		// credit and rolls back the consumer deduction already made in this tx.
		_, err = integrationDB.Exec(`DELETE FROM usage_billing_dedup WHERE request_id=$1`, cmd.RequestID)
		require.NoError(t, err)
		_, err = billing.Apply(ctx, cmd)
		require.Error(t, err)
		require.Equal(t, "98.75000000", balance(consumer.ID))
		require.Equal(t, "11.00000000", balance(owner.ID))
		require.Equal(t, "0.25000000", balance(admin.ID))
		var n int
		require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1`, cmd.RequestID).Scan(&n))
		require.Zero(t, n)
	})
	t.Run("multiple_channels_and_captured_snapshot", func(t *testing.T) {
		require.Len(t, a.Rental.Channels, 2)
		_, err := bank.Revenue(ctx, consumer.ID, a.ID, "", 20, 0)
		require.ErrorIs(t, err, service.ErrRentalNotFound)
		cmd := command(10)
		cmd.AccountQuotaCost = 6
		result, err := billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.True(t, result.Applied)
		var bill, earned, adminEarned string
		var channelID int64
		require.NoError(t, integrationDB.QueryRow(`SELECT channel_id,bill_amount::text,owner_amount::text,admin_amount::text FROM account_revenue_ledger WHERE request_id=$1`, cmd.RequestID).Scan(&channelID, &bill, &earned, &adminEarned))
		require.Equal(t, channel.ID, channelID)
		require.Equal(t, "10.00000000", bill)
		require.Equal(t, "8.00000000", earned)
		require.Equal(t, "2.00000000", adminEarned)

		cmd = command(10)
		cmd.Rental = a.Rental.ForGroup(secondGroup.ID)
		_, err = billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.NoError(t, integrationDB.QueryRow(`SELECT channel_id,owner_amount::text FROM account_revenue_ledger WHERE request_id=$1`, cmd.RequestID).Scan(&channelID, &earned))
		require.Equal(t, secondChannel.ID, channelID)
		require.Equal(t, "3.00000000", earned)

		channel.FeaturesConfig = config(5000, true)
		require.NoError(t, channelRepo.Update(ctx, channel))
		cmd = command(1)
		_, err = billing.Apply(ctx, cmd)
		require.NoError(t, err)
		var share int
		require.NoError(t, integrationDB.QueryRow(`SELECT owner_share_bps FROM account_revenue_ledger WHERE request_id=$1`, cmd.RequestID).Scan(&share))
		require.Equal(t, 8000, share, "in-flight request retains captured channel settings")
		fresh, err := accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, 5000, fresh.Rental.ForGroup(group.ID).OwnerShareBPS)
		channel.FeaturesConfig = config(8000, false)
		require.NoError(t, channelRepo.Update(ctx, channel))
		fresh, err = accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.True(t, fresh.IsSchedulable(), "channel sharing switch does not change original account scheduling")
		require.Nil(t, fresh.Rental.ForGroup(group.ID))
		require.NotNil(t, fresh.Rental.ForGroup(secondGroup.ID))
		channel.FeaturesConfig = config(8000, true)
		require.NoError(t, channelRepo.Update(ctx, channel))
	})
	t.Run("disabled_channel_stops_new_sharing_but_preserves_inflight_snapshot", func(t *testing.T) {
		captured := a.Rental.ForGroup(group.ID)
		require.NotNil(t, captured)
		channel.Status = "disabled"
		require.NoError(t, channelRepo.Update(ctx, channel))
		fresh, err := accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.Nil(t, fresh.Rental.ForGroup(group.ID))
		require.NotNil(t, fresh.Rental.ForGroup(secondGroup.ID))
		require.True(t, fresh.IsSchedulable(), "channel status does not change original account scheduling")
		require.True(t, captured.Enabled)
		require.Equal(t, 8000, captured.OwnerShareBPS)
		cmd := command(1)
		cmd.Rental = captured
		_, err = billing.Apply(ctx, cmd)
		require.NoError(t, err)
		var ownerAmount string
		require.NoError(t, integrationDB.QueryRow(`SELECT owner_amount::text FROM account_revenue_ledger WHERE request_id=$1`, cmd.RequestID).Scan(&ownerAmount))
		require.Equal(t, "0.80000000", ownerAmount)
		channel.Status = service.StatusActive
		require.NoError(t, channelRepo.Update(ctx, channel))
	})
	t.Run("malformed_channel_snapshot_fails_account_load", func(t *testing.T) {
		_, err := integrationDB.Exec(`UPDATE channels SET features_config=jsonb_set(features_config,'{token_savings,owner_share_bps}','10001'::jsonb) WHERE id=$1`, channel.ID)
		require.NoError(t, err)
		_, err = accounts.GetByID(ctx, a.ID)
		require.Error(t, err, "loading a broken sharing snapshot must not silently skip revenue")
		require.NoError(t, channelRepo.Update(ctx, channel))
	})
	t.Run("self_use_and_same_recipient", func(t *testing.T) {
		self := mustCreateUser(t, client, &service.User{Balance: 10})
		cmd := command(1)
		snap := *a.Rental.ForGroup(group.ID)
		snap.OwnerUserID = self.ID
		snap.AdminUserID = self.ID
		cmd.Rental = &snap
		cmd.UserID = self.ID
		result, err := billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.Equal(t, "10.00000000", balance(self.ID))
		require.NotNil(t, result.NewBalance)
		require.Equal(t, 10.0, *result.NewBalance)
		require.Equal(t, []int64{self.ID}, result.RevenueUserIDs)
	})
	t.Run("concurrent_retries_and_reciprocal_users", func(t *testing.T) {
		u := mustCreateUser(t, client, &service.User{Balance: 100})
		v := mustCreateUser(t, client, &service.User{Balance: 100})
		cmd1, cmd2 := command(1), command(1)
		s1, s2 := *a.Rental.ForGroup(group.ID), *a.Rental.ForGroup(group.ID)
		s1.OwnerUserID = v.ID
		s2.OwnerUserID = u.ID
		cmd1.UserID = u.ID
		cmd2.UserID = v.ID
		cmd1.Rental = &s1
		cmd2.Rental = &s2
		cmd1.Normalize()
		cmd2.Normalize()
		var wg sync.WaitGroup
		errs := make(chan error, 20)
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				cmd := *cmd1
				if i%2 == 1 {
					cmd = *cmd2
				}
				_, err := billing.Apply(ctx, &cmd)
				errs <- err
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		require.Equal(t, "99.80000000", balance(u.ID))
		require.Equal(t, "99.80000000", balance(v.ID))
	})
	t.Run("free_and_subscription", func(t *testing.T) {
		cmd := command(0)
		result, err := billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.Empty(t, result.RevenueUserIDs)
		var count int
		require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM account_revenue_ledger WHERE request_id=$1`, cmd.RequestID).Scan(&count))
		require.Zero(t, count)
		sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: consumer.ID, GroupID: group.ID})
		before := balance(consumer.ID)
		cmd = command(0)
		cmd.SubscriptionID = &sub.ID
		cmd.SubscriptionCost = 2
		cmd.BillingType = service.BillingTypeSubscription
		_, err = billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.Equal(t, before, balance(consumer.ID))
		var kind int
		var earned string
		require.NoError(t, integrationDB.QueryRow(`SELECT billing_type,owner_amount::text FROM account_revenue_ledger WHERE request_id=$1`, cmd.RequestID).Scan(&kind, &earned))
		require.Equal(t, 1, kind)
		require.Equal(t, "1.60000000", earned)
	})
	t.Run("overview_safe_and_shadow_inheritance", func(t *testing.T) {
		shadow := &service.Account{Name: "rental-shadow", Platform: a.Platform, Type: a.Type, Credentials: map[string]any{}, ParentAccountID: &a.ID, QuotaDimension: "spark", Status: service.StatusActive, Schedulable: true, Concurrency: 1}
		require.NoError(t, accounts.(service.AccountDuplicateRepository).CreateWithAccountGroups(ctx, shadow, []service.AccountGroup{{GroupID: group.ID}, {GroupID: secondGroup.ID}}))
		inherited, err := accounts.GetByID(ctx, shadow.ID)
		require.NoError(t, err)
		require.NotNil(t, inherited.Rental)
		require.Equal(t, a.ID, inherited.Rental.OwnerAccountID)
		require.Equal(t, owner.ID, inherited.Rental.OwnerUserID)
		require.Len(t, inherited.Rental.Channels, 2)
		cmd := command(1)
		cmd.AccountID = shadow.ID
		cmd.Rental = inherited.Rental.ForGroup(secondGroup.ID)
		_, err = billing.Apply(ctx, cmd)
		require.NoError(t, err)
		var billedAccount, ownerAccount, earnedOwner int64
		require.NoError(t, integrationDB.QueryRow(`SELECT account_id,owner_account_id,owner_user_id FROM account_revenue_ledger WHERE request_id=$1`, cmd.RequestID).Scan(&billedAccount, &ownerAccount, &earnedOwner))
		require.Equal(t, shadow.ID, billedAccount)
		require.Equal(t, a.ID, ownerAccount)
		require.Equal(t, owner.ID, earnedOwner)

		batch, err := accounts.GetByIDs(ctx, []int64{a.ID, shadow.ID})
		require.NoError(t, err)
		require.Len(t, batch, 2)
		for _, account := range batch {
			require.NotNil(t, account.Rental)
		}
		shadows, err := accounts.ListShadowsByParent(ctx, a.ID)
		require.NoError(t, err)
		require.Len(t, shadows, 1)
		require.NotNil(t, shadows[0].Rental)
		var lastEvent int64
		require.NoError(t, integrationDB.QueryRow(`SELECT COALESCE(MAX(id),0) FROM scheduler_outbox`).Scan(&lastEvent))
		require.NoError(t, channelRepo.Update(ctx, channel))
		var refreshed int
		require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(DISTINCT account_id) FROM scheduler_outbox WHERE id>$1 AND account_id IN ($2,$3) AND event_type='account_changed'`, lastEvent, a.ID, shadow.ID).Scan(&refreshed))
		require.Equal(t, 2, refreshed, "channel configuration refreshes parent and shadow scheduler snapshots")

		a.Schedulable = false
		require.NoError(t, accounts.Update(ctx, a))
		inherited, err = accounts.GetByID(ctx, shadow.ID)
		require.NoError(t, err)
		require.False(t, inherited.IsSchedulable())
		a.Schedulable = true
		require.NoError(t, accounts.Update(ctx, a))
		overview, err := bank.Overview(ctx, owner.ID, "", "", 20, 0)
		require.NoError(t, err)
		require.Equal(t, 1, overview.TotalAccounts)
		require.Len(t, overview.Accounts, 1)
		require.Positive(t, overview.TotalRevenue)
		other, err := bank.Overview(ctx, consumer.ID, "", "", 20, 0)
		require.NoError(t, err)
		require.Empty(t, other.Accounts)
		t.Logf("Verified original account %d: earnings %.8f across channels; independent ledger preserved", a.ID, overview.TotalRevenue)
	})
}
