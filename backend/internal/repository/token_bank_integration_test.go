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
	wrongGroup := mustCreateGroup(t, client, &service.Group{Name: "rental-wrong-" + uuid.NewString(), Platform: service.PlatformOpenAI})
	p := service.RentalPolicy{Platform: service.PlatformDeepseek, GroupID: group.ID, AdminUserID: admin.ID, OwnerShareBPS: 8000, Enabled: true}
	require.NoError(t, bank.SavePolicy(ctx, p))
	policies, err := bank.Policies(ctx)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	p = policies[0]
	identity := uuid.NewString()
	a := &service.Account{Name: "rented", Platform: p.Platform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-only-not-real"}, OwnerUserID: &owner.ID, RentalPolicyID: &p.ID, RentalIdentity: &identity, RentalStatus: service.StatusActive, Status: service.StatusActive, Schedulable: true, Concurrency: 1}
	require.NoError(t, accounts.(service.AccountDuplicateRepository).CreateWithAccountGroups(ctx, a, []service.AccountGroup{{GroupID: group.ID, Priority: 50}}))
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
		return &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: key.ID, UserID: consumer.ID, AccountID: a.ID, AccountType: a.Type, Model: "deepseek-chat", InputTokens: 100, OutputTokens: 50, BalanceCost: amount, Rental: a.Rental}
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
		page, err := bank.Revenue(ctx, owner.ID, a.ID, "", 20, 0)
		require.NoError(t, err)
		require.Equal(t, 1, page.Total)
		require.Equal(t, 1.0, page.Items[0].OwnerAmount)
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
	t.Run("ownership_platform_and_pause_snapshot", func(t *testing.T) {
		require.ErrorIs(t, bank.SetStatus(ctx, consumer.ID, a.ID, "paused"), service.ErrRentalNotFound)
		_, err := bank.Revenue(ctx, consumer.ID, a.ID, "", 20, 0)
		require.ErrorIs(t, err, service.ErrRentalNotFound)
		_, err = integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id,priority) VALUES($1,$2,50)`, a.ID, wrongGroup.ID)
		require.Error(t, err)
		_, err = integrationDB.Exec(`DELETE FROM account_groups WHERE account_id=$1`, a.ID)
		require.Error(t, err, "rentals cannot escape their dedicated pool by clearing groups")
		tx, err := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.Exec(`DELETE FROM account_groups WHERE account_id=$1`, a.ID)
		require.NoError(t, err)
		_, err = tx.Exec(`INSERT INTO account_groups(account_id,group_id,priority) VALUES($1,$2,50)`, a.ID, group.ID)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(), "atomic rebind to the same pool remains supported")
		_, err = integrationDB.Exec(`UPDATE accounts SET platform='openai' WHERE id=$1`, a.ID)
		require.Error(t, err)
		_, err = integrationDB.Exec(`UPDATE accounts SET owner_user_id=$1 WHERE id=$2`, consumer.ID, a.ID)
		require.Error(t, err)
		require.NoError(t, bank.SetStatus(ctx, owner.ID, a.ID, "paused"))
		paused, err := accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.False(t, paused.IsSchedulable())
		p.OwnerShareBPS = 5000
		require.NoError(t, bank.SavePolicy(ctx, p))
		cmd := command(1)
		result, err := billing.Apply(ctx, cmd)
		require.NoError(t, err)
		require.True(t, result.Applied)
		var share int
		require.NoError(t, integrationDB.QueryRow(`SELECT owner_share_bps FROM account_revenue_ledger WHERE request_id=$1`, cmd.RequestID).Scan(&share))
		require.Equal(t, 8000, share)
		require.NoError(t, bank.SetStatus(ctx, owner.ID, a.ID, "active"))
		fresh, err := accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.True(t, fresh.IsSchedulable())
		require.Equal(t, 5000, fresh.Rental.OwnerShareBPS)
		p.OwnerShareBPS = 8000
		require.NoError(t, bank.SavePolicy(ctx, p))
		p.Enabled = false
		require.NoError(t, bank.SavePolicy(ctx, p))
		fresh, err = accounts.GetByID(ctx, a.ID)
		require.NoError(t, err)
		require.False(t, fresh.IsSchedulable())
		p.Enabled = true
		require.NoError(t, bank.SavePolicy(ctx, p))
	})
	t.Run("self_use_and_same_recipient", func(t *testing.T) {
		self := mustCreateUser(t, client, &service.User{Balance: 10})
		cmd := command(1)
		snap := *a.Rental
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
		s1, s2 := *a.Rental, *a.Rental
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
		shadow := &service.Account{Name: "rental-shadow", Platform: p.Platform, Type: a.Type, Credentials: map[string]any{}, ParentAccountID: &a.ID, QuotaDimension: "spark", Status: service.StatusActive, Schedulable: true, Concurrency: 1}
		err := accounts.(service.AccountDuplicateRepository).CreateWithAccountGroups(ctx, shadow, []service.AccountGroup{{GroupID: wrongGroup.ID}})
		require.Error(t, err)
		require.NoError(t, accounts.(service.AccountDuplicateRepository).CreateWithAccountGroups(ctx, shadow, []service.AccountGroup{{GroupID: group.ID}}))
		inherited, err := accounts.GetByID(ctx, shadow.ID)
		require.NoError(t, err)
		require.NotNil(t, inherited.Rental)
		require.Equal(t, a.ID, inherited.Rental.OwnerAccountID)
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
		require.NoError(t, bank.SetStatus(ctx, owner.ID, a.ID, "paused"))
		inherited, err = accounts.GetByID(ctx, shadow.ID)
		require.NoError(t, err)
		require.False(t, inherited.IsSchedulable())
		require.NoError(t, bank.SetStatus(ctx, owner.ID, a.ID, "active"))
		overview, err := bank.Overview(ctx, owner.ID, "", "", 20, 0)
		require.NoError(t, err)
		require.Equal(t, 1, overview.TotalAccounts)
		require.Len(t, overview.Accounts, 1)
		require.Positive(t, overview.TotalRevenue)
		other, err := bank.Overview(ctx, consumer.ID, "", "", 20, 0)
		require.NoError(t, err)
		require.Empty(t, other.Accounts)
		t.Logf("Verified rental account %d: earnings %.8f, policy %d; money rows persist independently of usage logs", a.ID, overview.TotalRevenue, p.ID)
	})
}
