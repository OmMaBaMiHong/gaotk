//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTokenBankShowcaseIntegration(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	bank := NewTokenBankRepository(integrationDB)
	prefix := "showcase-" + uuid.NewString()
	// These isolated fixture rows exercise only the read model. Atomic balance
	// credit and ledger settlement are covered by TestTokenBankIntegration.
	insert := `INSERT INTO account_revenue_ledger (request_id,api_key_id,account_id,owner_account_id,owner_user_id,admin_user_id,channel_id,platform,group_id,model,billing_type,input_tokens,output_tokens,cache_tokens,bill_amount,owner_share_bps,owner_amount,admin_amount) VALUES ($1,1,$2,$2,$3,1,1,'deepseek',1,'test',0,1,1,0,$4,8000,$4,0)`
	owners := make([]int64, 12)
	for i := range owners {
		u := mustCreateUser(t, client, &service.User{Username: fmt.Sprintf("%s-%02d", prefix, i)})
		owners[i] = u.ID
		_, err := integrationDB.ExecContext(ctx, insert, fmt.Sprintf("%s-%d", prefix, i), i+1, u.ID, 1000000-i)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM account_revenue_ledger WHERE request_id LIKE $1`, prefix+"%")
	})
	// A second account contributes to the same owner's all-time total.
	_, err := integrationDB.ExecContext(ctx, insert, prefix+"-second-account", 99, owners[1], 2)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, insert, prefix+"-zero", 99, owners[1], 0)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, owners[0])
	require.NoError(t, err)
	// An uncommitted entry must never appear in the public read model.
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, insert, prefix+"-uncommitted", 99, owners[2], 9999999)
	require.NoError(t, err)
	result, err := bank.Showcase(ctx)
	require.NoError(t, err)
	require.Len(t, result.Leaderboard, 10)
	require.Equal(t, prefix+"-01", result.Leaderboard[0].Name)
	require.Equal(t, 1000001.0, result.Leaderboard[0].Amount)
	for i, row := range result.Leaderboard {
		require.Equal(t, i+1, row.Rank)
		require.NotEqual(t, prefix+"-00", row.Name)
		require.Less(t, row.Amount, 9999999.0)
	}
	require.Len(t, result.Recent, 10)
	require.Equal(t, 2.0, result.Recent[0].Amount)
	for _, row := range result.Recent {
		require.Positive(t, row.Amount)
		require.NotEqual(t, prefix+"-00", row.Name)
		require.Less(t, row.Amount, 9999999.0)
	}
}
