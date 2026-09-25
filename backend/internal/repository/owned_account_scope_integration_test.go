//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOwnedAccountListFiltersBeforePagination(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewAccountRepository(client, integrationDB, nil)
	owner := mustCreateUser(t, client, &service.User{})
	other := mustCreateUser(t, client, &service.User{})
	prefix := "owned-list-" + uuid.NewString()
	makeAccount := func(name string, ownerID *int64) *service.Account {
		account := &service.Account{Name: prefix + name, OwnerUserID: ownerID, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Concurrency: 3, Credentials: map[string]any{"access_token": "test"}}
		require.NoError(t, repo.Create(ctx, account))
		return account
	}
	mine := makeAccount("-mine", &owner.ID)
	makeAccount("-other", &other.ID)
	makeAccount("-admin", nil)
	scoped := service.WithOwnedAccountScope(ctx, owner.ID, nil)
	params := pagination.PaginationParams{Page: 1, PageSize: 1}
	rows, page, err := repo.ListWithFilters(scoped, params, "", "", "", prefix, 0, "")
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, rows, 1)
	require.Equal(t, mine.ID, rows[0].ID)
	all, err := repo.ListAllWithFilters(scoped, "", "", "", prefix, 0, "")
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, mine.ID, all[0].ID)
	adminFiltered := service.WithAccountListOwnerFilter(ctx, other.ID)
	otherRows, otherPage, err := repo.ListWithFilters(adminFiltered, params, "", "", "", prefix, 0, "")
	require.NoError(t, err)
	require.Equal(t, int64(1), otherPage.Total)
	require.Equal(t, other.ID, *otherRows[0].OwnerUserID)
	require.Zero(t, service.OwnedAccountUserID(adminFiltered), "admin permissions must be unchanged")
	_, adminPage, err := repo.ListWithFilters(ctx, params, "", "", "", prefix, 0, "")
	require.NoError(t, err)
	require.Equal(t, int64(3), adminPage.Total)
}
